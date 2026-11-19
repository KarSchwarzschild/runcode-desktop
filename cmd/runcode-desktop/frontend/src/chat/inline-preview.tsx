// InlinePreview 把一段已判定可预览的内容画进助手消息：成功时显示图形，失败时
// 退回传入的 defaultNode（原代码块）并附带可读原因。代码和预览共用同一份解析结果。
//
// HTML 另受 InlinePreviewBudgetProvider 名额约束（liveFrames）：有名额且靠近视口才挂
// iframe；等待名额显示占位（不当失败）；离屏卸载保留已测高度。SVG／Mermaid 不占名额。
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { sanitizeSvg } from '@/preview/static-svg'
import {
  HTML_PREVIEW_SANDBOX, createHeightGovernor, probeIframeContentHeight,
} from '@/preview/static-html'
import { prepareHtmlPreview, HTML_SCRIPT_PREVIEW_SANDBOX, readHtmlPreviewMessage } from '@/preview/html-document'
import { renderMermaid } from '@/preview/static-mermaid-loader'
import {
  clampHtmlZoomPercent, contentDigest, HTML_ZOOM_MAX, HTML_ZOOM_MIN,
  htmlZoomScrollLeft, PREVIEW_LIMITS, stepHtmlZoomPercent, type StaticPreviewKind,
} from '@/preview/static-detect'
import { logPreviewFailure, previewFailureHint } from '@/preview/preview-failure'
import { noteInlineFile, type InlineFileStatus } from '@/session/inline-preview-status'
import { copyText } from '@/core/bridge'
import { Icon } from '@/ui/icons'
import { InlineError, SystemNote } from '@/ui/feedback'
import { Popover } from '@/ui/popover'
import { IconBtn } from '@/preview/icon-btn'
import { ImageLightbox } from '@/preview/image-lightbox'
import { codePreviewTarget, type CodePreviewTarget } from '@/preview/code-preview'
import { downloadSvgText } from '@/preview/svg-download'
import { useHtmlPreviewSlot } from './inline-preview-budget'

type Props = {
  kind: StaticPreviewKind
  source: string
  defaultNode: ReactNode
  /** 流式中只延后已闭合块；完成态和直出预览不增加等待。 */
  streaming?: boolean
  onHeightChange?: () => void
  messageId?: string
  /** 与本预览同扩展名、同条消息里提到的工作区相对路径；用于抑制右侧自动预览。 */
  relatedPaths?: string[]
  sessionId?: string
  previewId?: string
  onOpenCodePreview?: (target: CodePreviewTarget) => void
}

type Ready =
  | { kind: 'svg' | 'mermaid'; url: string; svg: string; width: number; height: number }
  | { kind: 'html'; srcdoc: string; mode: 'static' | 'scripted'; channel: string | null; blocked: { scripts: number; externalStyles: number } }

const PREVIEW_TITLE: Record<StaticPreviewKind, string> = {
  mermaid: 'Mermaid 预览',
  svg: 'SVG 预览',
  html: 'HTML 预览',
}

const INLINE_IMAGE_STYLE = {
  maxHeight: 'clamp(240px, 42vh, 420px)',
} as const

const STREAMING_DEBOUNCE_MS = 200

function reportPaths(sessionId: string | undefined, paths: string[] | undefined, status: InlineFileStatus) {
  if (!sessionId || !paths?.length) return
  for (const p of paths) noteInlineFile(sessionId, p, status)
}

export function InlinePreview({
  kind, source, defaultNode, streaming = false, onHeightChange, messageId, relatedPaths, sessionId,
  previewId, onOpenCodePreview,
}: Props) {
  const [renderSource, setRenderSource] = useState<string | null>(() => streaming ? null : source)
  const [ready, setReady] = useState<Ready | null>(null)
  const [failReason, setFailReason] = useState<string | null>(null)
  const [view, setView] = useState<'preview' | 'source'>('preview')
  const [viewOpen, setViewOpen] = useState(false)
  const [lightboxOpen, setLightboxOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  /** HTML 用户缩放（相对设计宽）；null 表示使用初始 100%。 */
  const [zoomPercent, setZoomPercent] = useState<number | null>(null)
  const urlRef = useRef<string | null>(null)
  const notify = useRef(onHeightChange)
  notify.current = onHeightChange
  // 代次：源变更／卸载后迟到的异步结果不得回填。
  const genRef = useRef(0)
  const copyTimer = useRef<number | null>(null)

  // 路径列表用稳定键，避免父组件每次新数组引用导致重渲循环。
  const pathsKey = relatedPaths?.join('\0') ?? ''
  const pathsRef = useRef(relatedPaths)
  pathsRef.current = relatedPaths

  // 流式时只延后已经闭合的围栏；源内容变化时先撤掉旧结果，避免展示过期图。
  useEffect(() => {
    if (!streaming) {
      setRenderSource(source)
      return
    }
    setRenderSource(null)
    setReady(null)
    setFailReason(null)
    setViewOpen(false)
    setLightboxOpen(false)
    if (urlRef.current) {
      URL.revokeObjectURL(urlRef.current)
      urlRef.current = null
    }
    const timer = window.setTimeout(() => setRenderSource(source), STREAMING_DEBOUNCE_MS)
    return () => window.clearTimeout(timer)
  }, [source, streaming])

  useEffect(() => {
    if (renderSource === null) return
    const stableSource = renderSource
    const gen = ++genRef.current
    let cancelled = false
    const ac = new AbortController()
    const paths = pathsRef.current
    setViewOpen(false)
    setLightboxOpen(false)
    setReady(null)
    setFailReason(null)
    reportPaths(sessionId, paths, 'pending')

    const fail = (reason: string) => {
      if (cancelled || gen !== genRef.current) return
      setFailReason(reason)
      logPreviewFailure({ kind, reason, messageId })
      reportPaths(sessionId, paths, 'fallback')
    }

    async function run() {
      try {
        if (kind === 'svg') {
          const r = sanitizeSvg(stableSource)
          if (!r.ok) { fail(r.reason); return }
          const url = URL.createObjectURL(new Blob([r.svg], { type: 'image/svg+xml' }))
          if (cancelled || gen !== genRef.current) { URL.revokeObjectURL(url); return }
          if (urlRef.current) URL.revokeObjectURL(urlRef.current)
          urlRef.current = url
          setReady({ kind: 'svg', url, svg: r.svg, width: r.width, height: r.height })
          reportPaths(sessionId, paths, 'rendered')
          return
        }
        if (kind === 'html') {
          const r = prepareHtmlPreview(stableSource)
          if (!r.ok) { fail(r.reason); return }
          if (!cancelled && gen === genRef.current) {
            setReady({ ...r, kind: 'html' })
            reportPaths(sessionId, paths, 'rendered')
          }
          return
        }
        const r = await renderMermaid(stableSource, ac.signal)
        if (cancelled || ac.signal.aborted || gen !== genRef.current) return
        if (!r.ok) { fail(r.reason); return }
        const url = URL.createObjectURL(new Blob([r.svg], { type: 'image/svg+xml' }))
        if (cancelled || gen !== genRef.current) { URL.revokeObjectURL(url); return }
        if (urlRef.current) URL.revokeObjectURL(urlRef.current)
        urlRef.current = url
        setReady({ kind: 'mermaid', url, svg: r.svg, width: r.width, height: r.height })
        reportPaths(sessionId, paths, 'rendered')
      } catch (err) {
        const msg = err instanceof Error ? err.message : 'unknown'
        fail(/CSSStyleSheet/i.test(msg) ? 'platform-unsupported' : 'syntax')
      }
    }
    void run()
    return () => {
      cancelled = true
      ac.abort()
      if (urlRef.current) { URL.revokeObjectURL(urlRef.current); urlRef.current = null }
    }
  }, [kind, renderSource, messageId, sessionId, pathsKey])

  useEffect(() => {
    if (ready) notify.current?.()
  }, [ready])

  // 换 HTML 源：清用户缩放，新页面重新从 100% 开始。
  const htmlSrcdoc = ready?.kind === 'html' ? ready.srcdoc : null
  useEffect(() => {
    if (!htmlSrcdoc) return
    setZoomPercent(null)
  }, [htmlSrcdoc])

  useEffect(() => () => {
    if (copyTimer.current != null) window.clearTimeout(copyTimer.current)
  }, [])

  const title = PREVIEW_TITLE[kind]
  const imageReady = ready && ready.kind !== 'html' ? ready : null
  const canFullscreen = targetFullscreen(imageReady, view)
  const digest = contentDigest(source.trim())
  const displayZoom = zoomPercent ?? 100
  const applyZoom = (next: number) => {
    setZoomPercent(clampHtmlZoomPercent(next))
  }
  /** 用函数式更新，避免连点／同步多击仍读到同一帧的旧比例。 */
  const stepZoom = (direction: 1 | -1) => {
    setZoomPercent((cur) => stepHtmlZoomPercent(cur ?? 100, direction))
  }
  const openSidePreview = () => {
    if (!onOpenCodePreview) return
    onOpenCodePreview(codePreviewTarget({
      kind, source, title, sessionId, messageId, previewId, relatedPaths,
    }))
  }
  const onCopySource = () => {
    void (async () => {
      try {
        await copyText(source)
        setCopied(true)
        if (copyTimer.current != null) window.clearTimeout(copyTimer.current)
        copyTimer.current = window.setTimeout(() => setCopied(false), 1600)
      } catch {
        setCopied(false)
      }
    })()
  }
  const titleNode = kind === 'html' && onOpenCodePreview ? (
    <button
      type="button"
      className="text-[12px] font-medium text-ink flex-1 min-w-0 truncate text-left hover:text-primary"
      title="在右侧预览打开"
      onClick={openSidePreview}
    >
      {title}
    </button>
  ) : (
    <span className="text-[12px] font-medium text-ink flex-1 min-w-0 truncate">{title}</span>
  )
  const header = ready ? (
    <div className="relative flex items-center gap-2 min-h-9 px-2.5 border-b border-line2 bg-surface">
      {titleNode}
      {/* 复制仅源码态出现，样式与「视图」同系幽灵按钮（非黑底）。 */}
      {view === 'source' ? (
        <button
          type="button"
          className={HEADER_TOOL_BTN}
          title="复制源码"
          onClick={onCopySource}
        >
          <Icon name="copy" size={12} />
          {copied ? '已复制' : '复制'}
        </button>
      ) : null}
      {kind === 'html' && view === 'preview' ? (
        <div className="flex-none inline-flex items-center gap-0.5" data-html-zoom-controls>
          <button
            type="button"
            className={HEADER_TOOL_BTN}
            aria-label="缩小"
            title="缩小"
            disabled={displayZoom <= HTML_ZOOM_MIN}
            onClick={() => stepZoom(-1)}
          >
            −
          </button>
          <span
            className="min-w-10 text-center text-[12px] tabular-nums text-muted px-0.5"
            title={`缩放 ${displayZoom}%`}
            aria-live="polite"
          >
            {displayZoom}%
          </span>
          <button
            type="button"
            className={HEADER_TOOL_BTN}
            aria-label="放大"
            title="放大"
            disabled={displayZoom >= HTML_ZOOM_MAX}
            onClick={() => stepZoom(1)}
          >
            +
          </button>
        </div>
      ) : null}
      <button
        type="button"
        className={HEADER_TOOL_BTN}
        aria-expanded={viewOpen}
        onClick={() => setViewOpen((open) => !open)}
      >
        视图
        <Icon name="chevron-down" size={12} className={viewOpen ? 'rotate-180 transition' : 'transition'} />
      </button>
      <Popover open={viewOpen} onClose={() => setViewOpen(false)} placement="down-right" className="w-[132px]">
        <button type="button" className={`${VIEW_ITEM} ${view === 'preview' ? ACTIVE_VIEW_ITEM : ''}`} onClick={() => { setView('preview'); setViewOpen(false) }}>
          {kind === 'html' ? '仅显示页面' : '仅显示图表'}
        </button>
        <button type="button" className={`${VIEW_ITEM} ${view === 'source' ? ACTIVE_VIEW_ITEM : ''}`} onClick={() => { setView('source'); setViewOpen(false) }}>
          仅显示代码
        </button>
      </Popover>
      {imageReady && view === 'preview' && (
        <IconBtn
          name="download"
          title="下载 SVG"
          onClick={() => downloadSvgText(imageReady.svg, `code-${kind}-${digest}.svg`)}
        />
      )}
      {canFullscreen && <IconBtn name="expand" title="全屏" onClick={() => setLightboxOpen(true)} />}
    </div>
  ) : (
    <div className="flex items-center min-h-9 px-2.5 border-b border-line2 bg-surface">
      <span className="text-[12px] font-medium text-ink truncate">{title}</span>
    </div>
  )

  if (failReason) {
    return (
      <div className="my-2 overflow-hidden rounded-lg border border-line2 bg-surface">
        {header}
        <div className="p-2.5">
          {defaultNode}
          <p className="text-[12px] text-faint px-0.5 mt-1.5" role="note">
            未显示预览：{previewFailureHint(failReason)}
          </p>
        </div>
      </div>
    )
  }
  if (!ready) {
    return <div className="my-2 min-h-12 rounded-lg border border-line2 bg-inset/40" aria-hidden />
  }
  if (ready.kind === 'html') {
    return (
      <div className="my-2 overflow-visible rounded-lg border border-line2 bg-surface">
        {header}
        {ready.blocked.scripts + ready.blocked.externalStyles > 0 && (
          <SystemNote tone="warning">页面依赖的外部脚本或样式未加载，部分内容可能未显示；可切换源码查看。</SystemNote>
        )}
        {view === 'source' ? (
          <SourceShell>{defaultNode}</SourceShell>
        ) : (
          <HtmlPreviewGate
            key={ready.srcdoc}
            srcdoc={ready.srcdoc}
            mode={ready.mode}
            channel={ready.channel}
            zoomPercent={displayZoom}
            onZoomPercentChange={applyZoom}
            defaultNode={defaultNode}
            onHeightChange={() => notify.current?.()}
            onFallback={(reason) => {
              setFailReason(reason)
              logPreviewFailure({ kind: 'html', reason, messageId })
              reportPaths(sessionId, relatedPaths, 'fallback')
            }}
          />
        )}
      </div>
    )
  }
  return (
    <div className="my-2 overflow-visible rounded-lg border border-line2 bg-surface">
      {header}
      {view === 'source' ? (
        <SourceShell>{defaultNode}</SourceShell>
      ) : (
        <div className="p-2 overflow-hidden rounded-b-lg">
          {/* 图面不再单击开右侧专用预览（尤其 Mermaid inline-static）；全屏走顶栏。 */}
          <img
            src={ready.url}
            alt={`${title}图`}
            width={ready.width}
            height={ready.height}
            style={INLINE_IMAGE_STYLE}
            className="block max-w-full w-auto h-auto mx-auto object-contain"
            onLoad={() => notify.current?.()}
          />
        </div>
      )}
      <ImageLightbox
        open={lightboxOpen && !!imageReady}
        src={imageReady?.url ?? ''}
        alt={`${title}图`}
        mode="vector"
        onClose={() => setLightboxOpen(false)}
      />
    </div>
  )
}

/** 顶栏幽灵工具钮：与「视图」同级，不用黑底主按钮。 */
const HEADER_TOOL_BTN = 'flex-none inline-flex items-center gap-1 h-7 px-2 rounded-md text-[12px] text-muted hover:text-ink hover:bg-surface2'
const VIEW_ITEM = 'block w-full text-left px-3 py-2 text-[12px] text-ink2 hover:bg-surface2'
const ACTIVE_VIEW_ITEM = 'text-primary bg-primarysoft'

/** 源码视图：限高 + 纵向滚动 + 显示折行；复制走 header 的原始 source，不依赖选区。 */
function SourceShell({ children }: { children: ReactNode }) {
  return (
    <div
      data-inline-source-shell
      className="p-2.5 max-h-[min(50vh,480px)] overflow-y-auto overflow-x-hidden break-words whitespace-pre-wrap [&_pre]:whitespace-pre-wrap [&_pre]:break-words [&_code]:whitespace-pre-wrap [&_code]:break-words"
    >
      {children}
    </div>
  )
}

function targetFullscreen(ready: Extract<Ready, { kind: 'svg' | 'mermaid' }> | null, view: 'preview' | 'source'): boolean {
  return !!ready && view === 'preview'
}

/** HTML：先占位并观察视口，有名额才挂 HtmlFrame；无 IO 且无名额时回退源码。 */
function HtmlPreviewGate({
  srcdoc, mode, channel, zoomPercent, onZoomPercentChange, defaultNode, onHeightChange, onFallback,
}: {
  srcdoc: string
  mode: 'static' | 'scripted'
  channel: string | null
  zoomPercent: number
  onZoomPercentChange: (percent: number) => void
  defaultNode: ReactNode
  onHeightChange?: () => void
  onFallback?: (reason: string) => void
}) {
  const shellRef = useRef<HTMLDivElement>(null)
  const hasSlot = useHtmlPreviewSlot(shellRef, true)
  const [savedHeight, setSavedHeight] = useState<number | null>(null)
  const noIo = typeof IntersectionObserver === 'undefined'
  const unsupported = noIo && !hasSlot
  const onFallbackRef = useRef(onFallback)
  onFallbackRef.current = onFallback

  useEffect(() => {
    if (unsupported) onFallbackRef.current?.('platform-unsupported')
  }, [unsupported])

  // 无 IntersectionObserver 且拿不到名额 → 保留源码（手册要求），不是无限空白。
  if (unsupported) {
    return (
      <div className="my-2 flex flex-col gap-1">
        {defaultNode}
        <p className="text-[12px] text-faint px-0.5" role="note">
          未显示预览：{previewFailureHint('platform-unsupported')}
        </p>
      </div>
    )
  }

  return (
    <div ref={shellRef}>
      {hasSlot ? (
        <HtmlFrame
          srcdoc={srcdoc}
          mode={mode}
          channel={channel}
          zoomPercent={zoomPercent}
          onZoomPercentChange={onZoomPercentChange}
          onHeightChange={onHeightChange}
          onMeasured={(h) => setSavedHeight(h)}
        />
      ) : (
        // 等待名额或暂时离屏：占位，不当渲染失败。
        <div
          className="rounded-lg border border-line2 bg-inset/40"
          style={{ minHeight: savedHeight ?? 48 }}
          aria-hidden
        />
      )}
    </div>
  )
}

function HtmlFrame({
  srcdoc, mode, channel, zoomPercent, onZoomPercentChange, onHeightChange, onMeasured,
}: {
  srcdoc: string
  mode: 'static' | 'scripted'
  channel: string | null
  zoomPercent: number
  onZoomPercentChange: (percent: number) => void
  onHeightChange?: () => void
  onMeasured?: (height: number) => void
}) {
  const displayRef = useRef<HTMLDivElement>(null)
  const shellRef = useRef<HTMLDivElement>(null)
  const ref = useRef<HTMLIFrameElement>(null)
  const gov = useRef(createHeightGovernor({ max: PREVIEW_LIMITS.htmlMaxHeight }))
  const designW = PREVIEW_LIMITS.htmlDesignWidth
  const maxVisual = PREVIEW_LIMITS.htmlInlineMaxHeight
  const probeH = PREVIEW_LIMITS.htmlMaxHeight
  const initialHeight = mode === 'scripted' ? PREVIEW_LIMITS.htmlScriptViewportHeight : 120
  const [contentH, setContentH] = useState(initialHeight)
  const [innerScroll, setInnerScroll] = useState(false)
  const [scriptError, setScriptError] = useState(false)
  const [viewportW, setViewportW] = useState(0)
  const previousLayout = useRef({ width: 0, viewport: 0, scrollLeft: 0 })
  const onZoomRef = useRef(onZoomPercentChange)
  onZoomRef.current = onZoomPercentChange
  const zoomRef = useRef(zoomPercent)
  zoomRef.current = zoomPercent
  const onMeasuredRef = useRef(onMeasured)
  onMeasuredRef.current = onMeasured
  const onHeightChangeRef = useRef(onHeightChange)
  onHeightChangeRef.current = onHeightChange

  useEffect(() => {
    gov.current.reset()
    setContentH(initialHeight)
    setInnerScroll(false)
    setScriptError(false)
    previousLayout.current = { width: 0, viewport: 0, scrollLeft: 0 }
  }, [srcdoc, initialHeight])

  useEffect(() => {
    const el = shellRef.current
    if (!el) return
    const sync = () => {
      const w = el.clientWidth
      if (w <= 0) return
      // 默认比例固定为 100%；容器变化只更新滚动几何，不暗中改变内容大小。
      setViewportW(w)
    }
    sync()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => sync())
    ro.observe(el)
    return () => ro.disconnect()
  }, [srcdoc, designW])

  // 父壳与静态文档共用步进；跨来源脚本文档通过校验后的消息请求缩放。
  const stepWheelRef = useRef<(direction: 1 | -1) => void>(() => {})
  stepWheelRef.current = (direction) => {
    const next = stepHtmlZoomPercent(zoomRef.current, direction)
    zoomRef.current = next
    onZoomRef.current(next)
  }

  useEffect(() => {
    const el = shellRef.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return
      e.preventDefault()
      e.stopPropagation()
      stepWheelRef.current(e.deltaY < 0 ? 1 : -1)
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [srcdoc])

  useEffect(() => {
    if (mode !== 'scripted' || !channel) return
    const onMessage = (event: MessageEvent) => {
      // opaque origin 必须同时校验窗口与本代 channel，不能只凭 type 接受任意页面消息。
      if (event.source !== ref.current?.contentWindow || event.origin !== 'null') return
      const message = readHtmlPreviewMessage(event.data, channel)
      if (!message) return
      if ('height' in message) {
        const next = gov.current.observe(Math.max(initialHeight, message.height))
        setContentH(next.height)
        setInnerScroll(next.scroll)
      } else if ('zoom' in message) {
        stepWheelRef.current(message.zoom)
      } else {
        setScriptError(true)
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [mode, channel, initialHeight])

  useEffect(() => {
    if (mode !== 'static') return
    const frame = ref.current
    if (!frame) return
    let ro: ResizeObserver | null = null
    let wheelDoc: Document | null = null
    const onWheel = (event: WheelEvent) => {
      if (!event.ctrlKey && !event.metaKey) return
      event.preventDefault()
      event.stopPropagation()
      stepWheelRef.current(event.deltaY < 0 ? 1 : -1)
    }
    const measure = () => {
      const doc = frame.contentDocument
      if (!doc?.body) return
      // 探测高度后再收敛，避免短 iframe 下主栏塌缩。
      const probed = probeIframeContentHeight(frame, probeH)
      const next = gov.current.observe(probed)
      setContentH(next.height)
      setInnerScroll(next.scroll)
      // freeze 且高度封顶时保留文档内滚动；否则主滚动在外壳，避免双重滚轮。
      const overflow = next.scroll ? 'auto' : 'hidden'
      doc.documentElement.style.overflow = overflow
      doc.body.style.overflow = overflow
      onHeightChangeRef.current?.()
    }
    const onLoad = () => {
      wheelDoc?.removeEventListener('wheel', onWheel)
      wheelDoc = frame.contentDocument
      wheelDoc?.addEventListener('wheel', onWheel, { passive: false })
      measure()
      ro?.disconnect()
      if (typeof ResizeObserver !== 'undefined' && frame.contentDocument?.body) {
        ro = new ResizeObserver(() => measure())
        ro.observe(frame.contentDocument.body)
      }
    }
    frame.addEventListener('load', onLoad)
    if (frame.contentDocument?.readyState === 'complete') onLoad()
    return () => {
      frame.removeEventListener('load', onLoad)
      wheelDoc?.removeEventListener('wheel', onWheel)
      ro?.disconnect()
    }
  }, [srcdoc, probeH, mode])

  const scale = clampHtmlZoomPercent(zoomPercent) / 100
  const scaledW = designW * scale
  const scaledH = Math.ceil(contentH * scale)
  const shellH = Math.min(maxVisual, scaledH)

  useLayoutEffect(() => {
    const shell = shellRef.current
    if (!shell || viewportW <= 0) return
    // 包装层负责未溢出时的居中；溢出时保留缩放前视区中心，仍可滚到左右两端。
    const nextScrollLeft = htmlZoomScrollLeft({
      previousWidth: previousLayout.current.width,
      previousViewport: previousLayout.current.viewport,
      // DOM 更新后浏览器可能已把 scrollLeft 截到新上限，必须使用上次保存的值。
      scrollLeft: previousLayout.current.scrollLeft,
      width: scaledW,
      viewport: viewportW,
    })
    shell.scrollLeft = nextScrollLeft
    // 保留未取整的逻辑位置，连续缩放不会累积浏览器整数滚动的舍入误差。
    previousLayout.current = { width: scaledW, viewport: viewportW, scrollLeft: nextScrollLeft }
  }, [scaledW, viewportW])

  useEffect(() => {
    // 错误提示也是预览占位的一部分，离屏卸载时一并保存其真实高度。
    onMeasuredRef.current?.(displayRef.current?.getBoundingClientRect().height || shellH)
    onHeightChangeRef.current?.()
  }, [shellH, scriptError])

  return (
    <div ref={displayRef}>
    {scriptError && <InlineError variant="text" className="px-2.5 py-2">页面脚本执行失败或资源加载受限，部分内容可能未显示；可切换源码查看。</InlineError>}
    <div
      ref={shellRef}
      data-html-inline-shell
      data-html-inner-scroll={innerScroll ? '1' : '0'}
      onScroll={(event) => {
        const actual = event.currentTarget.scrollLeft
        // 程序滚动可能四舍五入；只有实际横移才更新内容锚点。
        if (Math.abs(actual - previousLayout.current.scrollLeft) > 1) previousLayout.current.scrollLeft = actual
      }}
      className="w-full max-w-full overflow-x-auto overflow-y-auto bg-surface"
      style={{ maxHeight: maxVisual, height: shellH, scrollbarGutter: 'stable' }}
    >
      <div className="mx-auto" style={{ width: scaledW, height: scaledH, overflow: 'hidden' }}>
        <iframe
          ref={ref}
          title="HTML 预览"
          sandbox={mode === 'scripted' ? HTML_SCRIPT_PREVIEW_SANDBOX : HTML_PREVIEW_SANDBOX}
          data-html-mode={mode}
          data-html-channel={channel ?? undefined}
          srcDoc={srcdoc}
          className="block border-0 bg-white"
          style={{
            width: designW,
            height: contentH,
            transform: `scale(${scale})`,
            transformOrigin: 'top left',
          }}
        />
      </div>
    </div>
    </div>
  )
}

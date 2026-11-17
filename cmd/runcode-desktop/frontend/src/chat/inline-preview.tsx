// InlinePreview 把一段已判定可预览的静态内容画进助手消息：成功时显示图形，失败时
// 退回传入的 defaultNode（原代码块）。代码和预览共用同一份解析结果。
//
// HTML 另受 InlinePreviewBudgetProvider 名额约束（liveFrames）：有名额且靠近视口才挂
// iframe；等待名额显示占位（不当失败）；离屏卸载保留已测高度。SVG／Mermaid 不占名额。
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { sanitizeSvg } from '@/preview/static-svg'
import { renderStaticHtml, HTML_PREVIEW_SANDBOX, measureDocumentHeight, createHeightGovernor } from '@/preview/static-html'
import { renderMermaid } from '@/preview/static-mermaid-loader'
import { PREVIEW_LIMITS, type StaticPreviewKind } from '@/preview/static-detect'
import { Icon } from '@/ui/icons'
import { Popover } from '@/ui/popover'
import { IconBtn } from '@/preview/icon-btn'
import { ImageLightbox } from '@/preview/image-lightbox'
import { useHtmlPreviewSlot } from './inline-preview-budget'

type Props = {
  kind: StaticPreviewKind
  source: string
  defaultNode: ReactNode
  onHeightChange?: () => void
}

type Ready =
  | { kind: 'svg' | 'mermaid'; url: string; width: number; height: number }
  | { kind: 'html'; srcdoc: string }

const PREVIEW_TITLE: Record<StaticPreviewKind, string> = {
  mermaid: 'Mermaid 预览',
  svg: 'SVG 预览',
  html: 'HTML 预览',
}

const INLINE_IMAGE_STYLE = {
  maxHeight: 'clamp(240px, 42vh, 420px)',
} as const

export function InlinePreview({ kind, source, defaultNode, onHeightChange }: Props) {
  const [ready, setReady] = useState<Ready | null>(null)
  const [failed, setFailed] = useState(false)
  const [view, setView] = useState<'preview' | 'source'>('preview')
  const [viewOpen, setViewOpen] = useState(false)
  const [lightboxOpen, setLightboxOpen] = useState(false)
  const urlRef = useRef<string | null>(null)
  const notify = useRef(onHeightChange)
  notify.current = onHeightChange
  // 代次：源变更／卸载后迟到的异步结果不得回填。
  const genRef = useRef(0)

  useEffect(() => {
    const gen = ++genRef.current
    let cancelled = false
    const ac = new AbortController()
    setViewOpen(false)
    setLightboxOpen(false)
    setReady(null)
    setFailed(false)

    async function run() {
      try {
        if (kind === 'svg') {
          const r = sanitizeSvg(source)
          if (!r.ok) { if (!cancelled && gen === genRef.current) setFailed(true); return }
          const url = URL.createObjectURL(new Blob([r.svg], { type: 'image/svg+xml' }))
          if (cancelled || gen !== genRef.current) { URL.revokeObjectURL(url); return }
          if (urlRef.current) URL.revokeObjectURL(urlRef.current)
          urlRef.current = url
          setReady({ kind: 'svg', url, width: r.width, height: r.height })
          return
        }
        if (kind === 'html') {
          const r = renderStaticHtml(source)
          if (!r.ok) { if (!cancelled && gen === genRef.current) setFailed(true); return }
          if (!cancelled && gen === genRef.current) setReady({ kind: 'html', srcdoc: r.srcdoc })
          return
        }
        const r = await renderMermaid(source, ac.signal)
        if (cancelled || ac.signal.aborted || gen !== genRef.current) return
        if (!r.ok) { setFailed(true); return }
        const url = URL.createObjectURL(new Blob([r.svg], { type: 'image/svg+xml' }))
        if (cancelled || gen !== genRef.current) { URL.revokeObjectURL(url); return }
        if (urlRef.current) URL.revokeObjectURL(urlRef.current)
        urlRef.current = url
        setReady({ kind: 'mermaid', url, width: r.width, height: r.height })
      } catch {
        if (!cancelled && gen === genRef.current) setFailed(true)
      }
    }
    void run()
    return () => {
      cancelled = true
      ac.abort()
      if (urlRef.current) { URL.revokeObjectURL(urlRef.current); urlRef.current = null }
    }
  }, [kind, source])

  useEffect(() => {
    if (ready) notify.current?.()
  }, [ready])

  const title = PREVIEW_TITLE[kind]
  const imageReady = ready && ready.kind !== 'html' ? ready : null
  const canFullscreen = targetFullscreen(imageReady, view)
  const header = ready ? (
    <div className="relative flex items-center gap-2 min-h-9 px-2.5 border-b border-line2 bg-surface">
      <span className="text-[12px] font-medium text-ink flex-1 min-w-0 truncate">{title}</span>
      <button
        type="button"
        className="flex-none inline-flex items-center gap-1 h-7 px-2 rounded-md text-[12px] text-muted hover:text-ink hover:bg-surface2"
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
      {canFullscreen && <IconBtn name="expand" title="全屏" onClick={() => setLightboxOpen(true)} />}
    </div>
  ) : (
    <div className="flex items-center min-h-9 px-2.5 border-b border-line2 bg-surface">
      <span className="text-[12px] font-medium text-ink truncate">{title}</span>
    </div>
  )

  if (failed) {
    return (
      <div className="my-2 overflow-hidden rounded-lg border border-line2 bg-surface">
        {header}
        <div className="p-2.5">{defaultNode}</div>
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
        {view === 'source' ? (
          <div className="p-2.5">{defaultNode}</div>
        ) : (
          <HtmlPreviewGate
            srcdoc={ready.srcdoc}
            defaultNode={defaultNode}
            onHeightChange={() => notify.current?.()}
          />
        )}
      </div>
    )
  }
  return (
    <div className="my-2 overflow-visible rounded-lg border border-line2 bg-surface">
      {header}
      {view === 'source' ? (
        <div className="p-2.5">{defaultNode}</div>
      ) : (
        <div className="p-2 overflow-hidden rounded-b-lg">
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

const VIEW_ITEM = 'block w-full text-left px-3 py-2 text-[12px] text-ink2 hover:bg-surface2'
const ACTIVE_VIEW_ITEM = 'text-primary bg-primarysoft'

function targetFullscreen(ready: Extract<Ready, { kind: 'svg' | 'mermaid' }> | null, view: 'preview' | 'source'): boolean {
  return !!ready && view === 'preview'
}

/** HTML：先占位并观察视口，有名额才挂 HtmlFrame；无 IO 且无名额时回退源码。 */
function HtmlPreviewGate({
  srcdoc, defaultNode, onHeightChange,
}: {
  srcdoc: string
  defaultNode: ReactNode
  onHeightChange?: () => void
}) {
  const shellRef = useRef<HTMLDivElement>(null)
  const hasSlot = useHtmlPreviewSlot(shellRef, true)
  const [savedHeight, setSavedHeight] = useState<number | null>(null)
  const noIo = typeof IntersectionObserver === 'undefined'

  // 无 IntersectionObserver 且拿不到名额 → 保留源码（手册要求），不是无限空白。
  if (noIo && !hasSlot) return <>{defaultNode}</>

  return (
    <div ref={shellRef}>
      {hasSlot ? (
        <HtmlFrame
          srcdoc={srcdoc}
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
  srcdoc, onHeightChange, onMeasured,
}: {
  srcdoc: string
  onHeightChange?: () => void
  onMeasured?: (height: number) => void
}) {
  const ref = useRef<HTMLIFrameElement>(null)
  const gov = useRef(createHeightGovernor({ max: PREVIEW_LIMITS.htmlMaxHeight }))
  const [box, setBox] = useState({ height: 120, scroll: false })

  useEffect(() => {
    gov.current.reset()
    setBox({ height: 120, scroll: false })
  }, [srcdoc])

  useEffect(() => {
    const frame = ref.current
    if (!frame) return
    let ro: ResizeObserver | null = null
    const measure = () => {
      const doc = frame.contentDocument
      if (!doc?.body) return
      const next = gov.current.observe(measureDocumentHeight(doc))
      setBox(next)
      onMeasured?.(next.height)
      doc.documentElement.style.overflow = next.scroll ? 'auto' : 'hidden'
      onHeightChange?.()
    }
    const onLoad = () => {
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
      ro?.disconnect()
    }
  }, [srcdoc, onHeightChange, onMeasured])

  return (
    <div
      className={`overflow-hidden bg-surface${box.scroll ? ' overflow-y-auto' : ''}`}
      style={{ maxHeight: box.scroll ? PREVIEW_LIMITS.htmlMaxHeight : undefined }}
    >
      <iframe
        ref={ref}
        title="静态预览"
        sandbox={HTML_PREVIEW_SANDBOX}
        srcDoc={srcdoc}
        className="block w-full border-0 bg-white"
        style={{ height: box.height }}
      />
    </div>
  )
}

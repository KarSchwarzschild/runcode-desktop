// InlinePreview 把一段已判定可预览的静态内容画进助手消息：成功时显示图形，失败时
// 退回传入的 defaultNode（原代码块）并附带可读原因。无工具栏、无切换按钮。
//
// HTML 另受 InlinePreviewBudgetProvider 名额约束（liveFrames）：有名额且靠近视口才挂
// iframe；等待名额显示占位（不当失败）；离屏卸载保留已测高度。SVG／Mermaid 不占名额。
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { sanitizeSvg } from '@/preview/static-svg'
import { renderStaticHtml, HTML_PREVIEW_SANDBOX, measureDocumentHeight, createHeightGovernor } from '@/preview/static-html'
import { renderMermaid } from '@/preview/static-mermaid-loader'
import { PREVIEW_LIMITS, type StaticPreviewKind } from '@/preview/static-detect'
import { logPreviewFailure, previewFailureHint } from '@/preview/preview-failure'
import { noteInlineFile, type InlineFileStatus } from '@/session/inline-preview-status'
import { useHtmlPreviewSlot } from './inline-preview-budget'

type Props = {
  kind: StaticPreviewKind
  source: string
  defaultNode: ReactNode
  onHeightChange?: () => void
  messageId?: string
  /** 与本预览同扩展名、同条消息里提到的工作区相对路径；用于抑制右侧自动预览。 */
  relatedPaths?: string[]
  sessionId?: string
}

type Ready =
  | { kind: 'svg' | 'mermaid'; url: string; width: number; height: number }
  | { kind: 'html'; srcdoc: string }

function reportPaths(sessionId: string | undefined, paths: string[] | undefined, status: InlineFileStatus) {
  if (!sessionId || !paths?.length) return
  for (const p of paths) noteInlineFile(sessionId, p, status)
}

export function InlinePreview({ kind, source, defaultNode, onHeightChange, messageId, relatedPaths, sessionId }: Props) {
  const [ready, setReady] = useState<Ready | null>(null)
  const [failReason, setFailReason] = useState<string | null>(null)
  const urlRef = useRef<string | null>(null)
  const notify = useRef(onHeightChange)
  notify.current = onHeightChange
  // 代次：源变更／卸载后迟到的异步结果不得回填。
  const genRef = useRef(0)

  // 路径列表用稳定键，避免父组件每次新数组引用导致重渲循环。
  const pathsKey = relatedPaths?.join('\0') ?? ''
  const pathsRef = useRef(relatedPaths)
  pathsRef.current = relatedPaths

  useEffect(() => {
    const gen = ++genRef.current
    let cancelled = false
    const ac = new AbortController()
    const paths = pathsRef.current
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
          const r = sanitizeSvg(source)
          if (!r.ok) { fail(r.reason); return }
          const url = URL.createObjectURL(new Blob([r.svg], { type: 'image/svg+xml' }))
          if (cancelled || gen !== genRef.current) { URL.revokeObjectURL(url); return }
          if (urlRef.current) URL.revokeObjectURL(urlRef.current)
          urlRef.current = url
          setReady({ kind: 'svg', url, width: r.width, height: r.height })
          reportPaths(sessionId, paths, 'rendered')
          return
        }
        if (kind === 'html') {
          const r = renderStaticHtml(source)
          if (!r.ok) { fail(r.reason); return }
          if (!cancelled && gen === genRef.current) {
            setReady({ kind: 'html', srcdoc: r.srcdoc })
            reportPaths(sessionId, paths, 'rendered')
          }
          return
        }
        const r = await renderMermaid(source, ac.signal)
        if (cancelled || ac.signal.aborted || gen !== genRef.current) return
        if (!r.ok) { fail(r.reason); return }
        const url = URL.createObjectURL(new Blob([r.svg], { type: 'image/svg+xml' }))
        if (cancelled || gen !== genRef.current) { URL.revokeObjectURL(url); return }
        if (urlRef.current) URL.revokeObjectURL(urlRef.current)
        urlRef.current = url
        setReady({ kind: 'mermaid', url, width: r.width, height: r.height })
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
  }, [kind, source, messageId, sessionId, pathsKey])

  useEffect(() => {
    if (ready) notify.current?.()
  }, [ready])

  if (failReason) {
    return (
      <div className="my-2 flex flex-col gap-1">
        {defaultNode}
        <p className="text-[12px] text-faint px-0.5" role="note">
          未显示预览：{previewFailureHint(failReason)}
        </p>
      </div>
    )
  }
  if (!ready) {
    return <div className="my-2 min-h-12 rounded-lg border border-line2 bg-inset/40" aria-hidden />
  }
  if (ready.kind === 'html') {
    return (
      <HtmlPreviewGate
        srcdoc={ready.srcdoc}
        defaultNode={defaultNode}
        onHeightChange={() => notify.current?.()}
        onFallback={(reason) => {
          setFailReason(reason)
          logPreviewFailure({ kind: 'html', reason, messageId })
          reportPaths(sessionId, relatedPaths, 'fallback')
        }}
      />
    )
  }
  return (
    <div className="my-2 overflow-hidden rounded-lg border border-line2 bg-surface">
      <img
        src={ready.url}
        alt=""
        width={ready.width}
        height={ready.height}
        className="block max-w-full h-auto mx-auto"
        onLoad={() => notify.current?.()}
      />
    </div>
  )
}

/** HTML：先占位并观察视口，有名额才挂 HtmlFrame；无 IO 且无名额时回退源码。 */
function HtmlPreviewGate({
  srcdoc, defaultNode, onHeightChange, onFallback,
}: {
  srcdoc: string
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
    <div ref={shellRef} className="my-2">
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
      className={`overflow-hidden rounded-lg border border-line2 bg-surface${box.scroll ? ' overflow-y-auto' : ''}`}
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

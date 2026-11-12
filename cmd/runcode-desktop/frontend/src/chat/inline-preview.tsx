// InlinePreview 把一段已判定可预览的静态内容画进助手消息：成功时显示图形，失败时
// 退回传入的 defaultNode（原代码块）。无工具栏、无切换按钮。
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { sanitizeSvg } from '@/preview/static-svg'
import { renderStaticHtml, HTML_PREVIEW_SANDBOX, measureDocumentHeight, createHeightGovernor } from '@/preview/static-html'
import { renderMermaid } from '@/preview/static-mermaid-loader'
import { PREVIEW_LIMITS, type StaticPreviewKind } from '@/preview/static-detect'

type Props = {
  kind: StaticPreviewKind
  source: string
  defaultNode: ReactNode
  onHeightChange?: () => void
}

type Ready =
  | { kind: 'svg' | 'mermaid'; url: string; width: number; height: number }
  | { kind: 'html'; srcdoc: string }

export function InlinePreview({ kind, source, defaultNode, onHeightChange }: Props) {
  const [ready, setReady] = useState<Ready | null>(null)
  const [failed, setFailed] = useState(false)
  const urlRef = useRef<string | null>(null)
  const notify = useRef(onHeightChange)
  notify.current = onHeightChange

  useEffect(() => {
    let cancelled = false
    const ac = new AbortController()
    setReady(null)
    setFailed(false)

    async function run() {
      try {
        if (kind === 'svg') {
          const r = sanitizeSvg(source)
          if (!r.ok) { if (!cancelled) setFailed(true); return }
          const url = URL.createObjectURL(new Blob([r.svg], { type: 'image/svg+xml' }))
          if (cancelled) { URL.revokeObjectURL(url); return }
          if (urlRef.current) URL.revokeObjectURL(urlRef.current)
          urlRef.current = url
          setReady({ kind: 'svg', url, width: r.width, height: r.height })
          return
        }
        if (kind === 'html') {
          const r = renderStaticHtml(source)
          if (!r.ok) { if (!cancelled) setFailed(true); return }
          if (!cancelled) setReady({ kind: 'html', srcdoc: r.srcdoc })
          return
        }
        const r = await renderMermaid(source, ac.signal)
        if (cancelled || ac.signal.aborted) return
        if (!r.ok) { setFailed(true); return }
        const url = URL.createObjectURL(new Blob([r.svg], { type: 'image/svg+xml' }))
        if (cancelled) { URL.revokeObjectURL(url); return }
        if (urlRef.current) URL.revokeObjectURL(urlRef.current)
        urlRef.current = url
        setReady({ kind: 'mermaid', url, width: r.width, height: r.height })
      } catch {
        if (!cancelled) setFailed(true)
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

  if (failed) return <>{defaultNode}</>
  if (!ready) {
    return <div className="my-2 min-h-12 rounded-lg border border-line2 bg-inset/40" aria-hidden />
  }
  if (ready.kind === 'html') {
    return <HtmlFrame srcdoc={ready.srcdoc} onHeightChange={() => notify.current?.()} />
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

function HtmlFrame({ srcdoc, onHeightChange }: { srcdoc: string; onHeightChange?: () => void }) {
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
  }, [srcdoc, onHeightChange])

  return (
    <div
      className={`my-2 overflow-hidden rounded-lg border border-line2 bg-surface${box.scroll ? ' overflow-y-auto' : ''}`}
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

// GeneratedImageCard：一次 image_generate 的紧凑结果卡。
// 成功只保留缩略图、宽高/大小/格式与三个文件操作；失败仅两种中性文案。
// 不展示文件名、完整路径、requestId、Message、Code、SubmissionStatus、计费与堆栈。
import { useContext, useEffect, useRef, useState, type RefObject } from 'react'
import { openArtifact, releaseArtifact, type ImageGenerationArtifact, type ToolEvent } from '@/core/bridge'
import {
  imageGenerationCardState, imageGenerationFailureLabel, isImageGenerationResult,
} from '@/core/image-generation'
import { fmtBytes } from '@/core/format'
import { Icon } from '@/ui/icons'
import { ArtifactActionsContext } from './artifact-actions'
import { ImageEditCanvas } from './image-edit-canvas'
import './generated-image-card.css'

// useVisible：进入视口才申请预览引用，避免长历史每张卡都占着引用。
function useVisible(ref: RefObject<HTMLElement>): boolean {
  const [visible, setVisible] = useState(false)
  useEffect(() => {
    const el = ref.current
    if (!el || visible) return
    if (typeof IntersectionObserver === 'undefined') {
      setVisible(true)
      return
    }
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        setVisible(true)
        io.disconnect()
      }
    })
    io.observe(el)
    return () => io.disconnect()
  }, [ref, visible])
  return visible
}

function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(() =>
    typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches,
  )
  useEffect(() => {
    if (typeof matchMedia === 'undefined') return
    const mq = matchMedia('(prefers-reduced-motion: reduce)')
    const onChange = () => setReduced(mq.matches)
    mq.addEventListener?.('change', onChange)
    return () => mq.removeEventListener?.('change', onChange)
  }, [])
  return reduced
}

type Thumb = { path: string; url: string; failed: boolean }

const REVEAL_STRIPS = 8

export function GeneratedImageCard({ tool, sessionId, onOpenFile }: { tool: ToolEvent; sessionId: string; onOpenFile?: (relPath: string) => void }) {
  const actions = useContext(ArtifactActionsContext)
  const rootRef = useRef<HTMLDivElement>(null)
  const visible = useVisible(rootRef)
  const reduceMotion = usePrefersReducedMotion()
  const result = isImageGenerationResult(tool.data) ? tool.data : null
  const image = result?.status === 'succeeded' ? result.images?.[0] : undefined
  const sourcePath = image?.path ?? ''
  // 保存标注后改绑到最新 edited 副本；原图路径仍留在工具结果里。
  const [edited, setEdited] = useState<ImageGenerationArtifact | null>(null)
  const path = edited?.path || sourcePath
  const [editing, setEditing] = useState(false)
  const [thumb, setThumb] = useState<Thumb | null>(null)
  const [revealing, setRevealing] = useState(false)
  const revealRootRef = useRef<HTMLDivElement>(null)
  // 挂载期间见过 running 才播百叶窗；历史回放直接静态图。
  const sawRunningRef = useRef(false)
  const revealForUrlRef = useRef('')

  useEffect(() => {
    if (!visible || !path || !sessionId) return
    let cancelled = false
    let reference = ''
    openArtifact(sessionId, path)
      .then((artifact) => {
        if (cancelled || artifact.sessionId !== sessionId) {
          void releaseArtifact(artifact.reference).catch(() => {})
          return
        }
        reference = artifact.reference
        setThumb({ path, url: artifact.url, failed: false })
      })
      .catch(() => {
        // 不把后端错误原文画到卡片上；只记失败供状态机判 missing。
        if (!cancelled) setThumb({ path, url: '', failed: true })
      })
    return () => {
      cancelled = true
      if (reference) void releaseArtifact(reference).catch(() => {})
    }
  }, [visible, path, sessionId])

  const current = thumb && thumb.path === path ? thumb : null
  const toolEnded = tool.type === 'completed' || tool.type === 'failed'
  const cardState = imageGenerationCardState(result, {
    toolEnded,
    thumbReadable: !!current?.url,
    thumbFailed: !!current?.failed,
  })
  const metaW = edited?.width || image?.width
  const metaH = edited?.height || image?.height
  const metaBytes = edited?.bytes || image?.bytes
  const metaMime = edited?.mimeType || image?.mimeType || ''

  if (cardState === 'running') {
    sawRunningRef.current = true
  }

  useEffect(() => {
    if (reduceMotion) {
      setRevealing(false)
      return
    }
    if (cardState !== 'ready' || !current?.url) return
    if (!sawRunningRef.current) return
    if (revealForUrlRef.current === current.url) return
    revealForUrlRef.current = current.url
    setRevealing(true)
  }, [cardState, current?.url, reduceMotion])

  // 原生 animationend：最后一条条带结束后再摘遮罩（避免前几条提前清掉）。
  useEffect(() => {
    if (!revealing || reduceMotion) return
    const root = revealRootRef.current
    const last = root?.querySelector('span:last-child')
    if (!last) return
    const done = () => setRevealing(false)
    last.addEventListener('animationend', done)
    return () => last.removeEventListener('animationend', done)
  }, [revealing, reduceMotion])

  return (
    <div ref={rootRef} className="border border-line2 rounded-card bg-surface p-3 flex flex-col gap-2 max-w-[440px] shadow-xs">
      <div className="flex items-center gap-2 text-[13px]">
        <span className="text-muted flex-none"><Icon name="image-gen" size={16} /></span>
        <span className="font-medium text-ink flex-none whitespace-nowrap">生成图片</span>
        {cardState === 'running' && <span className="text-[12px] text-muted flex-none">生成中</span>}
        {cardState === 'ready' && <span className="text-[12px] text-muted flex-none">已生成</span>}
      </div>

      {/* 意图行：生成中读 progress message；完成后读结果里的清洗字段；缺失不画。 */}
      {cardState === 'running' && (
        <p className="text-[12px] text-muted truncate" title={tool.message || undefined}>
          {tool.message || '正在生成图片'}…
        </p>
      )}
      {cardState !== 'running' && result?.displayIntent ? (
        <p className="text-[12px] text-muted truncate" title={result.displayIntent}>
          {result.displayIntent}
        </p>
      ) : null}

      {cardState === 'running' && (
        <div
          className={`ig-card-shimmer flex-none w-[96px] h-[96px] rounded-lg border border-line2${reduceMotion ? '' : ' ig-card-shimmer-animate'}`}
          aria-hidden
        />
      )}

      {(cardState === 'not-generated' || cardState === 'missing') && (
        <p className="text-[12px] text-muted break-words">
          {imageGenerationFailureLabel(cardState, result?.model)}
        </p>
      )}

      {cardState === 'ready' && image && (
        <div className="flex gap-3 items-start min-w-0">
          {sessionId ? (
            current?.url ? (
              <button
                type="button"
                className="ig-card-thumb flex-none w-[96px] h-[96px] rounded-lg overflow-hidden border border-line2 bg-inset cursor-zoom-in hover:border-primary/50 transition"
                title="打开标注画板"
                onClick={() => setEditing(true)}
              >
                <img src={current.url} alt="" className="block w-full h-full object-cover" />
                {revealing ? (
                  <div
                    ref={revealRootRef}
                    className={`ig-card-reveal${reduceMotion ? '' : ' ig-card-reveal-animate'}`}
                    aria-hidden
                  >
                    {Array.from({ length: REVEAL_STRIPS }, (_, i) => (
                      <span key={i} />
                    ))}
                  </div>
                ) : null}
              </button>
            ) : (
              <div className="flex-none w-[96px] h-[96px] rounded-lg border border-line2 bg-inset flex items-center justify-center text-[11px] text-faint">载入中…</div>
            )
          ) : (
            <div className="flex-none w-[96px] h-[96px] rounded-lg border border-dashed border-line2 bg-inset flex items-center justify-center text-[11px] text-faint">缩略图</div>
          )}
          <div className="flex-1 min-w-0 flex flex-col gap-1.5">
            <div className="text-[11px] text-faint">{metaW} × {metaH} · {fmtBytes(metaBytes ?? 0)} · {formatLabel(metaMime)}</div>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px]">
              <button type="button" className="text-primary hover:underline" onClick={() => onOpenFile?.(path)}>预览</button>
              <button type="button" className="text-muted hover:text-ink" onClick={() => { actions.reveal(path).catch(() => {}) }}>在文件夹中显示</button>
              <button type="button" className="text-muted hover:text-ink" onClick={() => { actions.external(path).catch(() => {}) }}>用系统程序打开</button>
            </div>
          </div>
        </div>
      )}

      {editing && current?.url && path ? (
        <ImageEditCanvas
          sessionId={sessionId}
          sourcePath={path}
          imageUrl={current.url}
          naturalWidth={metaW ?? 0}
          naturalHeight={metaH ?? 0}
          onClose={() => setEditing(false)}
          onSaved={(art) => { setEdited(art); setEditing(false) }}
        />
      ) : null}
    </div>
  )
}

function formatLabel(mime: string): string {
  const m = (mime || '').toLowerCase()
  if (m.includes('png')) return 'PNG'
  if (m.includes('jpeg') || m.includes('jpg')) return 'JPEG'
  if (m.includes('webp')) return 'WebP'
  if (m.includes('gif')) return 'GIF'
  return mime || '图像'
}

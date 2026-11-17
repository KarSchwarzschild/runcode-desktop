// GeneratedImageCard：一次 image_generate 的紧凑结果卡。
// 成功只保留缩略图、宽高/大小/格式与三个文件操作；失败仅两种中性文案。
// 不展示文件名、完整路径、requestId、Message、Code、SubmissionStatus、计费与堆栈。
import { useContext, useEffect, useRef, useState, type RefObject } from 'react'
import { openArtifact, releaseArtifact, type ToolEvent } from '@/core/bridge'
import {
  imageGenerationCardState, imageGenerationFailureLabel, isImageGenerationResult,
} from '@/core/image-generation'
import { fmtBytes } from '@/core/format'
import { Icon } from '@/ui/icons'
import { ArtifactActionsContext } from './artifact-actions'

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

type Thumb = { path: string; url: string; failed: boolean }

export function GeneratedImageCard({ tool, sessionId, onOpenFile }: { tool: ToolEvent; sessionId: string; onOpenFile?: (relPath: string) => void }) {
  const actions = useContext(ArtifactActionsContext)
  const rootRef = useRef<HTMLDivElement>(null)
  const visible = useVisible(rootRef)
  const result = isImageGenerationResult(tool.data) ? tool.data : null
  const image = result?.status === 'succeeded' ? result.images?.[0] : undefined
  const path = image?.path ?? ''
  const [thumb, setThumb] = useState<Thumb | null>(null)

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

  return (
    <div ref={rootRef} className="border border-line2 rounded-card bg-surface p-3 flex flex-col gap-2 max-w-[440px] shadow-xs">
      <div className="flex items-center gap-2 text-[13px]">
        <span className="text-muted flex-none"><Icon name="image-gen" size={16} /></span>
        <span className="font-medium text-ink flex-none whitespace-nowrap">生成图片</span>
        {cardState === 'running' && <span className="text-[12px] text-muted flex-none">生成中</span>}
        {cardState === 'ready' && <span className="text-[12px] text-muted flex-none">已生成</span>}
      </div>

      {cardState === 'running' && (
        <p className="text-[12px] text-muted">{tool.message || '正在生成图片'}…</p>
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
                className="flex-none w-[96px] h-[96px] rounded-lg overflow-hidden border border-line2 bg-inset cursor-zoom-in hover:border-primary/50 transition"
                title="在预览面板打开"
                onClick={() => onOpenFile?.(path)}
              >
                <img src={current.url} alt="" className="block w-full h-full object-cover" />
              </button>
            ) : (
              <div className="flex-none w-[96px] h-[96px] rounded-lg border border-line2 bg-inset flex items-center justify-center text-[11px] text-faint">载入中…</div>
            )
          ) : (
            <div className="flex-none w-[96px] h-[96px] rounded-lg border border-dashed border-line2 bg-inset flex items-center justify-center text-[11px] text-faint">缩略图</div>
          )}
          <div className="flex-1 min-w-0 flex flex-col gap-1.5">
            <div className="text-[11px] text-faint">{image.width} × {image.height} · {fmtBytes(image.bytes)} · {formatLabel(image.mimeType)}</div>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px]">
              <button type="button" className="text-primary hover:underline" onClick={() => onOpenFile?.(path)}>预览</button>
              <button type="button" className="text-muted hover:text-ink" onClick={() => { actions.reveal(path).catch(() => {}) }}>在文件夹中显示</button>
              <button type="button" className="text-muted hover:text-ink" onClick={() => { actions.external(path).catch(() => {}) }}>用系统程序打开</button>
            </div>
          </div>
        </div>
      )}
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

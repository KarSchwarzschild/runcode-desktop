// GeneratedImageCard 是一次 image_generate 的结果卡片：成功时直接画缩略图（不依赖
// 回答正文里提到路径），失败/未知/保存失败时如实说明状态。缩略图只在卡片可见时才向
// 来源会话申请预览引用（OpenArtifact(sessionId, path)），卸载或换图时释放；文件已删除
// 显示「文件不存在」。这里没有「重试」：重新生成必须由用户发起新的一轮。
import { useContext, useEffect, useRef, useState, type RefObject } from 'react'
import { errCode, errText, openArtifact, releaseArtifact, type ToolEvent } from '@/core/bridge'
import {
  IMAGE_GENERATION_STATUS_LABEL, imageGenerationResultSummary, isImageGenerationResult,
} from '@/core/image-generation'
import { basename } from '@/core/paths'
import { fmtBytes } from '@/core/format'
import { Icon } from '@/ui/icons'
import { ArtifactActionsContext } from './artifact-actions'

// useVisible flips once the element first scrolls into view: a long history of
// images must not hold a preview reference for every card at once.
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

type Thumb = { path: string; url: string; error: string; missing: boolean }

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
        setThumb({ path, url: artifact.url, error: '', missing: false })
      })
      .catch((e: unknown) => {
        if (!cancelled) setThumb({ path, url: '', error: errText(e) || '无法预览这张图片', missing: errCode(e) === 'not_found' })
      })
    return () => {
      cancelled = true
      if (reference) void releaseArtifact(reference).catch(() => {})
    }
  }, [visible, path, sessionId])

  const current = thumb && thumb.path === path ? thumb : null
  const running = !result && tool.type !== 'completed' && tool.type !== 'failed'
  const status = result?.status ?? (running ? 'running' : 'failed')
  const tone = status === 'succeeded' ? 'text-green' : status === 'failed' ? 'text-red' : 'text-amberink'
  const label = running ? '生成中' : IMAGE_GENERATION_STATUS_LABEL[status] ?? status

  return (
    <div ref={rootRef} className="border border-line2 rounded-card bg-surface p-3 flex flex-col gap-2 max-w-[440px] shadow-xs">
      <div className="flex items-center gap-2 text-[13px]">
        <span className="text-muted flex-none"><Icon name="image-gen" size={16} /></span>
        <span className="font-medium text-ink flex-none whitespace-nowrap">生成图片</span>
        <span className={`text-[12px] flex-none whitespace-nowrap ${tone}`}>{label}</span>
        {result?.model && <span className="ml-auto text-[11px] text-faint font-mono truncate min-w-0" title={result.model}>{result.model}</span>}
      </div>

      {running && <p className="text-[12px] text-muted">{tool.message || '正在生成图片'}…</p>}

      {!running && !result && <p className="text-[12px] text-muted">没有可显示的生成结果；不会自动重新生成。</p>}

      {image && (
        <div className="flex gap-3 items-start min-w-0">
          {/* 紧凑缩略图：固定小框，不撑开卡片；点击与「预览」相同，右侧打开。 */}
          {sessionId ? (
            current?.missing ? (
              <div className="flex-none w-[96px] h-[96px] rounded-lg border border-line2 bg-inset flex items-center justify-center px-1.5 text-center text-[11px] text-amberink leading-snug">文件不存在</div>
            ) : current?.error ? (
              <div className="flex-none w-[96px] h-[96px] rounded-lg border border-line2 bg-inset flex items-center justify-center px-1.5 text-center text-[11px] text-red leading-snug break-words">{current.error}</div>
            ) : current?.url ? (
              <button
                type="button"
                className="flex-none w-[96px] h-[96px] rounded-lg overflow-hidden border border-line2 bg-inset cursor-zoom-in hover:border-primary/50 transition"
                title="在预览面板打开"
                onClick={() => onOpenFile?.(path)}
              >
                <img src={current.url} alt="生成的图片" className="block w-full h-full object-cover" />
              </button>
            ) : (
              <div className="flex-none w-[96px] h-[96px] rounded-lg border border-line2 bg-inset flex items-center justify-center text-[11px] text-faint">载入中…</div>
            )
          ) : (
            <div className="flex-none w-[96px] h-[96px] rounded-lg border border-dashed border-line2 bg-inset flex items-center justify-center text-[11px] text-faint">缩略图</div>
          )}
          <div className="flex-1 min-w-0 flex flex-col gap-1.5">
            <div className="text-[11px] text-faint">{image.width} × {image.height} · {fmtBytes(image.bytes)} · {image.mimeType}</div>
            <div className="flex items-center gap-2 min-w-0">
              <span className="font-mono text-[12px] text-ink2 truncate min-w-0" title={path}>{basename(path)}</span>
            </div>
            <div className="font-mono text-[11px] text-faint truncate min-w-0" title={path}>{path}</div>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px]">
              <button type="button" className="text-primary hover:underline" onClick={() => onOpenFile?.(path)}>预览</button>
              <button type="button" className="text-muted hover:text-ink" onClick={() => { actions.reveal(path).catch(() => {}) }}>在文件夹中显示</button>
              <button type="button" className="text-muted hover:text-ink" onClick={() => { actions.external(path).catch(() => {}) }}>用系统程序打开</button>
            </div>
          </div>
        </div>
      )}

      {result && result.status !== 'succeeded' && (
        <p className={`text-[12px] break-words ${result.status === 'failed' ? 'text-muted' : 'text-amberink'}`}>{imageGenerationResultSummary(result)}</p>
      )}
    </div>
  )
}

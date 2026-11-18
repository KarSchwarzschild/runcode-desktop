// 栅格生图查看容器（只读大图）。
// 2026-11-18：永久删除底部紫色「开始标注」及查看器内标注模式／画板挂载；
// image-edit-* 实现仍保留供日后另开入口，本文件不再引用。
import { BTN } from '@/ui/tokens'
import { GeneratedImageModalSurface } from './generated-image-modal-surface'
import { useGeneratedImageViewer } from './generated-image-viewer-context'

export function GeneratedImageViewerHost(_props?: {
  /** @deprecated 标注入口已永久删除；保留参数以免调用方改签名。 */
  onSaved?: (sessionId: string, art: import('@/core/bridge').ImageGenerationArtifact) => void
}) {
  const v = useGeneratedImageViewer()
  if (!v.active || !v.target) return null
  const { target, artifact } = v
  const url = artifact?.url ?? ''

  return (
    <GeneratedImageModalSurface
      open={v.active}
      suspended={v.suspended}
      confirmOpen={false}
      onRequestClose={v.requestClose}
    >
      <div className="flex items-center gap-2 px-4 py-3 border-b border-line2">
        <span className="text-[14px] font-medium text-ink flex-none">查看图片</span>
        <div className="flex-1" />
        <button type="button" className={`${BTN} !px-3`} onClick={v.requestClose}>关闭</button>
      </div>
      <div className="flex-1 min-h-0 overflow-hidden bg-surface2">
        <div className="h-full flex items-center justify-center p-4">
          {url ? (
            <img src={url} alt={target.alt} className="max-w-full max-h-[80vh] object-contain rounded-lg" />
          ) : (
            <p className="text-[12px] text-muted">加载中…</p>
          )}
        </div>
      </div>
    </GeneratedImageModalSurface>
  )
}

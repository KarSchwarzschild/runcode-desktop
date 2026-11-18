// 栅格生图查看／标注容器：同一 host 内切模式，不再套第二层图片 dialog。
import { BTN, BTN_PRIMARY } from '@/ui/tokens'
import { ConfirmDialog } from '@/ui/confirm-dialog'
import { ImageEditCanvas } from './image-edit-canvas'
import { GeneratedImageModalSurface } from './generated-image-modal-surface'
import { useGeneratedImageViewer } from './generated-image-viewer-context'

export function GeneratedImageViewerHost({
  onSaved,
}: {
  onSaved?: (sessionId: string, art: import('@/core/bridge').ImageGenerationArtifact) => void
}) {
  const v = useGeneratedImageViewer()
  if (!v.active || !v.target) return null
  const { target, artifact, mode } = v
  const url = artifact?.url ?? ''

  return (
    <GeneratedImageModalSurface
      open={v.active}
      suspended={v.suspended}
      confirmOpen={v.confirmDiscard}
      onRequestClose={v.requestClose}
      confirm={(
        <ConfirmDialog
          title="放弃未保存的标注？"
          message="当前标注尚未保存，关闭后将丢失。"
          confirmLabel="放弃"
          onConfirm={v.confirmDiscardClose}
          onCancel={v.cancelDiscardClose}
        />
      )}
    >
      <div className="flex items-center gap-2 px-4 py-3 border-b border-line2">
        <span className="text-[14px] font-medium text-ink flex-none">
          {mode === 'annotate' ? '标注图片' : '查看图片'}
        </span>
        <div className="flex items-center gap-1 ml-2">
          <button type="button" className={`${BTN} !px-2 !py-1 text-[12px] ${mode === 'view' ? 'border-primary text-primaryink' : ''}`} onClick={() => v.setMode('view')}>查看</button>
          <button type="button" className={`${BTN} !px-2 !py-1 text-[12px] ${mode === 'annotate' ? 'border-primary text-primaryink' : ''}`} onClick={() => v.setMode('annotate')}>标注</button>
        </div>
        <div className="flex-1" />
        <button type="button" className={`${BTN} !px-3`} onClick={v.requestClose}>关闭</button>
      </div>
      <div className="flex-1 min-h-0 overflow-hidden bg-surface2">
        {mode === 'view' ? (
          <div className="h-full flex items-center justify-center p-4">
            {url ? (
              <img src={url} alt={target.alt} className="max-w-full max-h-[80vh] object-contain rounded-lg" />
            ) : (
              <p className="text-[12px] text-muted">加载中…</p>
            )}
          </div>
        ) : url ? (
          <ImageEditCanvas
            hosted
            sessionId={target.sessionId}
            sourcePath={target.image.path}
            sourceReference={artifact?.reference ?? ''}
            imageUrl={url}
            naturalWidth={target.image.width}
            naturalHeight={target.image.height}
            onClose={v.requestClose}
            onDirtyChange={v.markDirty}
            onSaved={(art) => {
              onSaved?.(target.sessionId, art)
              v.markDirty(false)
              v.confirmDiscardClose()
            }}
          />
        ) : (
          <p className="p-4 text-[12px] text-muted">加载中…</p>
        )}
      </div>
      {mode === 'view' ? (
        <div className="px-4 py-3 border-t border-line2 flex justify-end">
          <button type="button" className={`${BTN} ${BTN_PRIMARY} !px-3`} onClick={() => v.setMode('annotate')}>开始标注</button>
        </div>
      ) : null}
    </GeneratedImageModalSurface>
  )
}

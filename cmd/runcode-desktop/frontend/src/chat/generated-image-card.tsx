// GeneratedImageCard：栅格生图信息卡（无缩略图）+ 下方等比例预览舞台。
// 「预览」只滚动定位并展开下方舞台，不开侧栏；查看／标注由预览区触发。
import { useContext, useRef, useState } from 'react'
import type { ImageGenerationArtifact, ToolEvent } from '@/core/bridge'
import {
  imageGenerationCardState, imageGenerationFailureLabel, isImageGenerationResult,
  isRasterImageGeneration,
} from '@/core/image-generation'
import { fmtBytes } from '@/core/format'
import { Icon } from '@/ui/icons'
import { ArtifactActionsContext } from './artifact-actions'
import { GeneratedImagePreview } from './generated-image-preview'

export function GeneratedImageCard({
  tool, sessionId, imageOverride, onView, onAnnotate,
}: {
  tool: ToolEvent
  sessionId: string
  /** 保存副本后的展示覆盖；不改写工具历史。 */
  imageOverride?: ImageGenerationArtifact | null
  onOpenFile?: (relPath: string) => void
  onView?: (image: ImageGenerationArtifact) => void
  onAnnotate?: (image: ImageGenerationArtifact) => void
}) {
  const actions = useContext(ArtifactActionsContext)
  const previewAnchorRef = useRef<HTMLDivElement>(null)
  const [collapsed, setCollapsed] = useState(false)
  const result = isImageGenerationResult(tool.data) ? tool.data : null
  if (!isRasterImageGeneration(tool, result)) return null

  const source = result?.status === 'succeeded' ? result.images?.[0] : undefined
  const image = imageOverride || source
  const path = image?.path ?? ''
  const toolEnded = tool.type === 'completed' || tool.type === 'failed'
  const cardState = imageGenerationCardState(result, {
    toolEnded,
    previewReadable: !!image,
    previewMissing: false,
  })
  const metaW = image?.width
  const metaH = image?.height
  const metaBytes = image?.bytes
  const metaMime = image?.mimeType || ''
  const intent = cardState === 'running'
    ? (tool.message || '')
    : (result?.displayIntent || '')

  const scrollToPreview = () => {
    setCollapsed(false)
    previewAnchorRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  }

  const metaParts: string[] = []
  if (metaW && metaH) metaParts.push(`${metaW} × ${metaH}`)
  if (typeof metaBytes === 'number' && metaBytes > 0) metaParts.push(fmtBytes(metaBytes))
  if (metaMime) metaParts.push(formatLabel(metaMime))

  const showPreviewStage = cardState === 'running' || cardState === 'ready'

  return (
    <div className="max-w-[min(100%,400px)] min-w-0 w-fit">
      <div
        className="w-fit max-w-full min-w-0 border border-line2 rounded-[10px] bg-surface px-3 py-2.5 flex flex-col gap-1"
        data-generated-image-card=""
      >
        <div className="flex items-center gap-2 min-h-5 text-[13px] leading-5">
          <span className="text-muted flex-none"><Icon name="image-gen" size={15} /></span>
          <span className="font-medium text-ink flex-none whitespace-nowrap">生成图片</span>
          {cardState === 'running' && (
            <span className="ml-2 inline-flex items-center h-5 px-2 rounded-full text-[12px] text-primary bg-primarysoft flex-none">生成中</span>
          )}
          {cardState === 'ready' && (
            <span className="ml-2 inline-flex items-center h-5 px-2 rounded-full text-[12px] text-green bg-greenbg flex-none">已生成</span>
          )}
        </div>

        {intent ? (
          <p className="text-[14px] leading-5 font-semibold text-ink truncate min-w-0" title={intent}>
            {intent}
          </p>
        ) : cardState === 'running' ? (
          <p className="text-[14px] leading-5 font-semibold text-ink truncate">正在生成图片…</p>
        ) : null}

        {(cardState === 'not-generated' || cardState === 'missing') && (
          <p className="text-[12px] leading-[18px] text-muted break-words">
            {imageGenerationFailureLabel(cardState, result?.model)}
          </p>
        )}

        {cardState === 'ready' && metaParts.length > 0 ? (
          <div className="text-[12px] leading-[18px] text-muted truncate">{metaParts.join(' · ')}</div>
        ) : null}

        {cardState === 'ready' && path ? (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] leading-5">
            <ActionBtn icon="eye" label="预览" onClick={scrollToPreview} />
            <ActionBtn icon="folder" label="在文件夹中显示" onClick={() => { void actions.reveal(path).catch(() => {}) }} />
            <ActionBtn icon="external-link" label="用系统程序打开" onClick={() => { void actions.external(path).catch(() => {}) }} />
          </div>
        ) : null}
      </div>

      {showPreviewStage ? (
        <div ref={previewAnchorRef}>
          <GeneratedImagePreview
            tool={tool}
            sessionId={sessionId}
            image={image}
            collapsed={collapsed}
            onToggleCollapse={() => setCollapsed((v) => !v)}
            onView={() => { if (image) onView?.(image) }}
            onAnnotate={() => { if (image) onAnnotate?.(image) }}
          />
        </div>
      ) : null}
    </div>
  )
}

function ActionBtn({ icon, label, onClick }: { icon: string; label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      className="inline-flex items-center gap-[5px] min-h-6 text-ink2 hover:text-primaryink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/35 rounded-sm"
      onClick={onClick}
    >
      <Icon name={icon} size={14} className="flex-none" />
      <span>{label}</span>
    </button>
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

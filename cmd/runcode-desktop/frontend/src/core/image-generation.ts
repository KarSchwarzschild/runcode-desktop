// 文生图的纯逻辑：模型用途筛选、设置页的选择状态、生成结果的收窄与文案。
// 能力只认网关显式声明的 capabilities/imageGeneration（设计 6.1）——不按模型名猜，
// supportsImages 是"能看图"不是"能画图"。
import type { ImageGenerationCatalog, ImageGenerationResult, ImageGenerationSettings, ModelReference, PassportModel } from './bridge'

export const IMAGE_GENERATION_TOOL = 'image_generate'

/** 受支持的栅格 MIME；非栅格产物不进入新卡片／预览／查看器。 */
export const RASTER_IMAGE_GENERATION_MIMES = new Set([
  'image/png',
  'image/jpeg',
  'image/webp',
  'image/gif',
])

/**
 * isRasterImageGeneration：栅格生图 UI 门控。
 * 仅 toolName === image_generate；有结果时 MIME 须为受支持栅格；
 * 生成中尚无结果时只凭工具名允许展示进行中预览。不按模型名判断。
 */
export function isRasterImageGeneration(
  tool: { toolName?: string } | null | undefined,
  result?: ImageGenerationResult | null,
): boolean {
  if ((tool?.toolName ?? '') !== IMAGE_GENERATION_TOOL) return false
  if (!result) return true
  if (!isImageGenerationResult(result)) return false
  if (result.status !== 'succeeded') return true
  const mime = (result.images?.[0]?.mimeType ?? '').trim().toLowerCase()
  if (!mime) return false
  return RASTER_IMAGE_GENERATION_MIMES.has(mime)
}

// isChatModel 是所有文本用途（对话、判定、子代理、上下文预算、识图）候选的统一入口。
// 只有「声明了 image_generation 却没有 chat」才排除；没有能力字段的旧目录照旧当聊天模型。
export function isChatModel(m: PassportModel): boolean {
  const caps = m.capabilities ?? []
  if (caps.length === 0) return true
  return caps.includes('chat') || !caps.includes('image_generation')
}

export function isImageGenerationModel(m: PassportModel): boolean {
  return (m.capabilities ?? []).includes('image_generation')
}

export function imageGenerationModelKey(ref?: ModelReference | null): string {
  return ref ? JSON.stringify([ref.kind, ref.name, ref.bridge ?? '', ref.tenantId ?? '']) : ''
}

/** 多尺寸才允许「不固定尺寸」；仅一种尺寸时必须选该值。 */
export function allowsUnfixedSize(supportedSizes: string[] | null | undefined): boolean {
  return (supportedSizes ?? []).length > 1
}

/** 换模型时的默认尺寸草稿：单尺寸直接钉死，多尺寸默认不固定。 */
export function defaultDraftSize(supportedSizes: string[] | null | undefined): string {
  const sizes = supportedSizes ?? []
  return sizes.length === 1 ? sizes[0]! : ''
}

/** 把已保存尺寸校正到当前模型可选范围（含单尺寸强制）。 */
export function coerceDraftSize(savedSize: string, supportedSizes: string[] | null | undefined): string {
  const sizes = supportedSizes ?? []
  if (sizes.length === 0) return ''
  if (sizes.length === 1) return sizes[0]!
  const size = (savedSize ?? '').trim()
  if (size === '' || sizes.includes(size)) return size
  return ''
}

// settingsSelection 把已保存设置映射到当前目录上。没有保存时什么都不选（不自动挑第一个
// 付费模型）；保存的模型/尺寸在目录里已不可用时 stale=true，由界面画「不可用」占位。
export function settingsSelection(catalog: ImageGenerationCatalog | null, settings: ImageGenerationSettings | null): { key: string; size: string; stale: boolean } {
  const saved = settings?.defaultModel
  if (!saved) return { key: '', size: '', stale: false }
  const key = imageGenerationModelKey(saved)
  const hit = (catalog?.models ?? []).find((m) => imageGenerationModelKey(m.model) === key)
  const sizes = hit?.profile.supportedSizes ?? []
  // 单尺寸模型把已保存值钉成唯一支持项；多尺寸保留原值，不支持的仍标 stale 以便 UI 提示。
  const raw = settings?.defaultSize ?? ''
  const size = sizes.length === 1 ? sizes[0]! : raw
  const sizeStale = size !== '' && !sizes.includes(size)
  const stale = !hit || !hit.profile.available || sizeStale
  return { key, size, stale }
}

export function isImageGenerationResult(data: unknown): data is ImageGenerationResult {
  if (!data || typeof data !== 'object') return false
  const o = data as Partial<ImageGenerationResult>
  return o.version === 1 && typeof o.generationId === 'string' && typeof o.status === 'string'
}

// accountingLabel 只描述人工观察的证据状态。缺失、未知值都按待核验处理——任何
// 结果状态（失败、取消、未知）都不能推出“免费”。卡片 UI 不再展示本标签。
export function accountingLabel(status?: string): string {
  switch (status) {
    case 'verified': return '本次计费已人工核对'
    case 'ambiguous': return '计费记录无法唯一对应，待核验'
    case 'observed_mismatch': return '已发现计费异常，该模型已停用'
    default: return '计费待核验'
  }
}

/** 卡片四态：生成中 / 预览可读 / 本次未交付图 / 曾成功但文件现已不可读。 */
export type ImageGenerationCardState = 'running' | 'ready' | 'not-generated' | 'missing'

/**
 * imageGenerationCardState 把工具结果与下方预览读取结果收成卡片状态。
 * missing 仅当 status=succeeded 且确认 not_found；其它读取失败由预览区单独提示，
 * 不把已完成生成改成「未生成」。
 */
export function imageGenerationCardState(
  result: ImageGenerationResult | null | undefined,
  opts: { toolEnded: boolean; previewReadable?: boolean; previewMissing?: boolean },
): ImageGenerationCardState {
  if (!result) return opts.toolEnded ? 'not-generated' : 'running'
  if (result.status === 'succeeded') {
    if (opts.previewMissing) return 'missing'
    if (opts.previewReadable) return 'ready'
    // 成功结果尚在拉预览：仍按 ready 排版（占位），不落成「未生成」。
    return 'ready'
  }
  return 'not-generated'
}

/** 两种用户可见失败文案（中性色展示）；模型名空时用「未设置模型」。 */
export function imageGenerationFailureLabel(state: 'not-generated' | 'missing', model?: string | null): string {
  const name = (model ?? '').trim() || '未设置模型'
  if (state === 'missing') return `生成图片 文件不存在 ${name}`
  return `生成图片 未生成 ${name}`
}

// 文生图的纯逻辑：模型用途筛选、设置页的选择状态、生成结果的收窄与文案。
// 能力只认网关显式声明的 capabilities/imageGeneration（设计 6.1）——不按模型名猜，
// supportsImages 是"能看图"不是"能画图"。
import type { ImageGenerationCatalog, ImageGenerationResult, ImageGenerationSettings, ModelReference, PassportModel } from './bridge'

export const IMAGE_GENERATION_TOOL = 'image_generate'

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

// settingsSelection 把已保存设置映射到当前目录上。没有保存时什么都不选（不自动挑第一个
// 付费模型）；保存的模型/尺寸在目录里已不可用时 stale=true，由界面画「不可用」占位。
export function settingsSelection(catalog: ImageGenerationCatalog | null, settings: ImageGenerationSettings | null): { key: string; size: string; stale: boolean } {
  const saved = settings?.defaultModel
  if (!saved) return { key: '', size: '', stale: false }
  const key = imageGenerationModelKey(saved)
  const size = settings?.defaultSize ?? ''
  const hit = (catalog?.models ?? []).find((m) => imageGenerationModelKey(m.model) === key)
  const stale = !hit || !hit.profile.available || !(hit.profile.supportedSizes ?? []).includes(size)
  return { key, size, stale }
}

export function isImageGenerationResult(data: unknown): data is ImageGenerationResult {
  if (!data || typeof data !== 'object') return false
  const o = data as Partial<ImageGenerationResult>
  return o.version === 1 && typeof o.generationId === 'string' && typeof o.status === 'string'
}

// accountingLabel 只描述人工观察的证据状态。缺失、未知值都按待核验处理——任何
// 结果状态（失败、取消、未知）都不能推出“免费”。
export function accountingLabel(status?: string): string {
  switch (status) {
    case 'verified': return '本次计费已人工核对'
    case 'ambiguous': return '计费记录无法唯一对应，待核验'
    case 'observed_mismatch': return '已发现计费异常，该模型已停用'
    default: return '计费待核验'
  }
}

export const IMAGE_GENERATION_STATUS_LABEL: Record<string, string> = {
  succeeded: '已生成',
  failed: '未生成',
  unknown: '结果未知',
  cancelled: '已停止等待',
  save_failed: '保存失败',
}

export function imageGenerationResultSummary(r: ImageGenerationResult): string {
  switch (r.status) {
    case 'succeeded': return '图片已生成并保存到工作区'
    case 'failed': return r.message || '图片未生成'
    case 'unknown': return r.message || '生成服务未返回可确认的结果，可能已生成或计费，请勿重复提交'
    case 'cancelled': return r.message || '已停止等待；上游可能已生成或计费'
    case 'save_failed': return r.message || '已收到生成结果，下载或保存失败，计费待核验'
    default: return r.message || r.status
  }
}

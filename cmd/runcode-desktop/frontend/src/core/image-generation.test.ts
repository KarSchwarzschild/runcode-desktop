import { describe, expect, it } from 'vitest'
import type { ImageGenerationCatalog, ImageGenerationResult, PassportModel } from './bridge'
import {
  accountingLabel, allowsUnfixedSize, coerceDraftSize, defaultDraftSize,
  imageGenerationModelKey, imageGenerationResultSummary, isChatModel, isImageGenerationModel, submissionLabel,
  isImageGenerationResult, settingsSelection,
} from './image-generation'

const profile = { available: true, supportedSizes: ['1024x1024', '2048x2048'], defaultSize: '1024x1024', responseFormats: ['url'] }
const ref = (name: string, tenantId = 't1') => ({ kind: 'platform', name, bridge: 'https://bridge.example', tenantId })

describe('model purpose filters', () => {
  const models: PassportModel[] = [
    { id: 'legacy', ownedBy: 'p' },
    { id: 'chat', ownedBy: 'p', capabilities: ['chat'] },
    { id: 'seedream-like-name', ownedBy: 'v' },
    { id: 'image-only', ownedBy: 'v', capabilities: ['image_generation'], imageGeneration: profile },
    { id: 'dual', ownedBy: 'p', capabilities: ['chat', 'image_generation'], imageGeneration: profile },
    { id: 'vision-input', ownedBy: 'p', supportsImages: true },
  ]
  it('仅生图模型不进入文本用途；旧目录保持聊天', () => {
    expect(models.filter(isChatModel).map((m) => m.id)).toEqual(['legacy', 'chat', 'seedream-like-name', 'dual', 'vision-input'])
  })
  it('只认显式 image_generation 能力，不按名称或 supportsImages 猜', () => {
    expect(models.filter(isImageGenerationModel).map((m) => m.id)).toEqual(['image-only', 'dual'])
  })
})

describe('settingsSelection', () => {
  const catalog: ImageGenerationCatalog = {
    scopeId: 's1',
    models: [
      { model: ref('a'), provider: 'v', profile },
      { model: ref('off'), provider: 'v', profile: { ...profile, available: false, unavailableReason: '未验证' } },
    ],
  }
  it('没有保存时不自动选第一项', () => {
    expect(settingsSelection(catalog, { scopeId: 's1', revision: 0, defaultSize: '' })).toEqual({ key: '', size: '', stale: false })
  })
  it('有效保存选中并带出尺寸', () => {
    const got = settingsSelection(catalog, { scopeId: 's1', revision: 2, defaultModel: ref('a'), defaultSize: '2048x2048' })
    expect(got).toEqual({ key: imageGenerationModelKey(ref('a')), size: '2048x2048', stale: false })
  })
  it('原选择失效时保留不可用占位，不换成其他模型', () => {
    expect(settingsSelection(catalog, { scopeId: 's1', revision: 2, defaultModel: ref('off'), defaultSize: '1024x1024' }).stale).toBe(true)
    expect(settingsSelection(catalog, { scopeId: 's1', revision: 2, defaultModel: ref('gone'), defaultSize: '1024x1024' })).toMatchObject({ key: imageGenerationModelKey(ref('gone')), stale: true })
    expect(settingsSelection(catalog, { scopeId: 's1', revision: 2, defaultModel: ref('a', 'other'), defaultSize: '1024x1024' }).stale).toBe(true)
  })
  it('保存的尺寸已不支持时视为失效', () => {
    expect(settingsSelection(catalog, { scopeId: 's1', revision: 2, defaultModel: ref('a'), defaultSize: '4096x4096' }).stale).toBe(true)
  })
  it('allowsUnfixedSize 仅在多尺寸时为真；单尺寸 defaultDraftSize 钉死', () => {
    expect(allowsUnfixedSize(['1024x1024'])).toBe(false)
    expect(allowsUnfixedSize(['1024x1024', '2048x2048'])).toBe(true)
    expect(defaultDraftSize(['1024x1024'])).toBe('1024x1024')
    expect(defaultDraftSize(['1024x1024', '2048x2048'])).toBe('')
    expect(coerceDraftSize('', ['1024x1024'])).toBe('1024x1024')
    expect(coerceDraftSize('999x999', ['1024x1024', '2048x2048'])).toBe('')
  })

  it('空默认尺寸表示不固定，不算失效', () => {
    expect(settingsSelection(catalog, { scopeId: 's1', revision: 2, defaultModel: ref('a'), defaultSize: '' })).toEqual({
      key: imageGenerationModelKey(ref('a')), size: '', stale: false,
    })
  })
})

describe('results', () => {
  const ok: ImageGenerationResult = {
    version: 1, generationId: 'g1', status: 'succeeded', model: 'a', size: '1024x1024', accountingStatus: 'unverified',
    images: [{ path: '生成图片/g1/image.png', mimeType: 'image/png', bytes: 10, width: 1, height: 1 }],
  }
  it('只收窄出 version 1 的结构化结果', () => {
    expect(isImageGenerationResult(ok)).toBe(true)
    expect(isImageGenerationResult({ ...ok, version: 2 })).toBe(false)
    expect(isImageGenerationResult({ snapshotId: 'x', relPath: 'y' })).toBe(false)
    expect(isImageGenerationResult(null)).toBe(false)
  })
  it('计费状态缺失或未知一律显示待核验，不说失败免费', () => {
    expect(accountingLabel(undefined)).toBe('计费待核验')
    expect(accountingLabel('paid')).toBe('计费待核验')
    expect(accountingLabel('verified')).toBe('本次计费已人工核对')
    for (const status of ['failed', 'unknown', 'cancelled', 'save_failed']) {
      expect(imageGenerationResultSummary({ ...ok, status, images: [] })).not.toMatch(/免费|不收费|未扣费/)
    }
  })
  it('仅本地明确未进入发送边界时显示未提交，历史缺失保持保守', () => {
    expect(submissionLabel('not_submitted')).toBe('未提交生成请求')
    expect(submissionLabel('attempted')).toBe('')
    expect(submissionLabel('unknown')).toBe('')
    expect(submissionLabel(undefined)).toBe('')
  })
  it('未知与保存失败如实说明可能已生成', () => {
    expect(imageGenerationResultSummary({ ...ok, status: 'unknown', images: [] })).toContain('可能已生成')
    expect(imageGenerationResultSummary({ ...ok, status: 'save_failed', images: [] })).toContain('保存失败')
  })
})

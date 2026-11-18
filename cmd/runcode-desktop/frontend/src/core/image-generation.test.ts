import { describe, expect, it } from 'vitest'
import type { ImageGenerationCatalog, ImageGenerationResult, PassportModel } from './bridge'
import {
  accountingLabel, allowsUnfixedSize, coerceDraftSize, defaultDraftSize,
  imageGenerationCardState, imageGenerationFailureLabel, imageGenerationModelKey,
  isChatModel, isImageGenerateToolRunning, isImageGenerationModel, isImageGenerationResult,
  isRasterImageGeneration, settingsSelection,
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
  })
  it('卡片状态：成功可读为 ready，not_found 为 missing，其它未交付为 not-generated', () => {
    expect(imageGenerationCardState(null, { toolEnded: false })).toBe('running')
    expect(imageGenerationCardState(null, { toolEnded: true })).toBe('not-generated')
    expect(imageGenerationCardState(ok, { toolEnded: true, previewReadable: true })).toBe('ready')
    expect(imageGenerationCardState(ok, { toolEnded: true, previewMissing: true })).toBe('missing')
    for (const status of ['failed', 'unknown', 'cancelled', 'save_failed'] as const) {
      expect(imageGenerationCardState({ ...ok, status, images: [] }, { toolEnded: true })).toBe('not-generated')
    }
    expect(imageGenerationCardState({ ...ok, status: 'failed', submissionStatus: 'not_submitted', images: [] }, { toolEnded: true })).toBe('not-generated')
  })
  it('两种失败文案与模型名兜底', () => {
    expect(imageGenerationFailureLabel('not-generated', 'seedream')).toBe('生成图片 未生成 seedream')
    expect(imageGenerationFailureLabel('missing', 'seedream')).toBe('生成图片 文件不存在 seedream')
    expect(imageGenerationFailureLabel('not-generated', '')).toBe('生成图片 未生成 未设置模型')
    expect(imageGenerationFailureLabel('missing', null)).toBe('生成图片 文件不存在 未设置模型')
  })
  it('生图工具进行中判定：完成／失败不算 running', () => {
    expect(isImageGenerateToolRunning({ toolName: 'image_generate', type: 'progress' })).toBe(true)
    expect(isImageGenerateToolRunning({ toolName: 'image_generate', type: 'started' })).toBe(true)
    expect(isImageGenerateToolRunning({ toolName: 'image_generate', type: 'completed' })).toBe(false)
    expect(isImageGenerateToolRunning({ toolName: 'image_generate', type: 'failed' })).toBe(false)
    expect(isImageGenerateToolRunning({ toolName: 'Bash', type: 'progress' })).toBe(false)
  })
  it('栅格门控：只认 image_generate + 受支持 MIME，不按模型名', () => {
    expect(isRasterImageGeneration({ toolName: 'image_generate' })).toBe(true)
    expect(isRasterImageGeneration({ toolName: 'Read' })).toBe(false)
    expect(isRasterImageGeneration({ toolName: 'image_generate' }, ok)).toBe(true)
    expect(isRasterImageGeneration({ toolName: 'image_generate' }, {
      ...ok, model: 'other-vendor', images: [{ ...ok.images![0], mimeType: 'image/jpeg' }],
    })).toBe(true)
    expect(isRasterImageGeneration({ toolName: 'image_generate' }, {
      ...ok, images: [{ ...ok.images![0], mimeType: 'image/svg+xml' }],
    })).toBe(false)
    expect(isRasterImageGeneration({ toolName: 'image_generate' }, {
      ...ok, status: 'failed', images: [],
    })).toBe(true)
  })
})

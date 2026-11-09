import { describe, expect, it } from 'vitest'
import { contextDraft, contextModelCandidates, modelContextKey, parseContextDraft } from './model-context'

describe('model context overrides', () => {
  it('preserves explicit zero separately from inheritance', () => {
    const draft = contextDraft({ maxContextTokens: 0, maxHistoryMessages: 0 })
    expect(draft).toEqual({ maxOutputTokens: '', maxContextTokens: '0', maxHistoryMessages: '0' })
    expect(parseContextDraft(draft)).toEqual({ maxContextTokens: 0, maxHistoryMessages: 0 })
    expect(parseContextDraft(contextDraft({}))).toEqual({})
  })
  it.each(['-1', '1.5', '2e3', '0x100', '+5', '12bad', 'Infinity', '2147483648', '9007199254740993'])('rejects invalid complete integers: %s', (value) => {
    for (const key of ['maxOutputTokens', 'maxContextTokens', 'maxHistoryMessages'] as const) {
      expect(() => parseContextDraft({ ...contextDraft({}), [key]: value })).toThrow('整数')
    }
  })
  it('requires positive output but permits zero budgets', () => {
    expect(() => parseContextDraft({ ...contextDraft({}), maxOutputTokens: '0' })).toThrow()
    expect(parseContextDraft({ maxOutputTokens: ' 4096 ', maxContextTokens: '0', maxHistoryMessages: '' })).toEqual({ maxOutputTokens: 4096, maxContextTokens: 0 })
    expect(parseContextDraft({ ...contextDraft({}), maxOutputTokens: '2147483647' }).maxOutputTokens).toBe(2147483647)
  })
  it('partitions references by platform, tenant, and saved custom profile', () => {
    const refs = [
      { kind: 'platform', name: 'm', bridge: 'https://a', tenantId: 'one' },
      { kind: 'platform', name: 'm', bridge: 'https://a', tenantId: 'two' },
      { kind: 'platform', name: 'm', bridge: 'https://b', tenantId: 'one' },
      { kind: 'custom', name: 'm' },
      { kind: 'custom', name: 'another' },
    ]
    expect(new Set(refs.map(modelContextKey)).size).toBe(5)
    const candidates = contextModelCandidates([{ id: 'm', ownedBy: '' }], [{ name: 'profile', model: 'm', provider: 'openai', baseURL: '', hasAPIKey: false }], 'tenant')
    expect(candidates.map((c) => c.ref)).toEqual([{ kind: 'platform', name: 'm', tenantId: 'tenant' }, { kind: 'custom', name: 'profile' }])
  })
})

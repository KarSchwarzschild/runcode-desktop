import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { KnowledgeStatus, KnowledgePicker, knowledgePickerFooterLabel, KNOWLEDGE_ROW_HEIGHT_PX, MAX_KNOWLEDGE_LIST_HEIGHT_PX, MAX_VISIBLE_KNOWLEDGE_ROWS } from './knowledge-picker'
import type { KnowledgeState } from '@/session/knowledge-state'

const first = { id: 'demo-1', title: '很长的同名知识库测试目录名称', description: '产品说明与常见问题的测试摘要', path: '测试目录/产品资料/一号目录', source: 'demo', documentCount: 24, documentCountKnown: true }
const second = { ...first, id: 'demo-2', path: '测试目录/团队资料/二号目录', documentCount: 6, documentCountKnown: true }
const state: KnowledgeState = { sessionId: 'A', contextVersion: 0, revision: 1, libraries: [first], pendingIds: null, items: [first, second], previewItems: [], mode: 'demo', catalogMode: 'demo', unavailableReason: '', loading: false, loaded: true, saving: false, listing: false, refreshing: false, fromCache: false, catalogUpdatedAt: 0, error: '', listError: '', query: '', page: 1, pageSize: 10, total: 12, hasMore: true, maxSelection: 5 }
const render = (overrides: Partial<KnowledgeState> = {}, locked = false) => renderToStaticMarkup(<KnowledgePicker open state={{ ...state, ...overrides }} locked={locked} onClose={() => {}} onSearch={() => {}} onLoadMore={() => {}} onToggle={() => {}} onRetry={() => {}} />)

describe('knowledge picker viewport contract', () => {
  it('usesKnowledgeCylinderIconInsteadOfBook', () => {
    const markup = render()
    // 圆柱顶面椭圆；选择器不得再画技能书本路径。
    expect(markup).toContain('cx="12"')
    expect(markup).toContain('ry="3"')
    expect(markup).toContain('M4 5v14c0 1.66 3.58 3 8 3s8-1.34 8-3V5')
    expect(markup).not.toContain('M5 4.5h10.5a2 2 0 0 1 2 2V20H7a2 2 0 0 1-2-2Z')
  })

  it('capsTheViewportWithoutDroppingLoadedLibraries', () => {
    expect(MAX_VISIBLE_KNOWLEDGE_ROWS).toBe(7)
    expect(MAX_KNOWLEDGE_LIST_HEIGHT_PX).toBe(MAX_VISIBLE_KNOWLEDGE_ROWS * KNOWLEDGE_ROW_HEIGHT_PX)
    const items = Array.from({ length: 15 }, (_, index) => ({
      ...first,
      id: `lib-${index + 1}`,
      title: `知识库 ${index + 1}`,
      path: `测试目录/${index + 1}`,
    }))
    const selected = items.slice(0, 5)
    const markup = render({ items, libraries: selected, total: 15, hasMore: true })
    for (const library of items) {
      expect(markup).toContain(library.id)
      expect(markup).toContain(library.title)
    }
    expect(markup).toContain('已选择5/5')
    expect(markup).not.toContain('保存中...最多5个')
    expect(markup.match(/aria-pressed="false" disabled=""/g)?.length).toBeGreaterThanOrEqual(10)
    expect(markup).toContain(`max-height:${MAX_KNOWLEDGE_LIST_HEIGHT_PX}px`)
    expect(markup).not.toMatch(/items\.slice\(0,\s*7\)/)
  })

  it('keepsSearchAndSelectionCountOutsideScrollableRows', () => {
    const markup = render()
    const listStart = markup.indexOf('aria-label="知识库列表"')
    const searchPos = markup.indexOf('placeholder="按名称、简介或路径搜索"')
    const countPos = markup.indexOf('已选择 1 / 5')
    expect(listStart).toBeGreaterThan(-1)
    expect(searchPos).toBeGreaterThan(-1)
    expect(countPos).toBeGreaterThan(-1)
    expect(searchPos).toBeLessThan(listStart)
    expect(countPos).toBeGreaterThan(markup.indexOf('</div>', listStart))
    expect(markup).toMatch(/aria-label="知识库列表"[^>]*overflow-y-auto/)
    expect(markup).toMatch(/aria-label="知识库列表"[^>]*overscroll-contain/)
    expect(markup).toContain(`height:${KNOWLEDGE_ROW_HEIGHT_PX}px`)
  })
})

describe('knowledge selection metadata', () => {
  it('describes real search by name and keeps unavailable selection status visible', () => {
    const markup = render({ mode: 'real' })
    expect(markup).toContain('placeholder="搜索知识库名称"')
    expect(markup).not.toContain('按名称、简介或路径搜索')
    const status = renderToStaticMarkup(<KnowledgeStatus state={{ ...state, mode: 'unavailable', unavailableReason: '目录服务暂不可用' }} locked={false} onRetry={() => {}} />)
    expect(status).toContain('目录服务暂不可用')
    expect(status).not.toContain('目录已接入')
  })

  it('omits unknown counts and unknown totals while preserving verified zero counts in real mode', () => {
    const markup = render({ mode: 'real', total: -1, items: [{ ...first, documentCount: 0, documentCountKnown: false }, { ...second, documentCount: 0, documentCountKnown: true }] })
    expect(markup.match(/0 篇文档/g)).toHaveLength(2) // one visible label and its tooltip, only for the second row
    expect(markup).not.toContain('-1 个知识库')
    expect(markup).not.toContain('测试数据，仅验证选择与保存')
    const status = renderToStaticMarkup(<KnowledgeStatus state={{ ...state, mode: 'real' }} locked={false} onRetry={() => {}} />)
    expect(status).toBe('')
    expect(status).not.toContain('每次提问重新检索')
    expect(status).not.toContain('退格、Delete')
  })

  it('displays distinguishable complete names, summaries, logical paths and document counts', () => {
    const markup = render()
    expect(markup).toContain(first.title)
    expect(markup).toContain(first.description)
    expect(markup).toContain(first.path)
    expect(markup).toContain(second.path)
    expect(markup).toContain('24 篇文档')
    expect(markup).toContain('6 篇文档')
    expect(markup).toContain('demo-1')
    expect(markup).toContain('demo-2')
    expect(markup).toContain('测试数据，仅验证选择与保存，暂不参与回答')
    expect(markup).toContain('按名称、简介或路径搜索')
    expect(markup).toContain('aria-pressed="true"')
    expect(markup).not.toContain('上一页')
    expect(markup).not.toContain('下一页')
    expect(markup).not.toContain('href=')
  })
  it('keeps persistence feedback without a separate selected-library panel', () => {
    const markup = renderToStaticMarkup(<KnowledgeStatus state={state} locked={false} onRetry={() => {}} />)
    expect(markup).not.toContain(first.title)
    expect(markup).not.toContain('移除知识库')
    expect(markup).not.toContain('每次提问重新检索')
    expect(markup).not.toContain('退格、Delete')
    expect(markup).toBe('')
  })
  it('renders unavailable reason and a recoverable failure instead of a false confirmation', () => {
    const unavailable = render({ mode: 'unavailable', libraries: [], items: [], unavailableReason: '当前生产服务尚未接入' })
    expect(unavailable).toContain('当前生产服务尚未接入')
    expect(unavailable).not.toContain('测试目录')
    expect(unavailable).not.toContain('测试数据，仅验证选择与保存')
    const initialFailure = render({ loaded: false, error: '读取失败' })
    expect(initialFailure).toMatch(/<button[^>]*>重试读取<\/button>/)
    expect(initialFailure).not.toMatch(/<button[^>]*disabled=""[^>]*>重试读取<\/button>/)
    const failed = renderToStaticMarkup(<KnowledgeStatus state={{ ...state, error: '保存失败，请重试' }} locked={false} onRetry={() => {}} />)
    expect(failed).toContain('保存失败，请重试')
    expect(failed).toContain('重试')
    expect(failed).not.toContain('已保存')
  })
  it('doesNotDisableOtherRowsWhileSaving', () => {
    const choices = render({ saving: true, pendingIds: [first.id, second.id] })
    // 保存途中仍可点未达上限的行；不以整表 disabled／变淡表达 saving。
    expect(choices).toContain('aria-pressed="true"')
    expect(choices).not.toMatch(/aria-pressed="true" disabled=""/)
    expect(choices).toContain('已选择 2 / 5')
    expect(choices).not.toContain('正在保存…')
    const status = renderToStaticMarkup(<KnowledgeStatus state={{ ...state, saving: true, error: '保存失败' }} locked={false} onRetry={() => {}} />)
    expect(status).toContain('disabled=""')
    expect(status).toContain('正在保存知识库选择')
  })
  it('countsPendingSelectionImmediately', () => {
    const markup = render({ libraries: [], pendingIds: [first.id, second.id], saving: true })
    expect(markup).toContain('已选择 2 / 5')
    expect(markup).toContain('aria-pressed="true"')
  })

  it('showsLimitFooterWhenFiveSelectedAndKeepsDelayedCopyBelow', () => {
    expect(knowledgePickerFooterLabel(5, 5, '保存较慢，仍可继续选择…', true)).toBe('保存中...最多5个')
    expect(knowledgePickerFooterLabel(5, 5, '', false)).toBe('已选择5/5')
    expect(knowledgePickerFooterLabel(4, 5, '保存较慢，仍可继续选择…')).toBe('保存较慢，仍可继续选择…')
    expect(knowledgePickerFooterLabel(3, 5, '')).toBe('已选择 3 / 5')
    const savingAtLimit = render({ libraries: [first, second], pendingIds: ['a', 'b', 'c', 'd', 'e'], saving: true })
    expect(savingAtLimit).toContain('保存中...最多5个')
    expect(savingAtLimit).not.toContain('保存较慢，仍可继续选择')
    const savedAtLimit = render({ libraries: [first, second], pendingIds: ['a', 'b', 'c', 'd', 'e'], saving: false })
    expect(savedAtLimit).toContain('已选择5/5')
    expect(savedAtLimit).not.toContain('保存中...最多5个')
  })
})

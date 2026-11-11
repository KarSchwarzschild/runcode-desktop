import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { KnowledgeSelection } from './knowledge-selection'
import type { SessionKnowledge } from '@/session/use-knowledge'
import type { KnowledgeState } from '@/session/knowledge-state'

const library = (id: string, title = `库 ${id}`) => ({
  id, title, description: '', path: '', source: 'demo', documentCount: 0, documentCountKnown: false,
})

function harness(overrides: Partial<KnowledgeState> = {}, actions: Partial<SessionKnowledge> = {}) {
  const state: KnowledgeState = {
    sessionId: 'A', contextVersion: 1, revision: 1, libraries: [library('one'), library('two')],
    pendingIds: null, items: [], previewItems: [], mode: 'real', catalogMode: 'real', unavailableReason: '',
    loading: false, saving: false, listing: false, refreshing: false, fromCache: false, catalogUpdatedAt: 0,
    loaded: true, error: '', listError: '', query: '', page: 1, pageSize: 10, total: 2, hasMore: false, maxSelection: 5,
    ...overrides,
  }
  const knowledge = {
    state,
    visibleLibraries: (state.pendingIds ?? state.libraries.map((item) => item.id)).map((id) => state.libraries.find((item) => item.id === id) ?? library(id, '名称待恢复')),
    save: vi.fn(async () => {}),
    remove: vi.fn(async () => {}),
    reload: vi.fn(async () => {}),
    sendBlock: '',
    search: vi.fn(),
    loadMore: vi.fn(async () => {}),
    ...actions,
  } as unknown as SessionKnowledge
  return { knowledge, markup: renderToStaticMarkup(<KnowledgeSelection knowledge={knowledge} onFocusComposer={() => {}} />) }
}

describe('KnowledgeSelection', () => {
  it('showsOnlySelectedLibrariesInHeader', () => {
    const empty = harness({ libraries: [], pendingIds: null })
    expect(empty.markup).toBe('')
    const { markup } = harness()
    expect(markup).toContain('知识库 2')
    expect(markup).not.toContain('aria-label="清空已选知识库"')
    expect(markup).toContain('data-knowledge-selection')
  })

  it('removesOneWithoutClearAllControl', () => {
    const save = vi.fn(async () => {})
    const remove = vi.fn(async () => {})
    const { knowledge, markup } = harness({}, { save, remove })
    // 顶栏不再提供一键清空；单项删除仍接 remove／save。
    expect(markup).not.toContain('aria-label="清空已选知识库"')
    expect(markup).toContain('aria-label="已选 2 个知识库"')
    expect(typeof knowledge.remove).toBe('function')
    expect(typeof knowledge.save).toBe('function')
  })

  it('keepsFeedbackWhenSelectionEmptyDuringError', () => {
    const { markup } = harness({ libraries: [], pendingIds: null, error: '保存失败', saving: false })
    expect(markup).toContain('保存失败')
    expect(markup).toContain('重试')
  })

  it('countsPendingSelectionInHeader', () => {
    const { markup } = harness({ libraries: [], pendingIds: ['a', 'b', 'c'], saving: true })
    expect(markup).toContain('知识库 3')
    expect(markup).toContain('保存中')
  })

  it('hidesPerLibraryAccessConfirmationCopy', () => {
    const unavailable = {
      ...library('two', '金箍棒和bvvx'),
      unavailableReason: '无法确认访问权限，请重试或移除此知识库',
    }
    const { markup } = harness({ libraries: [unavailable] }, {
      visibleLibraries: [unavailable],
    } as Partial<SessionKnowledge>)
    // 折叠态不渲染条目正文；展开列表也不再展示「无法确认访问权限」字样。
    expect(markup).not.toContain('无法确认访问权限')
    expect(markup).not.toContain('请重试或移除此知识库')
    expect(markup).not.toContain('部分知识库访问权限待确认')
  })
})

import { describe, expect, it } from 'vitest'
import { knowledgeSendBlock, type KnowledgeState } from './knowledge-state'
import { toolLabel, toolVerb } from '@/core/tool-catalog'

function state(partial: Partial<KnowledgeState>): KnowledgeState {
  return {
    sessionId: 's1', contextVersion: 1, revision: 1, libraries: [], pendingIds: null, items: [], previewItems: [],
    mode: 'real', catalogMode: 'real', unavailableReason: '', loading: false, saving: false, listing: false,
    refreshing: false, fromCache: false, catalogUpdatedAt: 0, loaded: true, error: '', listError: '',
    query: '', page: 1, pageSize: 10, total: 0, hasMore: false, maxSelection: 5, ...partial,
  }
}

describe('knowledge tool flow UI contracts', () => {
  it('ordinaryQuestionWorksWithoutKnowledgeService: confirmed selection does not block send', () => {
    expect(knowledgeSendBlock(state({
      libraries: [{ id: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', title: '制度', description: '', path: '', source: '', documentCount: 0, documentCountKnown: false }],
    }))).toBe('')
  })

  it('catalog exposes knowledge_search verb and label', () => {
    expect(toolVerb('knowledge_search')).toContain('检索')
    expect(toolLabel('knowledge_search')).toContain('知识库')
  })
})

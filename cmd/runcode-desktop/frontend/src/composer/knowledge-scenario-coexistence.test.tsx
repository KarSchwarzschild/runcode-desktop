import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { scenariosForProfile } from '@/core/scenario-profiles'
import { visibleScenarios } from '@/core/scenarios'
import { KnowledgeStatus } from './knowledge-picker'
import type { KnowledgeState } from '@/session/knowledge-state'

const base: KnowledgeState = {
  sessionId: 'A', contextVersion: 1, revision: 1,
  libraries: [{ id: 'one', title: '库一', description: '', path: '', source: 'real', documentCount: 1, documentCountKnown: true }],
  pendingIds: null, items: [], previewItems: [], mode: 'real', catalogMode: 'real', unavailableReason: '',
  loading: false, saving: false, listing: false, refreshing: false, fromCache: false, catalogUpdatedAt: 0,
  loaded: true, error: '', listError: '', query: '', page: 1, pageSize: 10, total: 1, hasMore: false, maxSelection: 5,
}

describe('knowledge scenario coexistence', () => {
  it('keepsOriginalScenarioCategoriesWithKnowledge', () => {
    const categories = visibleScenarios(scenariosForProfile('guokai'), {})
    expect(categories.map((item) => item.name)).toEqual(['OA信息查询', '格式校验', '幻灯片'])
    // 选库不改变场景配置；Composer 仅在 readOnlyReason 时清空分类。
    expect(categories).toHaveLength(3)
  })

  it('removesKnowledgeNoticesWithoutEmptyStatus', () => {
    const quiet = renderToStaticMarkup(<KnowledgeStatus state={base} locked={false} onRetry={() => {}} />)
    expect(quiet).toBe('')
    expect(quiet).not.toContain('每次提问重新检索所选知识库')
    expect(quiet).not.toContain('可在输入框中用退格、Delete')
    expect(quiet).not.toContain('仅支持文字，不使用工具、技能或附件')

    const saving = renderToStaticMarkup(<KnowledgeStatus state={{ ...base, saving: true }} locked={false} onRetry={() => {}} />)
    expect(saving).toContain('正在保存知识库选择')
    expect(saving).not.toContain('每次提问重新检索')

    const failed = renderToStaticMarkup(<KnowledgeStatus state={{ ...base, error: '保存失败' }} locked={false} onRetry={() => {}} />)
    expect(failed).toContain('保存失败')
    expect(failed).toContain('重试读取')
  })
})

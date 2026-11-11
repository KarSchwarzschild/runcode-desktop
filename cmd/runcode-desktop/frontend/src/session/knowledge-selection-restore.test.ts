import { describe, expect, it } from 'vitest'
import { KnowledgeController } from './knowledge-state'
import type { KnowledgeBaseSummary, SessionKnowledgeView, SetSessionKnowledgeRequest } from '@/core/bridge'

const library = (id: string, title = `库 ${id}`): KnowledgeBaseSummary => ({
  id, title, description: '', path: '', source: 'demo', documentCount: 0, documentCountKnown: false,
})
const view = (sessionId: string, ids: string[], revision: number, mode = 'real'): SessionKnowledgeView => ({
  sessionId, revision, libraries: ids.map((id) => library(id)), mode, unavailableReason: '',
})

async function waitFor(predicate: () => boolean) {
  for (let i = 0; i < 50; i += 1) {
    if (predicate()) return
    await Promise.resolve()
  }
  throw new Error('condition not met')
}

describe('knowledge selection restore', () => {
  it('restoresLocalSelectionThenRevalidates', async () => {
    let onlineCalls = 0
    let resolveOnline!: (value: SessionKnowledgeView) => void
    const online = new Promise<SessionKnowledgeView>((resolve) => { resolveOnline = resolve })
    const sets: string[][] = []
    const controller = new KnowledgeController({
      snapshot: async (sessionId) => view(sessionId, ['one', 'two'], 4),
      get: async () => {
        onlineCalls += 1
        return online
      },
      list: async () => ({ items: [], page: 1, pageSize: 10, total: 0, hasMore: false, mode: 'real', unavailableReason: '', maxSelection: 5 }),
      set: async (request: SetSessionKnowledgeRequest) => {
        sets.push([...(request.libraryIds ?? [])])
        return view(request.sessionId, request.libraryIds ?? [], (request.expectedRevision ?? 0) + 1)
      },
    })
    const activate = controller.activate('A')
    await waitFor(() => controller.snapshot().libraries.length === 2 && controller.snapshot().loaded)
    expect(controller.snapshot().revision).toBe(4)
    expect(controller.snapshot().loading).toBe(true)
    expect(onlineCalls).toBe(1)
    // 快照已显示期间仍可取消；不因在线未返回而锁死。
    const remove = controller.remove('one')
    await waitFor(() => sets.length === 1)
    resolveOnline(view('A', ['one', 'two'], 4))
    await activate
    await remove
    expect(sets).toEqual([['two']])
    expect(controller.snapshot().libraries.map((item) => item.id)).toEqual(['two'])
    expect(controller.snapshot().revision).toBe(5)
    expect(controller.snapshot().loading).toBe(false)
  })

  it('keepsLocalSelectionWhenOnlineRevalidationFails', async () => {
    const controller = new KnowledgeController({
      snapshot: async (sessionId) => view(sessionId, ['kept'], 2),
      get: async () => { throw new Error('detail timeout') },
      list: async () => ({ items: [], page: 1, pageSize: 10, total: 0, hasMore: false, mode: 'real', unavailableReason: '', maxSelection: 5 }),
      set: async () => { throw new Error('unused') },
    })
    await controller.activate('A')
    expect(controller.snapshot().libraries.map((item) => item.id)).toEqual(['kept'])
    expect(controller.snapshot().revision).toBe(2)
    expect(controller.snapshot().error).toContain('timeout')
    expect(controller.snapshot().loaded).toBe(true)
  })
})

import { describe, expect, it, vi } from 'vitest'
import { KnowledgeController, effectiveKnowledgeIds, knowledgeSendBlock } from './knowledge-state'
import type { KnowledgeBaseSummary, KnowledgePage, SessionKnowledgeView, SetSessionKnowledgeRequest } from '@/core/bridge'

const library = (id: string): KnowledgeBaseSummary => ({ id, title: '同名测试目录', description: `简介 ${id}`, path: `测试目录/${id}`, source: 'demo', documentCount: 12, documentCountKnown: true })
const view = (sessionId: string, ids: string[] = [], revision = 0): SessionKnowledgeView => ({ sessionId, revision, libraries: ids.map(library), mode: 'demo', unavailableReason: '' })
const deferred = <T,>() => {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function backend() {
  const records = new Map<string, SessionKnowledgeView>()
  return {
    records,
    get: async (id: string) => records.get(id) ?? view(id),
    list: async () => ({ items: [library('one'), library('two')], page: 1, pageSize: 10, total: 2, hasMore: false, mode: 'demo', unavailableReason: '', maxSelection: 5 }),
    set: async (request: SetSessionKnowledgeRequest) => {
      const current = records.get(request.sessionId) ?? view(request.sessionId)
      if (request.expectedRevision !== current.revision) throw new Error('revision conflict')
      const ids = [...new Set(request.libraryIds ?? [])]
      if (ids.length > 5) throw new Error('selection limit')
      const next = view(request.sessionId, ids, current.revision + 1)
      records.set(request.sessionId, next)
      return next
    },
  }
}

describe('KnowledgeController', () => {
  it('debounces search for 300ms and ignores responses after closure or another search', async () => {
    vi.useFakeTimers()
    const pending = deferred<KnowledgePage>()
    const queries: string[] = []
    const controller = new KnowledgeController({ ...backend(), list: request => { queries.push(request.query); return pending.promise } })
    try {
      await controller.activate('A')
      controller.queueSearch('a')
      await vi.advanceTimersByTimeAsync(200)
      controller.queueSearch('ab')
      await vi.advanceTimersByTimeAsync(299)
      expect(queries).toEqual([])
      await vi.advanceTimersByTimeAsync(1)
      expect(queries).toEqual(['ab'])
      controller.cancelSearch()
      pending.resolve({ items: [library('late')], page: 1, pageSize: 10, total: -1, hasMore: true, mode: 'real', unavailableReason: '', maxSelection: 5 })
      await pending.promise
      expect(controller.snapshot().items).toEqual([])
      controller.queueSearch('closed')
      controller.cancelSearch()
      await vi.advanceTimersByTimeAsync(300)
      expect(queries).toEqual(['ab'])
    } finally { controller.dispose(); vi.useRealTimers() }
  })
  it('allows confirmed real selections to send while preserving libraries and revision', async () => {
    const controller = new KnowledgeController({ ...backend(), get: async () => ({ ...view('A', ['one'], 7), mode: 'real' }) })
    await controller.activate('A')
    expect(knowledgeSendBlock(controller.snapshot())).toBe('')
    expect(controller.snapshot()).toMatchObject({ revision: 7, libraries: [library('one')] })
    expect(knowledgeSendBlock({ ...controller.snapshot(), error: 'revision conflict' })).toContain('重试')
  })
  it('auto-revalidates retained libraries after a selection change returns placeholder detail', async () => {
    const retained = { ...library('two'), title: '保留的知识库', documentCount: 8, directoryUrl: 'https://ai.ouchn.edu.cn/knowledge-base/two/data-collection' }
    let ids = ['one', 'two'], reads = 0
    const controller = new KnowledgeController({ ...backend(),
      get: async () => { reads++; return { ...view('A', [], Math.max(0, reads - 1)), libraries: ids.map(id => id === 'two' ? retained : library(id)), mode: 'real' } },
      set: async request => {
        ids = request.libraryIds ?? []
        return { ...view('A', [], 1), libraries: [{ id: 'two', title: '不可用的知识库', description: '', path: '', source: '', documentCount: 0, documentCountKnown: false, unavailableReason: '无法确认访问权限，请重试或移除此知识库' }], mode: 'real' }
      },
    })
    await controller.activate('A')
    await controller.save(['two'])
    expect(reads).toBe(2) // Set 占位后自动 Get，无需用户点「重新确认」。
    expect(controller.visibleLibraries()).toEqual([retained])
  })

  it('preserves pagination on a fulfilled unavailable continuation and retries the same page', async () => {
    let fail = true
    const pages: number[] = []
    const controller = new KnowledgeController({ ...backend(), list: async request => {
      pages.push(request.page)
      if (request.page === 2 && fail) return { items: [], page: 2, pageSize: 10, total: 0, hasMore: false, mode: 'unavailable', unavailableReason: '目录服务暂不可用', maxSelection: 5 }
      return { items: [library(request.page === 1 ? 'first' : 'last')], page: request.page, pageSize: 10, total: -1, hasMore: request.page === 1, mode: 'real', unavailableReason: '', maxSelection: 5 }
    } })
    await controller.activate('A')
    await controller.search('')
    await controller.loadMore()
    expect(controller.snapshot()).toMatchObject({ page: 1, total: -1, hasMore: true, mode: 'real', listError: '目录服务暂不可用', listing: false })
    expect(controller.snapshot().items.map(item => item.id)).toEqual(['first'])
    fail = false
    await controller.loadMore()
    expect(pages).toEqual([1, 2, 2])
    expect(controller.snapshot().items.map(item => item.id)).toEqual(['first', 'last'])
    expect(controller.snapshot()).toMatchObject({ page: 2, hasMore: false, listError: '', mode: 'real' })
  })

  it('continues through filtered empty pages using authoritative hasMore with unknown totals', async () => {
    const requested: number[] = []
    const controller = new KnowledgeController({ ...backend(), list: async (request) => {
      requested.push(request.page)
      return { items: request.page === 3 ? [library('authorized')] : [], page: request.page, pageSize: 10, total: -1, hasMore: request.page < 3, mode: 'real', unavailableReason: '', maxSelection: 5 }
    } })
    await controller.activate('A')
    await controller.search('')
    await controller.loadMore()
    await controller.loadMore()
    await controller.loadMore()
    expect(requested).toEqual([1, 2, 3])
    expect(controller.snapshot().items.map(item => item.id)).toEqual(['authorized'])
    expect(controller.snapshot().hasMore).toBe(false)
    expect(controller.snapshot().total).toBe(-1)
  })

  it('keeps an unavailable selected reference non-navigable after catalog refresh and allows removal', async () => {
    const selected = { ...library('revoked'), title: '不可用的知识库', unavailableReason: '无权访问', directoryUrl: undefined }
    const api = backend()
    const restoring = deferred<SessionKnowledgeView>()
    const controller = new KnowledgeController({ ...api,
      get: async () => ({ ...view('A'), libraries: [selected], mode: 'real' }),
      list: async () => ({ ...await api.list(), items: [{ ...selected, unavailableReason: undefined, directoryUrl: 'https://ai.ouchn.edu.cn/knowledge-base/revoked/data-collection' }], hasMore: false, mode: 'real' }),
      set: request => request.libraryIds?.length ? restoring.promise : api.set(request),
    })
    await controller.activate('A')
    await controller.search('')
    expect(controller.visibleLibraries()[0].unavailableReason).toBe('无权访问')
    expect(controller.visibleLibraries()[0].directoryUrl).toBeUndefined()
    expect(knowledgeSendBlock(controller.snapshot())).toBe('')
    expect(knowledgeSendBlock(controller.snapshot())).not.toContain('测试数据')
    await controller.save([])
    expect(controller.visibleLibraries()).toEqual([])
    const undo = controller.save(['revoked'])
    expect(controller.visibleLibraries()[0].unavailableReason).toBe('无权访问')
    expect(controller.visibleLibraries()[0].directoryUrl).toBeUndefined()
    restoring.resolve({ ...view('A', [], 2), libraries: [selected], mode: 'real' })
    await undo
  })

  it('coalesces rapid deletes and never resurrects a locally deleted reference between saves', async () => {
    const api = backend()
    api.records.set('A', view('A', ['one', 'two', 'three'], 4))
    const gates = [deferred<void>(), deferred<void>()]
    const requests: SetSessionKnowledgeRequest[] = []
    const controller = new KnowledgeController({ ...api, set: async (request) => {
      requests.push(request)
      await gates[requests.length - 1].promise
      return api.set(request)
    } })
    await controller.activate('A')
    const saving = controller.save(['two', 'three'])
    const second = controller.save(['three'])
    const latest = controller.save([])
    const visibleSnapshots: string[][] = []
    controller.subscribe(() => visibleSnapshots.push(controller.visibleLibraries().map((item) => item.id)))
    expect(controller.snapshot().pendingIds).toEqual([])
    expect(controller.snapshot().saving).toBe(true)
    expect(controller.visibleLibraries()).toEqual([])
    expect(controller.snapshot().libraries.map((item) => item.id)).toEqual(['one', 'two', 'three'])
    expect(requests.map((request) => request.libraryIds)).toEqual([['two', 'three']])
    gates[0].resolve()
    await vi.waitFor(() => expect(requests).toHaveLength(2))
    expect(controller.snapshot().pendingIds).toEqual([])
    expect(controller.snapshot().saving).toBe(true)
    expect(requests.map((request) => [request.libraryIds, request.expectedRevision])).toEqual([[['two', 'three'], 4], [[], 5]])
    gates[1].resolve()
    await Promise.all([saving, second, latest])
    expect(controller.snapshot().libraries).toEqual([])
    expect(controller.snapshot().pendingIds).toBeNull()
    expect(visibleSnapshots.every((ids) => ids.length === 0)).toBe(true)
    expect(knowledgeSendBlock(controller.snapshot())).toBe('')
  })

  it('restores an undo intent while an earlier delete is saving and keeps its known metadata', async () => {
    const api = backend()
    api.records.set('A', view('A', ['one'], 2))
    const gate = deferred<void>()
    let calls = 0
    const controller = new KnowledgeController({ ...api, set: async (request) => {
      if (calls++ === 0) await gate.promise
      return api.set(request)
    } })
    await controller.activate('A')
    const contextVersion = controller.snapshot().contextVersion
    const deleting = controller.save([])
    const undoing = controller.save(['one'])
    expect(controller.snapshot().pendingIds).toEqual(['one'])
    gate.resolve()
    await Promise.all([deleting, undoing])
    expect(controller.snapshot().libraries.map((item) => item.id)).toEqual(['one'])
    expect(controller.snapshot().revision).toBe(4)
    expect(controller.snapshot().contextVersion).toBe(contextVersion)
    await controller.save([])
    expect(controller.visibleLibraries().map((item) => item.id)).toEqual([])
    const restoring = controller.save(['one'])
    expect(controller.visibleLibraries()).toEqual([library('one')])
    await restoring
  })

  it('rejects an oversized queued edit without replacing the latest valid editor intent', async () => {
    const api = backend()
    api.records.set('A', view('A', ['one', 'two'], 1))
    const gate = deferred<void>()
    const controller = new KnowledgeController({ ...api, set: async (request) => { await gate.promise; return api.set(request) } })
    await controller.activate('A')
    const saving = controller.save(['two'])
    const clearing = controller.save([])
    await controller.save(['one', 'two', 'three', 'four', 'five', 'six'])
    expect(controller.snapshot().pendingIds).toEqual([])
    expect(controller.snapshot().error).toContain('5')
    gate.resolve()
    await Promise.all([saving, clearing])
    expect(controller.snapshot().libraries).toEqual([])
    expect(controller.snapshot().revision).toBe(3)
  })

  it('keeps metadata available to restore a deleted item after a different catalog search', async () => {
    const api = backend()
    const gate = deferred<void>()
    let delay = false
    const controller = new KnowledgeController({ ...api,
      list: async (request) => ({ ...await api.list(), items: request.query ? [library('one')] : [library('one'), library('two')] }),
      set: async (request) => { if (delay) await gate.promise; return api.set(request) },
    })
    await controller.activate('A')
    await controller.search('')
    await controller.save(['two'])
    await controller.save([])
    await controller.search('different page')
    delay = true
    const undoing = controller.save(['two'])
    expect(controller.visibleLibraries()).toEqual([library('two')])
    gate.resolve()
    await undoing
  })

  it('discards queued intent on failure and rereads confirmation without automatically retrying', async () => {
    const api = backend()
    api.records.set('A', view('A', ['one', 'two'], 3))
    const pending = deferred<SessionKnowledgeView>()
    let calls = 0
    const controller = new KnowledgeController({ ...api, set: () => { calls++; return pending.promise } })
    await controller.activate('A')
    const deleting = controller.save(['two'])
    const queued = controller.save([])
    pending.reject(new Error('offline'))
    await Promise.all([deleting, queued])
    expect(calls).toBe(1)
    expect(controller.snapshot().pendingIds).toBeNull()
    expect(controller.snapshot().saving).toBe(false)
    expect(controller.snapshot().error).toContain('offline')
    expect(controller.visibleLibraries().map((item) => item.id)).toEqual(['one', 'two'])
  })

  it('waits for the entire queued A mutation chain before restoring A and leaves B isolated', async () => {
    const api = backend()
    api.records.set('A', view('A', ['one', 'two'], 1))
    const gates = [deferred<void>(), deferred<void>()]
    let calls = 0
    const controller = new KnowledgeController({ ...api, set: async (request) => { await gates[calls++].promise; return api.set(request) } })
    await controller.activate('A')
    const deleting = controller.save(['two'])
    const queued = controller.save([])
    await controller.activate('B')
    expect(controller.snapshot().pendingIds).toBeNull()
    expect(controller.visibleLibraries()).toEqual([])
    const returning = controller.activate('A')
    gates[0].resolve()
    await vi.waitFor(() => expect(calls).toBe(2))
    expect(calls).toBe(2)
    expect(controller.snapshot().loading).toBe(true)
    expect(controller.snapshot().libraries).toEqual([])
    gates[1].resolve()
    await Promise.all([deleting, queued, returning])
    expect(controller.snapshot().libraries).toEqual([])
    expect(controller.snapshot().revision).toBe(3)
  })

  it('invalidates queued account actions and their metadata on account reset', async () => {
    const api = backend()
    api.records.set('A', view('A', ['one', 'two'], 1))
    const pending = deferred<SessionKnowledgeView>()
    let calls = 0
    const controller = new KnowledgeController({ ...api, set: () => { calls++; return pending.promise } })
    await controller.activate('A')
    const contextVersion = controller.snapshot().contextVersion
    const deleting = controller.save(['two'])
    const queued = controller.save([])
    api.records.set('A', view('A', ['new-account'], 0))
    await controller.resetAccount()
    expect(controller.snapshot().contextVersion).toBeGreaterThan(contextVersion)
    pending.resolve(view('A', ['two'], 2))
    await Promise.all([deleting, queued])
    expect(calls).toBe(1)
    expect(controller.snapshot().pendingIds).toBeNull()
    expect(controller.visibleLibraries()).toEqual([library('new-account')])
    expect(controller.snapshot().revision).toBe(0)
  })

  it('restores confirmed selections separately from a sent draft and removes them by ID', async () => {
    const api = backend()
    api.records.set('A', view('A', ['one', 'two'], 7))
    const controller = new KnowledgeController(api)
    await controller.activate('A')
    expect(controller.snapshot().libraries.map((l) => l.id)).toEqual(['one', 'two'])
    expect(knowledgeSendBlock(controller.snapshot())).toContain('暂不参与回答')
    // Clearing or replacing a message draft has no selection-state operation.
    await controller.save(['two'])
    expect(controller.snapshot().libraries.map((l) => l.id)).toEqual(['two'])
    const reopened = new KnowledgeController(api)
    await reopened.activate('A')
    expect(reopened.snapshot().libraries.map((l) => l.id)).toEqual(['two'])
    await reopened.save([])
    expect(knowledgeSendBlock(reopened.snapshot())).toBe('')
  })

  it('ignores stale A responses after A → B → A, including a late save', async () => {
    const first = deferred<SessionKnowledgeView>()
    const saved = deferred<SessionKnowledgeView>()
    const api = backend()
    let aReads = 0
    const controller = new KnowledgeController({ ...api, get: (id) => id === 'A' && aReads++ === 0 ? first.promise : api.get(id), set: () => saved.promise })
    const oldRead = controller.activate('A')
    await controller.activate('B')
    await controller.activate('A')
    const oldSave = controller.save(['one'])
    await controller.activate('B')
    const returnToA = controller.activate('A')
    first.resolve(view('A', ['old-read'], 2))
    saved.resolve(view('A', ['old-save'], 3))
    await Promise.all([oldRead, oldSave, returnToA])
    expect(controller.snapshot().libraries).toEqual([])
    expect(controller.snapshot().sessionId).toBe('A')
    expect(controller.snapshot().saving).toBe(false)
  })

  it('waits for an earlier A mutation before restoring A so the confirmed snapshot catches up', async () => {
    const api = backend()
    const gate = deferred<void>()
    const controller = new KnowledgeController({ ...api, set: async (request) => { await gate.promise; return api.set(request) } })
    await controller.activate('A')
    const saving = controller.save(['one'])
    await controller.activate('B')
    const restoring = controller.activate('A')
    expect(controller.snapshot().loading).toBe(true)
    gate.resolve()
    await Promise.all([saving, restoring])
    expect(controller.snapshot().libraries.map((l) => l.id)).toEqual(['one'])
    expect(controller.snapshot().loading).toBe(false)
  })

  it('keeps the last confirmed selection while saving and on failure, then permits retry', async () => {
    const api = backend()
    api.records.set('A', view('A', ['one'], 4))
    const pending = deferred<SessionKnowledgeView>()
    let fail = true
    const controller = new KnowledgeController({ ...api, set: (request) => fail ? pending.promise : api.set(request) })
    await controller.activate('A')
    const saving = controller.save(['two'])
    expect(controller.snapshot().libraries.map((l) => l.id)).toEqual(['one'])
    expect(knowledgeSendBlock(controller.snapshot())).toContain('保存')
    pending.reject(new Error('offline'))
    await saving
    expect(controller.snapshot().libraries.map((l) => l.id)).toEqual(['one'])
    expect(controller.snapshot().error).toContain('offline')
    fail = false
    await controller.save(['two'])
    expect(controller.snapshot().libraries.map((l) => l.id)).toEqual(['two'])
  })

  it('refreshes authoritative revision after CAS conflict without silently overwriting it', async () => {
    const api = backend()
    const controller = new KnowledgeController(api)
    await controller.activate('A')
    api.records.set('A', view('A', ['two'], 9))
    await controller.save(['one'])
    expect(controller.snapshot().revision).toBe(9)
    expect(controller.snapshot().libraries.map((l) => l.id)).toEqual(['two'])
    expect(controller.snapshot().error).toContain('revision conflict')
    await controller.save(['one'])
    expect(controller.snapshot().revision).toBe(10)
  })

  it('deduplicates IDs, does not save an unchanged set, and enforces the limit', async () => {
    const api = backend()
    const controller = new KnowledgeController(api)
    await controller.activate('A')
    await controller.save(['one', 'one', 'two'])
    expect(controller.snapshot().libraries.map((l) => l.id)).toEqual(['one', 'two'])
    await controller.save(['two', 'one'])
    expect(controller.snapshot().revision).toBe(1)
    await controller.save(['one', 'two', 'three', 'four', 'five'])
    expect(controller.snapshot().libraries).toHaveLength(5)
    expect(controller.snapshot().revision).toBe(2)
    await controller.save(['one', 'two', 'three', 'four', 'five', 'six'])
    expect(controller.snapshot().revision).toBe(2)
    expect(controller.snapshot().libraries).toHaveLength(5)
    expect(controller.snapshot().error).toContain('5')
  })

  it('clears account memory immediately and invalidates loads when disposed', async () => {
    const api = backend()
    api.records.set('A', view('A', ['one'], 1))
    const pending = deferred<SessionKnowledgeView>()
    let delayed = false
    const controller = new KnowledgeController({ ...api, get: (id) => delayed ? pending.promise : api.get(id) })
    await controller.activate('A')
    delayed = true
    const refresh = controller.resetAccount()
    expect(controller.snapshot().libraries).toEqual([])
    expect(controller.snapshot().loading).toBe(true)
    controller.dispose()
    pending.resolve(view('A', ['old-account'], 4))
    await refresh
    expect(controller.snapshot().libraries).toEqual([])
  })

  it('only adopts the latest search and keeps confirmed libraries on another page', async () => {
    const api = backend()
    const oldPage = deferred<Awaited<ReturnType<typeof api.list>>>()
    const controller = new KnowledgeController({ ...api, list: (req) => req.query === 'old' ? oldPage.promise : api.list() })
    await controller.activate('A')
    await controller.save(['three'])
    const first = controller.search('old')
    await controller.search('new')
    oldPage.resolve({ items: [library('old')], page: 1, pageSize: 10, total: 1, hasMore: false, mode: 'demo', unavailableReason: '', maxSelection: 5 })
    await first
    expect(controller.snapshot().items.map((l) => l.id)).toEqual(['one', 'two'])
    expect(controller.snapshot().libraries.map((l) => l.id)).toEqual(['three'])
    expect(controller.snapshot().query).toBe('new')
  })

  it('appends beyond ten without losing visible rows and prevents duplicate in-flight loads', async () => {
    const next = deferred<KnowledgePage>()
    const requested: number[] = []
    const firstItems = Array.from({ length: 10 }, (_, i) => library(String(i + 1)))
    const controller = new KnowledgeController({ ...backend(), list: async (req) => {
      requested.push(req.page)
      return req.page === 1 ? { items: firstItems, page: 1, pageSize: 10, total: 12, hasMore: true, mode: 'demo', unavailableReason: '', maxSelection: 5 } : next.promise
    } })
    await controller.activate('A')
    await controller.search('')
    await controller.save(['1'])
    const loading = controller.loadMore()
    await controller.loadMore()
    expect(controller.snapshot().items).toHaveLength(10)
    expect(requested).toEqual([1, 2])
    next.resolve({ items: [library('10'), library('11'), library('12')], page: 2, pageSize: 10, total: 12, hasMore: false, mode: 'demo', unavailableReason: '', maxSelection: 5 })
    await loading
    expect(controller.snapshot().items.map((item) => item.id)).toEqual(['1', '2', '3', '4', '5', '6', '7', '8', '9', '10', '11', '12'])
    expect(controller.snapshot().libraries.map((item) => item.id)).toEqual(['1'])
    await controller.loadMore()
    expect(requested).toEqual([1, 2])
  })

  it('retains the first page on append failure and retries the same page', async () => {
    let fail = true
    const requested: number[] = []
    const controller = new KnowledgeController({ ...backend(), list: async (req) => {
      requested.push(req.page)
      if (req.page === 2 && fail) throw new Error('connection interrupted')
      return { items: [library(req.page === 1 ? 'first' : 'last')], page: req.page, pageSize: 10, total: 11, hasMore: req.page === 1, mode: 'demo', unavailableReason: '', maxSelection: 5 }
    } })
    await controller.activate('A')
    await controller.search('')
    await controller.loadMore()
    expect(controller.snapshot().items.map((item) => item.id)).toEqual(['first'])
    expect(controller.snapshot().listError).toContain('connection interrupted')
    fail = false
    await controller.loadMore()
    expect(requested).toEqual([1, 2, 2])
    expect(controller.snapshot().items.map((item) => item.id)).toEqual(['first', 'last'])
    expect(controller.snapshot().listError).toBe('')
  })

  it.each(['search', 'session'] as const)('discards an appended page after changing %s', async (change) => {
    const oldPage = deferred<KnowledgePage>()
    const controller = new KnowledgeController({ ...backend(), list: async (req) => req.page === 2 ? oldPage.promise : { items: [library(req.query || req.sessionId)], page: 1, pageSize: 10, total: 11, hasMore: true, mode: 'demo', unavailableReason: '', maxSelection: 5 } })
    await controller.activate('A')
    await controller.search('old')
    const loading = controller.loadMore()
    if (change === 'session') await controller.activate('B')
    await controller.search('new')
    oldPage.resolve({ items: [library('late')], page: 2, pageSize: 10, total: 11, hasMore: false, mode: 'demo', unavailableReason: '', maxSelection: 5 })
    await loading
    expect(controller.snapshot().items.map((item) => item.id)).toEqual(['new'])
    expect(controller.snapshot().page).toBe(1)
    expect(controller.snapshot().listing).toBe(false)
  })

  it('blocks submission while loading or after load failure and explains unavailable mode', async () => {
    const api = backend()
    const pending = deferred<SessionKnowledgeView>()
    const controller = new KnowledgeController({ ...api, get: () => pending.promise })
    const loading = controller.activate('A')
    expect(knowledgeSendBlock(controller.snapshot())).toContain('读取')
    pending.reject(new Error('cannot read'))
    await loading
    expect(knowledgeSendBlock(controller.snapshot())).toContain('重试')
    const unavailable = new KnowledgeController({ ...api, get: async (id) => ({ ...view(id), mode: 'unavailable', unavailableReason: '生产知识库尚未接入' }) })
    await unavailable.activate('A')
    expect(unavailable.snapshot().unavailableReason).toBe('生产知识库尚未接入')
    expect(knowledgeSendBlock(unavailable.snapshot())).toBe('')
  })

  it('reopensFreshCatalogWithoutRequest within 30s and retains snapshot on refresh failure', async () => {
    let now = 1_000
    let lists = 0
    const controller = new KnowledgeController({
      ...backend(),
      list: async () => {
        lists++
        if (lists === 2) return { items: [], page: 1, pageSize: 10, total: 0, hasMore: false, mode: 'unavailable', unavailableReason: '目录服务暂不可用', maxSelection: 5 }
        return { items: [library('one'), library('two')], page: 1, pageSize: 10, total: 2, hasMore: false, mode: 'real', unavailableReason: '', maxSelection: 5 }
      },
    }, { now: () => now })
    await controller.activate('A')
    await controller.openCatalog()
    expect(lists).toBe(1)
    expect(controller.snapshot().items.map((item) => item.id)).toEqual(['one', 'two'])
    controller.closeCatalog()
    now = 1_000 + 29_000
    await controller.openCatalog()
    expect(lists).toBe(1)
    expect(controller.snapshot().fromCache).toBe(true)
    now = 1_000 + 31_000
    await controller.openCatalog()
    expect(lists).toBe(2)
    expect(controller.snapshot().items.map((item) => item.id)).toEqual(['one', 'two'])
    expect(controller.snapshot().listError).toContain('更新失败')
  })

  it('togglesAgainstLatestPendingBeforeRerender', async () => {
    const api = backend()
    const gates = [deferred<void>(), deferred<void>()]
    const requests: SetSessionKnowledgeRequest[] = []
    const controller = new KnowledgeController({ ...api, set: async (request) => {
      requests.push(request)
      await gates[requests.length - 1]?.promise
      return api.set(request)
    } })
    await controller.activate('A')
    const first = controller.toggle('one')
    const second = controller.toggle('two')
    const third = controller.toggle('three')
    expect(effectiveKnowledgeIds(controller.snapshot()).sort()).toEqual(['one', 'three', 'two'])
    expect(controller.snapshot().pendingIds?.sort()).toEqual(['one', 'three', 'two'])
    expect(requests).toHaveLength(1)
    expect(requests[0].libraryIds?.sort()).toEqual(['one'])
    gates[0].resolve()
    await vi.waitFor(() => expect(requests).toHaveLength(2))
    expect(requests[1].libraryIds?.sort()).toEqual(['one', 'three', 'two'])
    expect(requests[1].expectedRevision).toBe(1)
    gates[1].resolve()
    await Promise.all([first, second, third])
    expect(controller.snapshot().libraries.map((item) => item.id).sort()).toEqual(['one', 'three', 'two'])
    expect(controller.snapshot().pendingIds).toBeNull()
    expect(controller.snapshot().saving).toBe(false)
  })

  it('doesNotResurrectRemovedPendingSelection', async () => {
    const api = backend()
    const gate = deferred<void>()
    let sets = 0
    const controller = new KnowledgeController({ ...api, set: async (request) => {
      sets += 1
      if (sets === 1) await gate.promise
      return api.set(request)
    } })
    await controller.activate('A')
    const adding = controller.toggle('one')
    expect(controller.snapshot().pendingIds).toEqual(['one'])
    const clearing = controller.toggle('one')
    void controller.toggle('two')
    expect(controller.snapshot().pendingIds).toEqual(['two'])
    gate.resolve()
    await Promise.all([adding, clearing])
    expect(controller.snapshot().libraries.map((item) => item.id)).toEqual(['two'])
    expect(controller.snapshot().pendingIds).toBeNull()
  })

  it('enforcesLimitAgainstPendingIds', async () => {
    const api = backend()
    const controller = new KnowledgeController(api)
    await controller.activate('A')
    await controller.save(['one', 'two', 'three', 'four', 'five'])
    const before = controller.snapshot().revision
    await controller.toggle('six')
    expect(controller.snapshot().revision).toBe(before)
    expect(controller.snapshot().error).toContain('最多选择')
    expect(effectiveKnowledgeIds(controller.snapshot())).toEqual(['one', 'two', 'three', 'four', 'five'])
    await controller.toggle('five')
    expect(effectiveKnowledgeIds(controller.snapshot())).toEqual(['one', 'two', 'three', 'four'])
    await controller.toggle('six')
    expect(controller.snapshot().libraries.map((item) => item.id).sort()).toEqual(['four', 'one', 'six', 'three', 'two'])
  })

  it('rereadsActualDiskAfterTimeoutEvenIfServerAlreadyWrote', async () => {
    const api = backend()
    api.records.set('A', view('A', ['one'], 4))
    const pending = deferred<SessionKnowledgeView>()
    let sets = 0
    const controller = new KnowledgeController({
      ...api,
      set: async (request) => {
        sets += 1
        if (sets === 1) {
          // 服务端已写入，但客户端收到超时；失败路径必须重读真实结果，不能假定仍是 one。
          api.records.set('A', view('A', ['two'], 5))
          return pending.promise
        }
        return api.set(request)
      },
    })
    await controller.activate('A')
    const saving = controller.save(['two'])
    pending.reject(new Error('timeout'))
    await saving
    expect(controller.snapshot().libraries.map((item) => item.id)).toEqual(['two'])
    expect(controller.snapshot().revision).toBe(5)
    expect(controller.snapshot().pendingIds).toBeNull()
    expect(controller.snapshot().error).toContain('timeout')
    expect(knowledgeSendBlock(controller.snapshot())).toContain('重试')
  })
})

// @vitest-environment jsdom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { Events, type Envelope, type SessionInfo, type StartSessionRequest } from '@/core/bridge'
import { useSession } from './use-session'

const native = vi.hoisted(() => ({ listeners: new Map<string, (event: Envelope<unknown>) => void>(), copies: [] as string[], titles: new Map<string, string>(), delayedLists: null as Promise<unknown[]> | null }))
vi.mock('@/core/bridge', async importOriginal => ({
  ...await importOriginal<typeof import('@/core/bridge')>(),
  onEnvelope: (name: string, fn: (event: Envelope<unknown>) => void) => { native.listeners.set(name, fn); return () => native.listeners.delete(name) },
  loadConfig: async () => ({}),
  openSessions: async () => native.delayedLists ?? [{ sessionId: 'K', kind: 'knowledge' }, { sessionId: 'A', kind: 'agent' }],
  listSessions: async () => native.delayedLists ?? [{ id: 'K', kind: 'knowledge', title: '私有标题' }],
  startSession: async () => ({ sessionId: 'K', kind: 'knowledge', model: 'platform' }),
  renameKnowledgeChat: async (id: string, title: string) => { native.titles.set(id, title) },
  exportKnowledgeChat: async () => '提问：原问\n回答：已保存的回答\n状态：completed',
  copyText: async (text: string) => { native.copies.push(text) },
}))
describe('knowledge session visibility', () => {
  it('ignores both old-account list responses after account invalidation', async () => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    let resolve!: (rows: unknown[]) => void
    native.delayedLists = new Promise(yes => { resolve = yes })
    let app!: ReturnType<typeof useSession>
    function Harness() { app = useSession({ busy: false, conversation: { reset: () => {}, applyResumed: async () => {}, pushError: () => {}, dropSession: () => {} }, showToast: () => {}, onEnterChat: () => {} }); return null }
    const root = createRoot(document.createElement('div'))
    try {
      await act(async () => root.render(<Harness />))
      await act(async () => app.start({} as StartSessionRequest))
      await act(async () => native.listeners.get(Events.PassportChanged)?.({ payload: { loggedIn: false } } as Envelope<unknown>))
      await act(async () => resolve([{ id: 'K', sessionId: 'K', kind: 'knowledge', title: '旧账号私有标题' }]))
      expect(app.recents).toEqual([])
      expect(app.openList).toEqual([])
    } finally { native.delayedLists = null; await act(async () => root.unmount()) }
  })
  it('renames knowledge sessions and exports only the host-authorized saved transcript', async () => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    native.copies = []; native.titles.clear()
    let app!: ReturnType<typeof useSession>
    function Harness() { app = useSession({ busy: false, conversation: { reset: () => {}, applyResumed: async () => {}, pushError: () => {}, dropSession: () => {} }, showToast: () => {}, onEnterChat: () => {} }); return null }
    const root = createRoot(document.createElement('div'))
    try {
      await act(async () => root.render(<Harness />))
      await act(async () => app.renameKnowledge('K', '新的问答标题'))
      expect(app.titles.K).toBe('新的问答标题')
      expect(native.titles.get('K')).toBe('新的问答标题')
      await act(async () => app.exportKnowledge('K'))
      expect(native.copies).toEqual(['提问：原问\n回答：已保存的回答\n状态：completed'])
    } finally { await act(async () => root.unmount()) }
  })
  it('clears all knowledge history and drafts on account changes while retaining ordinary sessions', async () => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    native.listeners.clear()
    let app!: ReturnType<typeof useSession>
    const dropped: string[] = [], drafts: string[] = []
    function Harness() {
      app = useSession({ busy: false, conversation: { reset: () => {}, applyResumed: async () => {}, pushError: () => {}, dropSession: id => dropped.push(id) }, showToast: () => {}, onEnterChat: () => {}, onKnowledgeInvalidated: ids => drafts.push(...ids) })
      return <output>{app.info?.sessionId}{app.recents.map(item => item.title).join(',')}</output>
    }
    const host = document.createElement('div'), root = createRoot(host)
    try {
      await act(async () => root.render(<Harness />))
      await act(async () => app.start({} as StartSessionRequest))
      expect(host.textContent).toContain('私有标题')
      await act(async () => native.listeners.get(Events.PassportChanged)?.({ payload: { loggedIn: false } } as Envelope<unknown>))
      expect(app.info).toBeNull()
      expect(app.started).toBe(false)
      expect(app.openList.map(item => item.sessionId)).toEqual(['A'])
      expect(app.recents).toEqual([])
      expect(dropped).toContain('K')
      expect(drafts).toContain('K')
      expect(host.textContent).not.toContain('私有标题')
    } finally { await act(async () => root.unmount()) }
  })
  it('does not focus a background session when its status changes', async () => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    let app!: ReturnType<typeof useSession>
    function Harness() { app = useSession({ busy: false, conversation: { reset: () => {}, applyResumed: async () => {}, pushError: () => {}, dropSession: () => {} }, showToast: () => {}, onEnterChat: () => {} }); return null }
    const root = createRoot(document.createElement('div'))
    try {
      await act(async () => root.render(<Harness />))
      await act(async () => app.start({} as StartSessionRequest))
      await act(async () => native.listeners.get(Events.SessionStatus)?.({ sessionId: 'A', payload: { sessionId: 'A', model: 'other' } as SessionInfo } as Envelope<unknown>))
      expect(app.info?.sessionId).toBe('K')
    } finally { await act(async () => root.unmount()) }
  })
})

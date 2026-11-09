// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { useChatDrafts } from './use-chat-drafts'
import { useKnowledgeMode } from './use-knowledge-mode'
import type { SessionKnowledge } from './use-knowledge'
import type { SessionInfo } from '@/core/bridge'

let root: Root, host: HTMLDivElement
let info: SessionInfo, history: boolean, created: string[][], saved: string[][], notices: string[]
let app: { mode: ReturnType<typeof useKnowledgeMode>; drafts: ReturnType<typeof useChatDrafts> }
function Harness() {
  const drafts = useChatDrafts('A')
  const knowledge = { state: { sessionId: 'A', libraries: [], loaded: true }, save: async (ids: string[]) => { saved.push(ids) } } as unknown as SessionKnowledge
  const mode = useKnowledgeMode({ info, knowledge, drafts, hasHistory: history, notify: text => notices.push(text),
    create: async (_source: string, ids: string[], onCreated: (id: string) => void) => { created.push(ids); onCreated('K'); return true },
  })
  app = { mode, drafts }
  return null
}
beforeEach(async () => {
  ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  info = { sessionId: 'A', kind: 'agent' } as SessionInfo
  history = false; created = []; saved = []; notices = []
  host = document.createElement('div'); document.body.append(host); root = createRoot(host)
  await act(async () => root.render(<Harness />))
})
afterEach(async () => { await act(async () => root.unmount()); host.remove() })
describe('knowledge mode creation', () => {
  it('saves into the same agent session on first select without creating another chat', async () => {
    await act(async () => app.drafts.setInput('未发送的原文'))
    await act(async () => app.mode.knowledge.save(['one']))
    expect(created).toEqual([])
    expect(saved).toEqual([['one']])
    expect(app.drafts.read('A').text).toBe('未发送的原文')
  })
  it('existingHistoryAlsoAllowsFirstMultiSelect', async () => {
    history = true
    await act(async () => { root.render(<Harness />); app.drafts.setInput('草稿') })
    await act(async () => app.mode.knowledge.save(['one']))
    expect(app.mode.confirming).toBe(false)
    expect(created).toEqual([])
    expect(saved).toEqual([['one']])
    expect(app.drafts.read('A').text).toBe('草稿')
  })
  it('keeps a knowledge session in the same mode after every library is removed', async () => {
    info = { ...info, kind: 'knowledge' }
    await act(async () => root.render(<Harness />))
    await act(async () => app.mode.knowledge.save([]))
    expect(saved).toEqual([[]])
    expect(created).toEqual([])
    expect(app.mode.isKnowledge).toBe(true)
  })
})

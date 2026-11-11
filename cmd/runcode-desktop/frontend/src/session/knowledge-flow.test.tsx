// @vitest-environment jsdom
import { act, createRef } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { EventName, Envelope, QuestionDraft, QuestionReceipt, SessionInfo, SubmitQuestionRequest } from '@/core/bridge'
import { Events } from '@/core/bridge'
import { useConversation } from './use-conversation'
import { useChatDrafts } from './use-chat-drafts'
import { useQuestionRetry } from './use-question-retry'
import { KnowledgeController } from './knowledge-state'
import type { Session } from './use-session'
import type { SessionKnowledge } from './use-knowledge'

const native = vi.hoisted(() => ({
  listeners: new Map<string, (env: Envelope<unknown>) => void>(),
  requests: [] as SubmitQuestionRequest[],
  question: null as QuestionDraft | null,
  receipt: null as Promise<QuestionReceipt> | null,
  forkKind: 'agent',
  editReads: [] as string[],
}))
vi.mock('@/core/bridge', async importOriginal => ({
  ...await importOriginal<typeof import('@/core/bridge')>(),
  onEnvelope: (event: string, callback: (env: Envelope<unknown>) => void) => {
    native.listeners.set(event, callback)
    return () => { native.listeners.delete(event) }
  },
  submitQuestion: async (request: SubmitQuestionRequest) => {
    native.requests.push(request)
    return native.receipt ?? { questionId: 'q-new', startedTurn: true }
  },
  getQuestion: async () => native.question,
  forkQuestion: async () => ({ info: { sessionId: 'branch', kind: native.forkKind }, blocks: [] }),
  getSessionKnowledge: async () => ({ sessionId: 'branch', revision: 17, libraries: [], mode: 'real' }),
  listEdits: async (id: string) => { native.editReads.push(id); return [] }, interrupt: async () => {},
}))

const library = { id: 'frozen', title: '原题知识库', description: '', path: '', source: '', documentCount: 1, documentCountKnown: true }
const original: QuestionDraft = { source: { sessionId: 'A', questionId: 'q-old' }, text: '原问题', images: [], knowledge: { libraries: [library], status: 'ready', applied: true, sources: [] } }
const noop = () => {}
let host: HTMLDivElement, root: Root
let app: { conversation: ReturnType<typeof useConversation>; drafts: ReturnType<typeof useChatDrafts>; retry: ReturnType<typeof useQuestionRetry> }
let focused = 'A'
let mode = 'agent'
let knowledge: SessionKnowledge
let controller: KnowledgeController
let branchLibraries = [library]
function emit(event: EventName, sessionId: string, payload: unknown) { native.listeners.get(event)?.({ sessionId, payload } as Envelope<unknown>) }
function Harness() {
  const drafts = useChatDrafts(focused)
  const conversation = useConversation({ focusedId: focused, infoRef: { current: { sessionId: focused, kind: mode } as SessionInfo }, permissions: { pending: null, enqueue: noop, clear: noop }, onFilesChanged: noop, onOpenPreview: noop, showToast: noop })
  const session = { captureSwitch: () => () => true, refreshOpen: async () => {}, runQuestionBranch: async (_r: unknown, current: () => boolean, submit: (id: string) => Promise<boolean>) => current() && submit('branch') } as unknown as Session
  const retry = useQuestionRetry({ sessionId: focused, blocked: conversation.busy, drafts, conversation, session, textarea: createRef(), notify: noop, knowledge })
  app = { conversation, drafts, retry }
  return null
}
beforeEach(async () => {
  ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  native.listeners.clear(); native.requests = []; native.question = original; native.receipt = null; native.forkKind = 'agent'; focused = 'A'; mode = 'agent'
  branchLibraries = [library]; native.editReads = []
  host = document.createElement('div'); document.body.append(host); root = createRoot(host)
  controller = new KnowledgeController({ get: async sessionId => ({ sessionId, revision: 9, libraries: sessionId === 'branch' ? branchLibraries : [{ ...library, id: 'current' }], mode: 'real', unavailableReason: '' }), list: async () => ({ items: [library], page: 1, pageSize: 10, total: 1, hasMore: false, mode: 'real', unavailableReason: '', maxSelection: 5 }), set: async () => { throw new Error('Retry must never mutate source selection') } })
  await controller.activate('A')
  knowledge = { state: controller.snapshot(), visibleLibraries: controller.visibleLibraries(), save: ids => controller.save(ids), search: noop, loadMore: noop, reload: noop, sendBlock: '' }
  await act(async () => root.render(<Harness />))
})
afterEach(async () => { await act(async () => root.unmount()); host.remove() })

describe('live knowledge question flow', () => {
  it('does not recreate a closed knowledge conversation when its receipt arrives late', async () => {
    mode = 'knowledge'
    await act(async () => root.render(<Harness />))
    let resolve!: (receipt: QuestionReceipt) => void
    native.receipt = new Promise(yes => { resolve = yes })
    let submitted!: Promise<boolean>
    await act(async () => { submitted = app.conversation.send('关闭前的草稿', [], undefined, 9) })
    await act(async () => app.conversation.dropSession('A'))
    await act(async () => { resolve({ questionId: 'late', startedTurn: true }); expect(await submitted).toBe(false) })
    expect(app.conversation.blocks).toEqual([])
    expect(app.conversation.busy).toBe(false)
  })
  it.each(['before', 'after'])('keeps an early host stop final when the first event arrives %s receipt', async ordering => {
    let resolve!: (receipt: QuestionReceipt) => void
    native.receipt = new Promise(yes => { resolve = yes })
    let submitted!: Promise<boolean>
    await act(async () => { submitted = app.conversation.send('提前停止', [], undefined, 9) })
    await act(async () => app.conversation.stop())
    const event = () => emit(Events.KnowledgeChat, 'A', { sessionId: 'A', questionId: 'host-stop', runId: 'run', seq: 1, phase: 'delta', text: '应丢弃' })
    if (ordering === 'before') await act(async () => event())
    await act(async () => { resolve({ questionId: 'host-stop', startedTurn: true }); await submitted })
    if (ordering === 'after') await act(async () => event())
    expect(app.conversation.busy).toBe(false)
    expect(app.conversation.blocks.some(b => b.kind === 'assistant')).toBe(false)
  })
  it('restores interrupted knowledge questions without consulting engine edits or executing them', async () => {
    await act(async () => { await app.conversation.applyResumed({ info: { sessionId: 'K', kind: 'knowledge' } as SessionInfo, contextTokens: 0, blocks: [
      { kind: 'user', questionId: 'saved-q', text: '原问', knowledge: { libraries: [], status: 'interrupted', applied: false, sources: [] } },
      { kind: 'assistant', text: '保存的部分回答' },
    ] }, () => false); focused = 'K'; root.render(<Harness />) })
    expect(native.editReads).toEqual([])
    expect(app.conversation.blocks[0]).toMatchObject({ questionId: 'saved-q', knowledge: { status: 'interrupted' } })
    expect(app.conversation.busy).toBe(false)
    expect(native.requests).toEqual([])
  })
  it('reads the fork selection revision and submits a knowledge retry with a new UUID and explicit empty override', async () => {
    native.forkKind = 'knowledge'
    await act(async () => { await app.retry.edit('q-old') })
    await act(async () => { await app.retry.knowledge.save([]) })
    await act(async () => { await app.retry.send('重新检索', []) })
    expect(native.requests[0]).toMatchObject({ sessionId: 'branch', knowledgeRevision: 17, knowledgeOverride: { libraryIds: [] }, source: original.source, allowSteering: false })
    expect(native.requests[0].submissionId).toMatch(/^[0-9a-f]{8}-[0-9a-f-]{27}$/)
  })
  it('gives knowledge submissions an idempotency UUID and blocks attachments and steering at IPC admission', async () => {
    mode = 'knowledge'
    await act(async () => root.render(<Harness />))
    await act(async () => { expect(await app.conversation.send('图片', ['photo.png'], undefined, 9)).toBe(false) })
    expect(native.requests).toEqual([])
    await act(async () => { expect(await app.conversation.send('文字', [], undefined, 9)).toBe(true) })
    expect(native.requests[0].submissionId).toMatch(/^[0-9a-f]{8}-[0-9a-f-]{27}$/)
    expect(native.requests[0].allowSteering).toBe(false)
    await act(async () => { expect(await app.conversation.send('补充', [], undefined, 9)).toBe(false) })
    expect(native.requests).toHaveLength(1)
  })
  it('buffers host stream before receipt, isolates A/B and rejects duplicate, stale-run and terminal-late output', async () => {
    let resolve!: (receipt: QuestionReceipt) => void
    native.receipt = new Promise(yes => { resolve = yes })
    let submitted!: Promise<boolean>
    await act(async () => { submitted = app.conversation.send('宿主原问', [], undefined, 9) })
    const hostEvent = (seq: number, phase: string, fields = {}) => emit(Events.KnowledgeChat, 'A', { sessionId: 'A', questionId: 'host-q', runId: 'run-1', seq, phase, ...fields })
    await act(async () => {
      hostEvent(1, 'accepted'); hostEvent(2, 'generating', { sources: [] }); hostEvent(3, 'delta', { text: '回答' })
      hostEvent(3, 'delta', { text: '重复' }); hostEvent(4, 'delta', { text: '旧运行', runId: 'other' })
      focused = 'B'; root.render(<Harness />)
    })
    expect(app.conversation.blocks).toEqual([])
    await act(async () => {
      hostEvent(4, 'completed', { text: '回答完成', inputTokens: 12, outputTokens: 6, truncated: true })
      hostEvent(5, 'delta', { text: '迟到' })
      resolve({ questionId: 'host-q', startedTurn: true }); await submitted
      focused = 'A'; root.render(<Harness />)
    })
    expect(app.conversation.busy).toBe(false)
    expect(app.conversation.blocks[0]).toMatchObject({ kind: 'user', questionId: 'host-q', text: '宿主原问' })
    expect(app.conversation.blocks.find(b => b.kind === 'assistant')).toMatchObject({ text: '回答完成', streaming: false })
    expect(app.conversation.blocks.find(b => b.kind === 'usage')).toMatchObject({ inTok: 12, outTok: 6 })
    expect(app.conversation.blocks.find(b => b.kind === 'notice')).toMatchObject({ text: expect.stringContaining('输出上限') })
  })
  it('stops host output and ignores events after the session has closed', async () => {
    await act(async () => { await app.conversation.send('宿主问题', [], undefined, 9) })
    await act(async () => { emit(Events.KnowledgeChat, 'A', { sessionId: 'A', questionId: 'q-new', runId: 'run', seq: 1, phase: 'delta', text: '部分' }) })
    await act(async () => app.conversation.stop())
    await act(async () => { emit(Events.KnowledgeChat, 'A', { sessionId: 'A', questionId: 'q-new', runId: 'run', seq: 2, phase: 'delta', text: '迟到' }) })
    expect(app.conversation.blocks.find(b => b.kind === 'assistant')).toMatchObject({ text: '部分', streaming: false })
    expect(app.conversation.busy).toBe(false)
    await act(async () => app.conversation.dropSession('A'))
    await act(async () => { emit(Events.KnowledgeChat, 'A', { sessionId: 'A', questionId: 'q-new', runId: 'run', seq: 3, phase: 'delta', text: '关闭后' }) })
    expect(app.conversation.blocks).toEqual([])
  })
  it('reconciles real subscribed events before receipt across focused sessions and ends busy on turn failure', async () => {
    let resolve!: (receipt: QuestionReceipt) => void
    native.receipt = new Promise(yes => { resolve = yes })
    let submitted!: Promise<boolean>
    await act(async () => { submitted = app.conversation.submitFor({ sessionId: 'A', text: '原问题', imagePaths: [], originalImages: [], allowSteering: false }) })
    await act(async () => { focused = 'B'; root.render(<Harness />) })
    const failed = { ...original.knowledge!, applied: false, status: 'failed', sources: [] }
    await act(async () => {
      emit(Events.KnowledgeContext, 'A', { questionId: 'q-accepted', knowledge: failed })
      emit(Events.TurnError, 'A', { error: '检索失败' })
      resolve({ questionId: 'q-accepted', startedTurn: true }); await submitted
    })
    expect(app.conversation.blocks).toEqual([])
    await act(async () => { focused = 'A'; root.render(<Harness />) })
    expect(app.conversation.blocks[0]).toMatchObject({ questionId: 'q-accepted', knowledge: failed, text: '原问题' })
    expect(app.conversation.busy).toBe(false)
  })
  it('sends the confirmed normal revision, clears only the sent draft and retains session selection', async () => {
    await act(async () => app.drafts.setInput('普通问题'))
    await act(async () => { await app.retry.send('普通问题', []) })
    expect(native.requests[0]).toMatchObject({ sessionId: 'A', text: '普通问题', knowledgeRevision: 9 })
    expect(app.drafts.read('A').text).toBe('')
    expect(controller.snapshot().libraries.map(item => item.id)).toEqual(['current'])
  })
  it('stops a preparing question immediately and does not revive its loader on a late ready event', async () => {
    await act(async () => { await app.conversation.send('原问题', [], undefined, 9) })
    await act(async () => { emit(Events.KnowledgeContext, 'A', { questionId: 'q-new', knowledge: { ...original.knowledge!, status: 'pending', applied: false } }) })
    await act(async () => app.conversation.stop())
    expect(app.conversation.blocks[0]).toMatchObject({ knowledge: { status: 'cancelled', applied: false } })
    expect(app.conversation.busy).toBe(false)
    await act(async () => { emit(Events.KnowledgeContext, 'A', { questionId: 'q-new', knowledge: { ...original.knowledge!, status: 'ready', applied: false } }) })
    expect(app.conversation.blocks[0]).toMatchObject({ knowledge: { status: 'cancelled' } })
  })
  it.each(['before', 'after'])('keeps an early stop final when the first knowledge event arrives %s its late receipt, while allowing the next question', async ordering => {
    let resolve!: (receipt: QuestionReceipt) => void
    native.receipt = new Promise(yes => { resolve = yes })
    let submitted!: Promise<boolean>
    await act(async () => { submitted = app.conversation.send('停止的原题', [], undefined, 9) })
    await act(async () => app.conversation.stop())
    const pending = { ...original.knowledge!, status: 'pending', applied: false, sources: [] }
    if (ordering === 'before') await act(async () => { emit(Events.KnowledgeContext, 'A', { questionId: 'q-stopped', knowledge: pending }) })
    await act(async () => { resolve({ questionId: 'q-stopped', startedTurn: true }); await submitted })
    if (ordering === 'after') await act(async () => { emit(Events.KnowledgeContext, 'A', { questionId: 'q-stopped', knowledge: pending }) })
    expect(app.conversation.blocks[0]).toMatchObject({ questionId: 'q-stopped', knowledge: { status: 'cancelled', applied: false } })
    expect(app.conversation.busy).toBe(false)
    native.receipt = null
    await act(async () => { await app.conversation.send('下一题', [], undefined, 9) })
    await act(async () => { emit(Events.KnowledgeContext, 'A', { questionId: 'q-new', knowledge: pending }) })
    expect(app.conversation.blocks[1]).toMatchObject({ questionId: 'q-new', text: '下一题', knowledge: { status: 'pending' } })
    expect(app.conversation.busy).toBe(true)
  })
  it('edits frozen references locally, sends explicit empty override to a branch and restores the source draft', async () => {
    await act(async () => app.drafts.setInput('保留草稿'))
    await act(async () => { await app.retry.edit('q-old') })
    expect(app.retry.knowledge.visibleLibraries).toEqual([library])
    await act(async () => { await app.retry.knowledge.save([]) })
    expect(app.retry.knowledge.visibleLibraries).toEqual([])
    expect(controller.snapshot().libraries.map(item => item.id)).toEqual(['current'])
    await act(async () => { await app.retry.send('修改问题', []) })
    expect(native.requests[0]).toMatchObject({ sessionId: 'branch', text: '修改问题', source: original.source, knowledgeOverride: { libraryIds: [] } })
    expect(app.drafts.read('A').text).toBe('保留草稿')
  })
  it('retries with no override despite a different current selection', async () => {
    await act(async () => { await app.retry.retry('q-old') })
    expect(native.requests[0].knowledgeOverride).toBeUndefined()
    expect(native.requests[0].source).toEqual(original.source)
    expect(controller.snapshot().libraries.map(item => item.id)).toEqual(['current'])
  })
  it('refreshes the active child selection after the backend accepts an edited retry override', async () => {
    await act(async () => { await app.retry.edit('q-old') })
    await act(async () => { await app.retry.knowledge.save([]) })
    let resolve!: (receipt: QuestionReceipt) => void
    native.receipt = new Promise(yes => { resolve = yes })
    let submitted!: Promise<boolean>
    await act(async () => { submitted = app.retry.send('修改问题', []) })
    await controller.activate('branch') // Initial fork selection is the frozen original.
    await act(async () => { knowledge = { ...knowledge, state: controller.snapshot(), visibleLibraries: controller.visibleLibraries(), reload: () => { void controller.activate('branch') } }; root.render(<Harness />) })
    branchLibraries = [] // Accepted override becomes the child's continuing selection.
    await act(async () => { resolve({ questionId: 'q-new', startedTurn: true }); await submitted })
    expect(controller.snapshot().libraries).toEqual([])
  })
  it('restores a removed retry reference on editor undo after a catalog search and sends only its ID', async () => {
    const selected = { ...library, id: 'new-selection', title: '新增知识库' }
    await act(async () => { await app.retry.edit('q-old') })
    await act(async () => { knowledge = { ...knowledge, state: { ...knowledge.state, items: [selected] } }; root.render(<Harness />) })
    await act(async () => { await app.retry.knowledge.save(['new-selection']) })
    await act(async () => { await app.retry.knowledge.save([]) })
    await act(async () => { knowledge = { ...knowledge, state: { ...knowledge.state, items: [] } }; root.render(<Harness />) })
    await act(async () => { await app.retry.knowledge.save(['new-selection']) })
    expect(app.retry.knowledge.visibleLibraries).toEqual([selected])
    await act(async () => { await app.retry.send('修改问题', []) })
    expect(native.requests[0].knowledgeOverride).toEqual({ libraryIds: ['new-selection'] })
    expect(native.requests[0].text).toBe('修改问题')
    expect(controller.snapshot().libraries.map(item => item.id)).toEqual(['current'])
  })
  it('cancels edits without changing the saved draft or source selection', async () => {
    await act(async () => app.drafts.setInput('保留草稿'))
    await act(async () => { await app.retry.edit('q-old') })
    await act(async () => { await app.retry.knowledge.save([]); app.retry.cancel() })
    expect(app.drafts.read('A').text).toBe('保留草稿')
    expect(controller.snapshot().revision).toBe(9)
    expect(native.requests).toEqual([])
  })
  it('does not overwrite a newer draft when an accepted retry receipt arrives late', async () => {
    await act(async () => { await app.retry.edit('q-old') })
    let resolve!: (receipt: QuestionReceipt) => void
    native.receipt = new Promise(yes => { resolve = yes })
    let submitted!: Promise<boolean>
    await act(async () => { submitted = app.retry.send('修改问题', []) })
    await act(async () => { app.drafts.setInput('更新草稿'); resolve({ questionId: 'q-new', startedTurn: true }); await submitted })
    expect(app.drafts.read('A').text).toBe('更新草稿')
  })
})

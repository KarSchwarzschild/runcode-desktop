import { describe, expect, it } from 'vitest'
import { bindQuestionReceipt, withQuestionKnowledge, reconcileQuestionBlocks } from './conversation-state'
import { resumedQuestionBlock } from '@/chat/blocks'
import { type ConvMap, emptyConversation, patchConv } from './conversation-state'

const prepared = { libraries: [], status: 'ready', applied: false, sources: [] }
const user = { kind: 'user' as const, id: 'optimistic', text: 'original', ts: '' }

describe('question knowledge reconciliation', () => {
  it('retains an event before receipt and binds it only to its exact session and row', () => {
    let map: ConvMap = { A: { ...emptyConversation, blocks: [user] }, B: { ...emptyConversation, blocks: [{ ...user, id: 'other' }] } }
    map = patchConv(map, 'A', s => withQuestionKnowledge(s, { questionId: 'q1', knowledge: prepared }))
    map = patchConv(map, 'A', s => bindQuestionReceipt(s, 'optimistic', 'q1'))
    expect(map.A.blocks[0]).toMatchObject({ questionId: 'q1', knowledge: prepared, text: 'original' })
    expect(map.B.blocks[0]).not.toHaveProperty('knowledge')
    expect(map.A.busy).toBe(false)
  })
  it('keeps failed accepted questions retryable without removing text or changing unrelated rows', () => {
    let state = bindQuestionReceipt({ ...emptyConversation, blocks: [user, { ...user, id: 'new' }], busy: true }, 'optimistic', 'q')
    const failed = { ...prepared, status: 'failed', error: '检索失败，请重试' }
    state = withQuestionKnowledge(state, { questionId: 'q', knowledge: failed })
    expect(state.blocks[0]).toMatchObject({ questionId: 'q', knowledge: failed, text: 'original' })
    expect(state.blocks[1]).toEqual({ ...user, id: 'new' })
    expect(state.busy).toBe(true) // Only turn:end/error owns busy completion.
  })
  it('reconstructs the same user metadata from history and supports legacy records', () => {
    const knowledge = { ...prepared, applied: true, sources: [{ id: '1', libraryId: 'lib', documentId: 'doc', chunkId: 'chunk', title: '文档' }] }
    const resumed = resumedQuestionBlock({ kind: 'user', questionId: 'q', text: 'original', knowledge }, 'history')
    expect(resumed).toEqual({ ...user, id: 'history', questionId: 'q', knowledge, attachments: undefined })
    expect(resumedQuestionBlock({ kind: 'user', text: 'old' }, 'legacy')).toMatchObject({ text: 'old', knowledge: undefined })
  })
  it('retains newer live knowledge if a slower resume snapshot arrives afterwards', () => {
    const live = { ...prepared, applied: true, sources: [{ id: '1', libraryId: 'lib', documentId: 'doc', chunkId: 'chunk', title: '文档' }] }
    const state = withQuestionKnowledge(emptyConversation, { questionId: 'q', knowledge: live })
    const blocks = reconcileQuestionBlocks(state, [resumedQuestionBlock({ kind: 'user', questionId: 'q', text: 'original', knowledge: prepared }, 'history')])
    expect(blocks[0]).toMatchObject({ knowledge: live, questionId: 'q', text: 'original' })
  })
})

/** @vitest-environment jsdom */
import { describe, expect, it, beforeEach } from 'vitest'
import type { Block } from './blocks'
import {
  formatImageGenerationNote, loadImageGenerationNoteMap, rehydrateAssistantNotes,
  saveImageGenerationNote, stampTurnAssistantNote, UNFIXED_SIZE_LABEL, questionIdForUserBlock,
} from './image-generation-note'

const asst = (id: string, patch: Partial<Extract<Block, { kind: 'assistant' }>> = {}): Block => ({
  kind: 'assistant', id, text: 'done', streaming: false, ts: '', ...patch,
})
const user = (id: string, questionId?: string): Block => ({ kind: 'user', id, text: 'hi', ts: '', questionId })

describe('formatImageGenerationNote', () => {
  it('formats missing model and empty size', () => {
    expect(formatImageGenerationNote({ modelName: '', size: '' })).toEqual({
      modelLabel: '未设置', sizeLabel: UNFIXED_SIZE_LABEL,
    })
  })
  it('keeps fixed size literal', () => {
    expect(formatImageGenerationNote({ modelName: 'img-a', size: '1024x1024' })).toEqual({
      modelLabel: 'img-a', sizeLabel: '1024x1024',
    })
  })
})

describe('stamp and rehydrate', () => {
  beforeEach(() => localStorage.clear())

  it('stamps the last completed assistant after the last user', () => {
    const blocks: Block[] = [user('u1', 'q1'), asst('a1', { streaming: true }), asst('a2')]
    const note = formatImageGenerationNote({ modelName: 'm', size: '' })
    const next = stampTurnAssistantNote(
      blocks.map((b) => (b.kind === 'assistant' && b.streaming ? { ...b, streaming: false } : b)),
      note,
    )
    expect(next[1].kind === 'assistant' && next[1].imageGenerationNote).toBeUndefined()
    expect(next[2].kind === 'assistant' && next[2].imageGenerationNote).toEqual(note)
  })

  it('persists by question id and rehydrates after rebuild', () => {
    const note = formatImageGenerationNote({ modelName: 'm', size: '2048x2048' })
    saveImageGenerationNote('sess', 'q1', note)
    expect(loadImageGenerationNoteMap('sess').q1).toEqual(note)
    const rebuilt: Block[] = [user('u1', 'q1'), asst('new-a')]
    const next = rehydrateAssistantNotes(rebuilt, loadImageGenerationNoteMap('sess'))
    expect(next[1].kind === 'assistant' && next[1].imageGenerationNote).toEqual(note)
    expect(questionIdForUserBlock(next, 'u1')).toBe('q1')
  })

  it('rehydrates only the last completed assistant of each question turn', () => {
    const n1 = formatImageGenerationNote({ modelName: 'old', size: '1024x1024' })
    const n2 = formatImageGenerationNote({ modelName: 'new', size: '' })
    saveImageGenerationNote('sess', 'q1', n1)
    saveImageGenerationNote('sess', 'q2', n2)
    const rebuilt: Block[] = [
      user('u1', 'q1'), asst('a1'), asst('a2'),
      user('u2', 'q2'), asst('a3'), asst('a4'),
    ]
    const next = rehydrateAssistantNotes(rebuilt, loadImageGenerationNoteMap('sess'))
    expect(next[1].kind === 'assistant' && next[1].imageGenerationNote).toBeUndefined()
    expect(next[2].kind === 'assistant' && next[2].imageGenerationNote).toEqual(n1)
    expect(next[4].kind === 'assistant' && next[4].imageGenerationNote).toBeUndefined()
    expect(next[5].kind === 'assistant' && next[5].imageGenerationNote).toEqual(n2)
  })
})

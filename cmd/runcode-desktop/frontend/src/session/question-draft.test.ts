import { describe, expect, it } from 'vitest'
import { cancelQuestion, editQuestion, emptyDraft, retryLibraries, selectRetryKnowledge, retryKnowledgeOverride } from './question-draft'
import type { QuestionDraft } from '@/core/bridge'

const original: QuestionDraft = {
  source: { sessionId: 'source', questionId: 'question' },
  text: 'Complete original prompt with D:/work/source.docx',
  images: [{ index: 0, name: '图片 1', mediaType: 'image/png' }],
}
describe('question edit drafts', () => {
  it('defaults to frozen original knowledge and supports an explicit empty retry selection', () => {
    const library = { id: 'original', title: '原库', description: '', path: '', source: '', documentCount: 0, documentCountKnown: false }
    const saved = { text: 'new unsent draft', attachments: [] }
    const draft = editQuestion(saved, { ...original, knowledge: { libraries: [library], status: 'ready', applied: true, sources: [] } })
    expect(retryLibraries(draft)).toEqual([library])
    expect(retryKnowledgeOverride(draft)).toBeUndefined()
    const cleared = selectRetryKnowledge(draft, [])
    expect(retryKnowledgeOverride(cleared)).toEqual({ libraryIds: [] })
    expect(retryLibraries(cleared)).toEqual([])
    expect(draft.retry?.original.knowledge?.libraries).toEqual([library])
    expect(cancelQuestion(cleared)).toBe(saved)
  })
  it('deduplicates edited selection and rejects a sixth library without replacing the draft', () => {
    const draft = editQuestion(emptyDraft(), original)
    const libraries = Array.from({ length: 6 }, (_, i) => ({ id: String(i), title: 'same title', description: '', path: '', source: '', documentCount: 0, documentCountKnown: false }))
    expect(retryKnowledgeOverride(selectRetryKnowledge(draft, [libraries[0], libraries[0], libraries[1]]))).toEqual({ libraryIds: ['0', '1'] })
    expect(selectRetryKnowledge(draft, libraries)).toBe(draft)
  })
  it('loads full original content and source-bound images, not display text', () => {
    const draft = editQuestion(emptyDraft(), original)
    expect(draft.text).toBe(original.text)
    expect(draft.attachments).toEqual([])
    expect(draft.retry?.images).toEqual([0])
    expect(draft.retry?.original.source).toEqual(original.source)
  })
  it('restores unsent text and attachments after cancellation', () => {
    const saved = { text: 'half written', attachments: [{ path: 'draft.png', image: true }] }
    const edited = editQuestion(saved, original)
    edited.text = 'changed question'
    edited.retry!.images = []
    expect(cancelQuestion(edited)).toBe(saved)
  })
  it('switching edited questions retains the initial draft exactly once', () => {
    const saved = { text: 'keep me', attachments: [] }
    const draft = editQuestion(editQuestion(saved, original), { ...original, text: 'different' })
    expect(cancelQuestion(draft)).toBe(saved)
  })
  it('supports pure-image questions and independent session drafts', () => {
    const saved = emptyDraft()
    const imageOnly = editQuestion(saved, { ...original, text: '' })
    expect(imageOnly.text).toBe('')
    expect(imageOnly.retry?.images).toEqual([0])
    expect(saved).toEqual(emptyDraft())
  })
})

import type { QuestionDraft, KnowledgeBaseSummary } from '@/core/bridge'
import type { Attachment } from '@/composer/paste'

export interface ChatDraft {
  text: string
  attachments: Attachment[]
  retry?: { original: QuestionDraft; images: number[]; saved: ChatDraft; libraries?: KnowledgeBaseSummary[] }
}
export const emptyDraft = (): ChatDraft => ({ text: '', attachments: [] })
export function editQuestion(draft: ChatDraft, original: QuestionDraft): ChatDraft {
  return { text: original.text, attachments: [], retry: {
    original, images: (original.images ?? []).map((i) => i.index), saved: draft.retry?.saved ?? draft,
  } }
}
export function cancelQuestion(draft: ChatDraft): ChatDraft { return draft.retry?.saved ?? draft }

export function retryLibraries(draft: ChatDraft): KnowledgeBaseSummary[] {
  return draft.retry?.libraries ?? draft.retry?.original.knowledge?.libraries ?? []
}
export function selectRetryKnowledge(draft: ChatDraft, libraries: KnowledgeBaseSummary[]): ChatDraft {
  const unique = [...new Map(libraries.map(library => [library.id, library])).values()]
  return !draft.retry || unique.length > 5 ? draft : { ...draft, retry: { ...draft.retry, libraries: unique } }
}

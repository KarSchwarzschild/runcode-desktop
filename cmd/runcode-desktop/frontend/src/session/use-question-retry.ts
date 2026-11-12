import { useRef, useState, type RefObject } from 'react'
import { errText, forkQuestion, getQuestion, getSessionKnowledge, type QuestionDraft, type KnowledgeBaseSummary } from '@/core/bridge'
import { basename } from '@/core/paths'
import type { ComposerEditorHandle } from '@/composer/inline-editor'
import type { Conversation } from './use-conversation'
import type { Session } from './use-session'
import type { useChatDrafts } from './use-chat-drafts'
import { cancelQuestion, editQuestion, emptyDraft, retryLibraries, selectRetryKnowledge } from './question-draft'
import type { SessionKnowledge } from './use-knowledge'
import { useActionScope } from './use-action-scope'

export function useQuestionRetry({ sessionId, blocked, drafts, conversation, session, textarea, notify, knowledge }: {
  sessionId: string
  blocked: boolean
  drafts: ReturnType<typeof useChatDrafts>
  conversation: Conversation
  session: Session
  textarea: RefObject<ComposerEditorHandle>
  notify: (text: string) => void
  knowledge: SessionKnowledge
}) {
  const [pending, setPending] = useState(false)
  const pendingRef = useRef(false)
  const scope = useActionScope(sessionId)
  const disabled = blocked || pending
  const latestBlocked = useRef(blocked)
  latestBlocked.current = blocked
  const libraryCache = useRef({ key: '', libraries: new Map<string, KnowledgeBaseSummary>() })
  const latestKnowledge = useRef(knowledge)
  latestKnowledge.current = knowledge

  async function exclusive(fn: (current: () => boolean) => Promise<boolean>) {
    if (latestBlocked.current || pendingRef.current) return false
    pendingRef.current = true
    setPending(true)
    const current = scope.capture()
    try { return await fn(current) }
    catch (e) { if (current()) notify(`重新生成失败：${errText(e)}`); return false }
    finally { pendingRef.current = false; setPending(false); void session.refreshOpen() }
  }

  async function generate(original: QuestionDraft, text: string, paths: string[], images: number[], current: () => boolean, libraries?: KnowledgeBaseSummary[]) {
    if (!current() || latestBlocked.current) return false
    const unchanged = session.captureSwitch()
    const r = await forkQuestion(original.source)
    if (!unchanged() || !current()) {
      await conversation.applyResumed(r, () => false)
      return false
    }
    // Register even an abandoned branch so later focusing it does not look empty.
    const branchDraft = editQuestion(emptyDraft(), original)
    branchDraft.text = text
    branchDraft.attachments = paths.map((path) => ({ path, image: true }))
    branchDraft.retry!.images = images
    if (libraries) branchDraft.retry!.libraries = libraries
    const accepted = await session.runQuestionBranch(r, current, async (id) => {
      drafts.update(id, () => branchDraft)
      const selection = await getSessionKnowledge(id)
      return conversation.submitFor({
        sessionId: id, text, imagePaths: paths, source: original.source,
        originalImages: images, allowSteering: false, knowledgeRevision: selection.revision,
      }, undefined, [...images.map((i) => (original.images ?? []).find((im) => im.index === i)?.name ?? '图片'), ...paths.map(basename)])
    })
    if (accepted) {
      drafts.update(r.info.sessionId, (d) => d === branchDraft ? emptyDraft() : d)
      if (latestKnowledge.current.state.sessionId === r.info.sessionId) latestKnowledge.current.reload()
    }
    return accepted
  }

  const edit = (questionId: string) => exclusive(async (current) => {
    const original = await getQuestion({ sessionId, questionId })
    if (!current() || latestBlocked.current) return false
    drafts.update(sessionId, (d) => editQuestion(d, original))
    requestAnimationFrame(() => { if (current()) textarea.current?.focus() })
    return true
  })

  const retry = (questionId: string) => exclusive(async (current) => {
    const original = await getQuestion({ sessionId, questionId })
    if (!current() || latestBlocked.current) return false
    // Failure leaves an editable input, rather than losing the original content.
    drafts.update(sessionId, (d) => editQuestion(d, original))
    const submittedDraft = drafts.read(sessionId)
    const accepted = await generate(original, original.text, [], (original.images ?? []).map((i) => i.index), current)
    if (accepted) drafts.update(sessionId, d => d === submittedDraft ? cancelQuestion(d) : d)
    return accepted
  })

  async function send(text: string, paths: string[], display?: string) {
    const draft = drafts.read(sessionId)
    if (!draft.retry) {
      if (knowledge.sendBlock) { notify(knowledge.sendBlock); return false }
      const accepted = await conversation.send(text, paths, display, knowledge.state.revision)
      if (accepted) drafts.update(sessionId, (d) => d === draft ? emptyDraft() : d)
      return accepted
    }
    const { original, images } = draft.retry
    return exclusive(async (current) => {
      const accepted = await generate(original, text, paths, images, current, retryLibraries(draft))
      if (accepted) drafts.update(sessionId, d => d === draft ? cancelQuestion(d) : d)
      return accepted
    })
  }

  const cancel = () => { if (!pendingRef.current) drafts.update(sessionId, cancelQuestion) }
  const removeImage = (index: number) => drafts.update(sessionId, (d) => d.retry
    ? { ...d, retry: { ...d.retry, images: d.retry.images.filter((i) => i !== index) } } : d)
  const editing = drafts.draft.retry
  const libraries = retryLibraries(drafts.draft)
  // Keep display metadata across catalog search while editing a retry. It carries
  // no authorization: submit revalidates every ID against the live selection.
  const cacheKey = `${sessionId}:${editing?.original.source.sessionId}:${editing?.original.source.questionId}:${knowledge.state.contextVersion}`
  if (libraryCache.current.key !== cacheKey) libraryCache.current = { key: cacheKey, libraries: new Map() }
  for (const library of [...knowledge.state.items, ...knowledge.visibleLibraries, ...(editing?.original.knowledge?.libraries ?? []), ...libraries]) libraryCache.current.libraries.set(library.id, library)
  const retryKnowledge: SessionKnowledge = editing ? {
    ...knowledge,
    visibleLibraries: libraries,
    state: { ...knowledge.state, libraries, pendingIds: null, loading: false, loaded: true, saving: false, error: '', unavailableReason: '' },
    sendBlock: '',
    save: async ids => {
      if (latestBlocked.current || pendingRef.current) return
      if (new Set(ids).size > 5) { notify('最多选择 5 个知识库'); return }
      const known = libraryCache.current.libraries
      if (ids.some(id => !known.has(id))) { notify('知识库目录已变化，请重新选择'); return }
      drafts.update(sessionId, draft => selectRetryKnowledge(draft, ids.map(id => known.get(id)!)))
    },
  } : knowledge
  return { edit, retry, send, cancel, removeImage, disabled, pending, editing, knowledge: retryKnowledge }
}

import { useRef, useState } from 'react'
import { errText, type SessionInfo } from '@/core/bridge'
import type { SessionKnowledge } from './use-knowledge'
import type { useChatDrafts } from './use-chat-drafts'
import { emptyDraft } from './question-draft'

// 选库一律落在当前会话；首次连选也不能创建／聚焦另一会话，否则 Composer 重挂载会关掉面板。
// create 仍保留给显式“新建知识库问答”确认入口，不由 save 触发。
export function useKnowledgeMode({ info, knowledge, drafts, hasHistory: _hasHistory, create, notify }: {
  info: SessionInfo | null
  knowledge: SessionKnowledge
  drafts: ReturnType<typeof useChatDrafts>
  hasHistory: boolean
  create: (sourceId: string, ids: string[], onCreated: (id: string) => void) => Promise<boolean>
  notify: (message: string) => void
}) {
  const [confirmation, setConfirmation] = useState<{ source: string; ids: string[] } | null>(null)
  const [pending, setPending] = useState(false)
  const pendingRef = useRef(false)
  const isKnowledge = info?.kind === 'knowledge'
  const source = info?.sessionId ?? ''
  async function createSelected(ids: string[]) {
    if (!source || pendingRef.current) return
    const draft = drafts.read(source)
    // 附件路径可能位于文字之外，重试草稿还包含原题图像。拒绝整次迁移，不能静默丢附件。
    if (draft.attachments.length || draft.retry) { notify('知识库问答仅支持纯文字，请先移除附件或取消原题修改；当前草稿已保留'); return }
    pendingRef.current = true
    setPending(true)
    try {
      await create(source, ids, id => {
        drafts.update(id, () => ({ text: draft.text, attachments: [] }))
        // 创建期间用户可能继续编辑原会话，仅在原草稿对象未变时清除已迁移文字。
        drafts.update(source, current => current === draft ? emptyDraft() : current)
      })
    } catch (error) { notify(`新建知识库问答失败：${errText(error)}`) }
    finally { pendingRef.current = false; setPending(false) }
  }
  async function save(ids: string[]) {
    if (info?.readOnlyReason) { notify(info.readOnlyReason); return }
    return knowledge.save(ids)
  }
  return {
    isKnowledge, pending, confirming: confirmation?.source === source,
    cancel: () => setConfirmation(null),
    confirm: async () => { const selected = confirmation; setConfirmation(null); if (selected?.source === source) await createSelected(selected.ids) },
    knowledge: { ...knowledge, save } satisfies SessionKnowledge,
  }
}

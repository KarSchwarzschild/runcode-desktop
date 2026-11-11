import type { KnowledgeChatEvent, KnowledgeSource, QuestionKnowledge } from '@/core/bridge'
import type { Block } from '@/chat/blocks'
import type { ConversationState } from './conversation-state'

export interface KnowledgeRun {
  runId: string
  seq: number
  phase: string
  text: string
  thinking: string
  sources: KnowledgeSource[]
  applied: boolean
  empty: boolean
  error?: string
  inputTokens?: number
  outputTokens?: number
  truncated?: boolean
}
const terminal = (phase: string) => ['completed', 'failed', 'cancelled', 'interrupted'].includes(phase)

// 事件可先于 durable receipt 到达。每题只累计一份流式状态，receipt 绑定原题后
// 才插入回答，不能把后台会话的输出或迟到事件挂到当前新草稿下面。
export function withKnowledgeChat(state: ConversationState, event: KnowledgeChatEvent): ConversationState {
  if (!event.questionId || !event.runId || !Number.isSafeInteger(event.seq) || event.seq < 1) return state
  if (!['accepted', 'retrieving', 'ready', 'empty', 'generating', 'delta', 'completed', 'failed', 'cancelled', 'interrupted'].includes(event.phase)) return state
  const previous = state.knowledgeRuns?.[event.questionId]
  // 首个 runId 固定该题运行身份；序号必须严格递增，终态不再接受任何后续增量。
  if (previous && (previous.runId !== event.runId || event.seq <= previous.seq || terminal(previous.phase))) return state
  const question = state.blocks.find(b => b.kind === 'user' && b.questionId === event.questionId)
  // 已关闭会话没有用户行，不允许单个迟到事件重新创建整个会话。
  if (!previous && !question && !state.blocks.some(b => b.kind === 'user' && !b.questionId)) return state
  if (state.stoppedQuestions?.has(event.questionId)) return state
  const latest = [...state.blocks].reverse().find(b => b.kind === 'user')
  // 停止可能发生在第一个宿主事件和 receipt 之前，此时只能用乐观行身份记住取消。
  if (!question && latest && state.stoppedQuestionRows?.has(latest.id)) return state
  const run: KnowledgeRun = {
    runId: event.runId, seq: event.seq, phase: event.phase,
    text: previous?.text ?? '', thinking: previous?.thinking ?? '',
    sources: previous?.sources ?? [], applied: previous?.applied ?? false,
    empty: previous?.empty ?? false, error: event.error,
    inputTokens: event.inputTokens, outputTokens: event.outputTokens, truncated: event.truncated,
  }
  if (event.phase === 'empty') run.empty = true
  if (event.phase === 'generating') { run.sources = event.sources ?? []; run.applied = true }
  if (event.phase === 'delta') {
    run.text += event.text ?? ''
    run.thinking += event.thinking ?? ''
  } else {
    if (event.text !== undefined) run.text = event.text
    if (event.thinking !== undefined) run.thinking = event.thinking
  }
  const next = { ...state, knowledgeRuns: { ...state.knowledgeRuns, [event.questionId]: run } }
  // 旧题迟到的终态不得把下一题的 busy 清掉。
  if (latest?.kind === 'user' && (!latest.questionId || latest.questionId === event.questionId)) next.busy = !terminal(run.phase)
  return renderKnowledgeRun(next, event.questionId)
}

export function renderKnowledgeRun(state: ConversationState, questionId: string): ConversationState {
  const run = state.knowledgeRuns?.[questionId]
  const index = state.blocks.findIndex(b => b.kind === 'user' && b.questionId === questionId)
  if (!run || index < 0) return state
  const user = state.blocks[index] as Extract<Block, { kind: 'user' }>
  const status = run.empty ? 'empty' : run.applied ? 'ready'
    : ['accepted', 'retrieving'].includes(run.phase) ? 'pending' : run.phase === 'ready' ? 'ready' : run.phase
  const knowledge: QuestionKnowledge = { libraries: user.knowledge?.libraries ?? [], status, applied: run.applied, sources: run.sources }
  const prefix = `knowledge:${questionId}:`
  const answer: Block[] = []
  if (run.text || run.thinking) answer.push({ kind: 'assistant', id: `${prefix}answer`, text: run.text, thinking: run.thinking, streaming: !terminal(run.phase), ts: '' })
  if (run.phase === 'failed') answer.push({ kind: 'error', id: `${prefix}status`, text: run.error || '知识库问答失败，可重新生成重试' })
  if (run.phase === 'cancelled' || run.phase === 'interrupted') answer.push({ kind: 'notice', id: `${prefix}status`, text: run.phase === 'cancelled' ? '已停止，部分回答未完成' : '上次回答已中断，请显式重新生成' })
  if (run.truncated) answer.push({ kind: 'notice', id: `${prefix}truncated`, text: '回答已达到输出上限，内容可能不完整' })
  if (terminal(run.phase) && ((run.inputTokens ?? 0) > 0 || (run.outputTokens ?? 0) > 0)) answer.push({ kind: 'usage', id: `${prefix}usage`, inTok: run.inputTokens ?? 0, outTok: run.outputTokens ?? 0 })
  // 使用稳定块 ID 原位重建，重复 delta/终态不会堆积多个回答、用量或错误条。
  const blocks = state.blocks.filter(b => !b.id.startsWith(prefix))
  const position = blocks.findIndex(b => b.id === user.id)
  blocks.splice(position, 1, { ...user, knowledge }, ...answer)
  return { ...state, blocks }
}

export function cancelKnowledgeRuns(state: ConversationState): ConversationState {
  let next = state
  for (const [questionId, run] of Object.entries(state.knowledgeRuns ?? {})) {
    if (terminal(run.phase)) continue
    next = { ...next, knowledgeRuns: { ...next.knowledgeRuns, [questionId]: { ...run, phase: 'cancelled' } } }
    next = renderKnowledgeRun(next, questionId)
  }
  return next
}

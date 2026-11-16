// 助手回复尾注：冻结的生图模型与尺寸策略。纯函数 + localStorage 映射，不依赖 React。
import type { Block } from './blocks'

export type AssistantImageGenerationNote = {
  modelLabel: string
  sizeLabel: string
}

export const UNFIXED_SIZE_LABEL = '不固定尺寸（由提示词和生图模型能力共同决定）'

export function formatImageGenerationNote(input: { modelName?: string | null; size?: string | null }): AssistantImageGenerationNote {
  const model = (input.modelName ?? '').trim()
  const size = (input.size ?? '').trim()
  return {
    modelLabel: model || '未设置',
    sizeLabel: size || UNFIXED_SIZE_LABEL,
  }
}

export function noteStorageKey(sessionId: string): string {
  return `imagegen.note.${sessionId}`
}

export type ImageGenerationNoteMap = Record<string, AssistantImageGenerationNote>

export function loadImageGenerationNoteMap(sessionId: string): ImageGenerationNoteMap {
  if (!sessionId) return {}
  try {
    const raw = localStorage.getItem(noteStorageKey(sessionId))
    if (!raw) return {}
    const parsed = JSON.parse(raw) as ImageGenerationNoteMap
    return parsed && typeof parsed === 'object' ? parsed : {}
  } catch {
    return {}
  }
}

export function saveImageGenerationNote(sessionId: string, questionId: string, note: AssistantImageGenerationNote): void {
  if (!sessionId || !questionId) return
  const map = loadImageGenerationNoteMap(sessionId)
  map[questionId] = note
  localStorage.setItem(noteStorageKey(sessionId), JSON.stringify(map))
}

// stampTurnAssistantNote 把本回合冻结的尾注写到最后一个已完成的助手气泡上。
export function stampTurnAssistantNote(blocks: Block[], note: AssistantImageGenerationNote): Block[] {
  let lastAsst = -1
  const lastUser = blocks.map((b) => b.kind).lastIndexOf('user')
  for (let i = blocks.length - 1; i > lastUser; i--) {
    const b = blocks[i]
    if (b.kind === 'assistant' && !b.streaming) {
      lastAsst = i
      break
    }
  }
  if (lastAsst < 0) return blocks
  const target = blocks[lastAsst]
  if (target.kind !== 'assistant') return blocks
  if (target.imageGenerationNote?.modelLabel === note.modelLabel && target.imageGenerationNote?.sizeLabel === note.sizeLabel) {
    return blocks
  }
  return blocks.map((b, i) => (i === lastAsst && b.kind === 'assistant' ? { ...b, imageGenerationNote: note } : b))
}

// rehydrateAssistantNotes 按 user.questionId 把持久化尾注贴回恢复后的助手块；
// 每个提问回合只贴在最后一个已完成的助手气泡上（与 stampTurnAssistantNote 一致）。
export function rehydrateAssistantNotes(blocks: Block[], map: ImageGenerationNoteMap): Block[] {
  if (!map || Object.keys(map).length === 0) return blocks
  const stampAt = new Set<number>()
  let turnStart = -1
  let turnNote: AssistantImageGenerationNote | undefined
  const flush = (endExclusive: number) => {
    if (!turnNote) return
    for (let i = endExclusive - 1; i > turnStart; i--) {
      const b = blocks[i]
      if (b.kind === 'assistant' && !b.streaming) {
        stampAt.add(i)
        return
      }
    }
  }
  for (let i = 0; i < blocks.length; i++) {
    const b = blocks[i]
    if (b.kind === 'user') {
      flush(i)
      turnStart = i
      turnNote = b.questionId ? map[b.questionId] : undefined
    }
  }
  flush(blocks.length)
  if (stampAt.size === 0) return blocks
  let changed = false
  const next = blocks.map((b, i) => {
    if (!stampAt.has(i) || b.kind !== 'assistant' || b.imageGenerationNote) return b
    const note = (() => {
      for (let j = i; j >= 0; j--) {
        const u = blocks[j]
        if (u.kind === 'user' && u.questionId && map[u.questionId]) return map[u.questionId]
      }
      return undefined
    })()
    if (!note) return b
    changed = true
    return { ...b, imageGenerationNote: note }
  })
  return changed ? next : blocks
}

export function questionIdForUserBlock(blocks: Block[], userBlockId: string): string | undefined {
  const b = blocks.find((x) => x.id === userBlockId && x.kind === 'user')
  return b && b.kind === 'user' ? b.questionId : undefined
}

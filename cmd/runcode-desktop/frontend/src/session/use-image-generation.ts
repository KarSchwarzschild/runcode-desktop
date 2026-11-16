// 当前会话的「生成图片」开关。状态在 Go 侧（会话 sidecar），这里只是它的镜子加一个
// 「还没有会话时」的草稿：草稿必须在会话创建后先保存成功，首条消息才能发出去
// （ensureCommitted），禁止未保存即乐观发送。开关只作用于下一轮；回合进行中禁止切换。
import { useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import {
  errText, Events, getSessionImageGeneration, onEvent, setSessionImageGeneration,
  type SessionImageGenerationView, type SetSessionImageGenerationRequest,
} from '@/core/bridge'

export type ImageGenerationDeps = {
  get: (sessionId: string) => Promise<SessionImageGenerationView>
  set: (req: SetSessionImageGenerationRequest) => Promise<SessionImageGenerationView>
}

export type ImageGenerationState = {
  sessionId: string
  view: SessionImageGenerationView | null
  // draft 是还没有会话时用户选的开关；null = 没有待保存的选择。
  draft: boolean | null
  loading: boolean
  saving: boolean
  error: string
}

const EMPTY: ImageGenerationState = { sessionId: '', view: null, draft: null, loading: false, saving: false, error: '' }

export class ImageGenerationController {
  private state: ImageGenerationState = EMPTY
  private listeners = new Set<() => void>()
  // generation 让切走之后迟到的旧会话结果作废。
  private generation = 0
  private pending: Promise<boolean> | null = null

  constructor(private deps: ImageGenerationDeps) {}

  subscribe = (fn: () => void) => {
    this.listeners.add(fn)
    return () => { this.listeners.delete(fn) }
  }
  snapshot = () => this.state

  private patch(next: Partial<ImageGenerationState>) {
    this.state = { ...this.state, ...next }
    this.listeners.forEach((fn) => fn())
  }

  async activate(sessionId: string): Promise<void> {
    const gen = ++this.generation
    this.pending = null
    // 草稿跟着「下一条会话」走，不跟着某条已存在的会话走。
    this.patch({ sessionId, view: null, loading: !!sessionId, saving: false, error: '' })
    if (!sessionId) return
    try {
      const view = await this.deps.get(sessionId)
      if (gen === this.generation) this.patch({ view, loading: false })
    } catch (e) {
      if (gen === this.generation) this.patch({ loading: false, error: errText(e) })
    }
  }

  refresh(): Promise<void> {
    return this.activate(this.state.sessionId)
  }

  // setEnabled 返回失败原因（成功或已作废为 ''），调用方据此提示用户。
  async setEnabled(enabled: boolean, opts: { busy?: boolean } = {}): Promise<string> {
    if (opts.busy) {
      const error = '回合进行中不能切换图片生成，请等待完成或停止后再试'
      this.patch({ error })
      return error
    }
    const { sessionId, view } = this.state
    if (!sessionId) {
      this.patch({ draft: enabled, error: '' })
      return ''
    }
    const gen = this.generation
    this.patch({ saving: true, error: '' })
    try {
      const next = await this.deps.set({ sessionId, enabled, expectedRevision: view?.revision ?? 0 })
      if (gen === this.generation) this.patch({ view: next, saving: false })
      return ''
    } catch (e) {
      const error = errText(e)
      if (gen === this.generation) this.patch({ saving: false, error })
      return gen === this.generation ? error : ''
    }
  }

  // ensureCommitted 在发送前调用：有草稿就先保存，保存失败返回 false（不发送）。
  ensureCommitted(): Promise<boolean> {
    const { draft, sessionId } = this.state
    if (draft === null) return Promise.resolve(true)
    if (!sessionId) return Promise.resolve(false)
    if (this.pending) return this.pending
    const gen = this.generation
    const run = this.commitDraft(sessionId, draft, gen).finally(() => {
      if (this.pending === run) this.pending = null
    })
    this.pending = run
    return run
  }

  private async commitDraft(sessionId: string, draft: boolean, gen: number): Promise<boolean> {
    this.patch({ saving: true, error: '' })
    try {
      const view = this.state.view ?? await this.deps.get(sessionId)
      const next = await this.deps.set({ sessionId, enabled: draft, expectedRevision: view.revision })
      if (gen !== this.generation) return false
      this.patch({ view: next, draft: null, saving: false })
      return true
    } catch (e) {
      if (gen === this.generation) this.patch({ saving: false, error: errText(e) })
      return false
    }
  }
}

// 组件里直接用的那一层：按会话激活，登录态变化时重读。
export function useImageGeneration(sessionId: string | null | undefined) {
  const [controller] = useState(() => new ImageGenerationController({ get: getSessionImageGeneration, set: setSessionImageGeneration }))
  const state = useSyncExternalStore(controller.subscribe, controller.snapshot)
  const id = sessionId ?? ''
  useEffect(() => { void controller.activate(id) }, [controller, id])
  useEffect(() => onEvent(Events.PassportChanged, () => { void controller.refresh() }), [controller])
  const actions = useMemo(() => ({
    setEnabled: (enabled: boolean, opts?: { busy?: boolean }) => controller.setEnabled(enabled, opts),
    ensureCommitted: () => controller.ensureCommitted(),
    currentError: () => controller.snapshot().error,
    refresh: () => { void controller.refresh() },
  }), [controller])
  // 会话 prop 先于 effect 变化；那一帧不能画旧会话的开关。
  const current = state.sessionId === id ? state : { ...EMPTY, sessionId: id, draft: state.draft, loading: !!id }
  const enabled = current.draft ?? current.view?.enabled ?? false
  return { state: current, enabled, ...actions }
}

export type SessionImageGeneration = ReturnType<typeof useImageGeneration>

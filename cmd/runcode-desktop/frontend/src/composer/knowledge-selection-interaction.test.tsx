// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { KnowledgeController } from '@/session/knowledge-state'
import type { KnowledgeBaseSummary, KnowledgePage, SessionKnowledgeView, SetSessionKnowledgeRequest } from '@/core/bridge'
import { KnowledgePicker } from './knowledge-picker'

const library = (id: string): KnowledgeBaseSummary => ({
  id, title: `库 ${id}`, description: `简介 ${id}`, path: `路径/${id}`, source: 'demo', documentCount: 1, documentCountKnown: true,
})
const view = (sessionId: string, ids: string[] = [], revision = 0): SessionKnowledgeView => ({
  sessionId, revision, libraries: ids.map(library), mode: 'demo', unavailableReason: '',
})
const deferred = <T,>() => {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

describe('knowledge selection interaction', () => {
  let root: Root
  let host: HTMLDivElement
  beforeEach(() => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
    host = document.createElement('div')
    document.body.append(host)
    root = createRoot(host)
    vi.useFakeTimers()
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    host.remove()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('firstSelectionKeepsPickerAndSession', async () => {
    const records = new Map<string, SessionKnowledgeView>()
    const gate = deferred<void>()
    let sets = 0
    const controller = new KnowledgeController({
      get: async (id) => records.get(id) ?? view(id),
      list: async () => ({ items: [library('a'), library('b'), library('c')], page: 1, pageSize: 10, total: 3, hasMore: false, mode: 'demo', unavailableReason: '', maxSelection: 5 } satisfies KnowledgePage),
      set: async (request: SetSessionKnowledgeRequest) => {
        sets += 1
        await gate.promise
        const current = records.get(request.sessionId) ?? view(request.sessionId)
        const next = view(request.sessionId, [...new Set(request.libraryIds ?? [])], current.revision + 1)
        records.set(request.sessionId, next)
        return next
      },
    })
    await controller.activate('session-A')
    await controller.openCatalog()
    let closed = 0
    const sessionId = 'session-A'
    function Shell() {
      const state = controller.snapshot()
      return <KnowledgePicker
        open
        state={state}
        locked={false}
        onClose={() => { closed += 1 }}
        onSearch={() => {}}
        onLoadMore={() => {}}
        onRetry={() => {}}
        onToggle={(id) => { void controller.toggle(id) }}
      />
    }
    controller.subscribe(() => { void act(() => root.render(<Shell />)) })
    await act(async () => root.render(<Shell />))
    expect(host.querySelector('[aria-label="知识库列表"]')).not.toBeNull()
    await act(async () => { void controller.toggle('a') })
    await act(async () => { void controller.toggle('b') })
    await act(async () => { void controller.toggle('c') })
    expect(controller.snapshot().pendingIds?.sort()).toEqual(['a', 'b', 'c'])
    expect(closed).toBe(0)
    expect(controller.snapshot().sessionId).toBe(sessionId)
    expect(sets).toBe(1)
    expect(host.querySelector('[aria-label="知识库列表"]')).not.toBeNull()
    expect(host.textContent).toContain('已选择 3 / 5')
    expect(host.textContent).not.toContain('正在保存…')
    await act(async () => { await vi.advanceTimersByTimeAsync(300) })
    expect(host.textContent).toContain('正在保存…')
    gate.resolve()
    await act(async () => { await Promise.resolve() })
    await vi.waitFor(() => expect(controller.snapshot().saving).toBe(false))
    expect(closed).toBe(0)
    expect(controller.snapshot().sessionId).toBe(sessionId)
    expect(controller.snapshot().libraries.map((item) => item.id).sort()).toEqual(['a', 'b', 'c'])
    expect(host.querySelector('[aria-label="知识库列表"]')).not.toBeNull()
  })

  it('delaysOnlySavingIndicator', async () => {
    const gate = deferred<SessionKnowledgeView>()
    const controller = new KnowledgeController({
      get: async () => view('A'),
      list: async () => ({ items: [library('one')], page: 1, pageSize: 10, total: 1, hasMore: false, mode: 'demo', unavailableReason: '', maxSelection: 5 }),
      set: () => gate.promise,
    })
    await controller.activate('A')
    await controller.openCatalog()
    function Shell() {
      return <KnowledgePicker open state={controller.snapshot()} locked={false} onClose={() => {}} onSearch={() => {}} onLoadMore={() => {}} onRetry={() => {}} onToggle={() => {}} />
    }
    controller.subscribe(() => { void act(() => root.render(<Shell />)) })
    await act(async () => root.render(<Shell />))
    await act(async () => { void controller.save(['one']) })
    expect(host.textContent).toContain('已选择 1 / 5')
    expect(host.textContent).not.toContain('正在保存')
    await act(async () => { await vi.advanceTimersByTimeAsync(50) })
    expect(host.textContent).not.toContain('正在保存')
    await act(async () => { await vi.advanceTimersByTimeAsync(250) })
    expect(host.textContent).toContain('正在保存…')
    await act(async () => { await vi.advanceTimersByTimeAsync(1700) })
    expect(host.textContent).toContain('保存较慢，仍可继续选择…')
    gate.resolve(view('A', ['one'], 1))
    await act(async () => { await Promise.resolve() })
  })
})

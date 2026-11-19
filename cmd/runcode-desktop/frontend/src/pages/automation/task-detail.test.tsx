// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AutomationTask, AutomationTaskDraft } from '@/core/bridge'
import { TaskDetail } from './task-detail'

const bridge = vi.hoisted(() => ({
  listAutomationRuns: vi.fn(),
  onEnvelope: vi.fn(() => () => {}),
}))

vi.mock('@/core/bridge', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/core/bridge')>()
  return {
    ...actual,
    listAutomationRuns: bridge.listAutomationRuns,
    onEnvelope: bridge.onEnvelope,
    errText: (e: unknown) => e instanceof Error ? e.message : String(e),
  }
})

const context = { viewId: 'view', epoch: 1 }
const schedule = {
  mode: 'daily', timeZone: 'Asia/Shanghai', startDate: '2026-11-19', timeOfDay: '09:00:00', interval: 1,
}

function task(extra: Partial<AutomationTask> = {}): AutomationTask {
  return {
    id: 'task-1',
    revision: 2,
    name: '整理记录',
    prompt: '生成摘要',
    workspace: 'D:\\资料',
    executionMode: 'reuse_session',
    schedule,
    state: 'enabled',
    nextRunAtUTC: '2026-11-20T01:00:00Z',
    binding: { modelKind: 'passport', modelName: 'model-a', bindingVersion: 1 },
    ...extra,
  }
}

let root: Root
let host: HTMLDivElement

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  bridge.listAutomationRuns.mockReset()
  bridge.listAutomationRuns.mockResolvedValue({ context, items: [] })
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})

function setInput(el: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value)
  el.dispatchEvent(new Event('input', { bubbles: true }))
}

describe('automation task detail', () => {
  it('keeps a dirty draft when save conflicts and refreshes only on demand', async () => {
    const current = task()
    const onSave = vi.fn<(_draft: AutomationTaskDraft, _revision: number) => Promise<AutomationTask>>()
      .mockRejectedValue(new Error('任务已发生变化，请刷新后重试'))
    await act(async () => {
      root.render(
        <TaskDetail
          task={current}
          context={context}
          recent={[]}
          pending={false}
          onSave={onSave}
          onSetPaused={async () => {}}
          onClose={() => {}}
          onOpenRun={async () => {}}
        />,
      )
    })

    const name = host.querySelector('input[aria-label="任务名称"]') as HTMLInputElement
    await act(async () => setInput(name, '新名称'))
    const save = [...host.querySelectorAll('button')].find((button) => button.textContent === '保存修改')!
    await act(async () => save.click())

    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ name: '新名称' }), 2)
    expect(host.textContent).toContain('任务已发生变化，请刷新后重试')
    expect((host.querySelector('input[aria-label="任务名称"]') as HTMLInputElement).value).toBe('新名称')

    const refresh = [...host.querySelectorAll('button')].find((button) => button.textContent === '刷新')!
    await act(async () => refresh.click())
    expect((host.querySelector('input[aria-label="任务名称"]') as HTMLInputElement).value).toBe('整理记录')
  })

  it('locks rule fields for an active run while allowing pause', async () => {
    const onPause = vi.fn(async () => {})
    const active = task({
      lastRun: {
        id: 'run-1', taskId: 'task-1', taskRevision: 2, runRevision: 1, status: 'running',
        scheduledAtUTC: '2026-11-19T01:00:00Z', schedule,
      },
    })
    await act(async () => {
      root.render(
        <TaskDetail
          task={active}
          context={context}
          recent={[]}
          pending={false}
          onSave={async () => active}
          onSetPaused={onPause}
          onClose={() => {}}
          onOpenRun={async () => {}}
        />,
      )
    })

    expect((host.querySelector('input[aria-label="任务名称"]') as HTMLInputElement).disabled).toBe(true)
    expect(host.textContent).toContain('任务正在执行，规则暂时不能修改')
    const pause = [...host.querySelectorAll('button')].find((button) => button.textContent === '暂停')!
    expect(pause).toBeTruthy()
    await act(async () => pause.click())
    expect(onPause).toHaveBeenCalledWith(true)
  })

  it('shows that the current run continues after pausing future schedules', async () => {
    const paused = task({
      state: 'paused',
      lastRun: {
        id: 'run-1', taskId: 'task-1', taskRevision: 2, runRevision: 1, status: 'running',
        scheduledAtUTC: '2026-11-19T01:00:00Z', schedule,
      },
    })
    await act(async () => {
      root.render(
        <TaskDetail
          task={paused}
          context={context}
          recent={[]}
          pending={false}
          onSave={async () => paused}
          onSetPaused={async () => {}}
          onClose={() => {}}
          onOpenRun={async () => {}}
        />,
      )
    })
    expect(host.textContent).toContain('已暂停 · 本次执行继续完成')
    expect([...host.querySelectorAll('button')].some((button) => button.textContent === '恢复')).toBe(true)
  })
})

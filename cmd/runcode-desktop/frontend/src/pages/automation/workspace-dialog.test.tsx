// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/core/bridge', () => ({
  getAutomationDefaults: vi.fn(async () => ({
    context: { viewId: 'view', epoch: 1 },
    available: true,
    recentWorkspaces: [],
    model: { name: 'model-a' },
    backgroundSupported: false,
    bindingVersion: 1,
  })),
  listAutomationTasks: vi.fn(async () => ({ context: { viewId: 'view', epoch: 1 }, items: [] })),
  errText: (e: unknown) => (e instanceof Error ? e.message : String(e)),
  Events: {
    AutomationTaskChanged: 'automation:task_changed',
    AutomationContextChanged: 'automation:context_changed',
    AutomationServiceChanged: 'automation:service_changed',
  },
  onEnvelope: () => () => {},
  openSession: vi.fn(),
  pickWorkspaceFolder: vi.fn(),
}))

import { AutomationWorkspaceDialog } from './workspace-dialog'

let root: Root
let host: HTMLDivElement

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})

function Harness() {
  const [open, setOpen] = useState(false)
  return (
    <>
      <textarea aria-label="composer" defaultValue="未发送的草稿" />
      <button type="button" onClick={() => setOpen(true)}>更多</button>
      <AutomationWorkspaceDialog open={open} onClose={() => setOpen(false)} />
    </>
  )
}

describe('automation workspace dialog', () => {
  it('returns focus to the trigger and leaves the composer draft untouched', async () => {
    await act(async () => { root.render(<Harness />) })
    const draft = host.querySelector('textarea') as HTMLTextAreaElement
    const trigger = [...host.querySelectorAll('button')].find((b) => b.textContent === '更多') as HTMLButtonElement
    expect(draft.value).toBe('未发送的草稿')
    // jsdom 的 click 不会把焦点送进按钮；真实点击会先 focus 再触发。这里补上这一步。
    trigger.focus()

    await act(async () => { trigger.click() })
    expect(host.querySelector('[role="dialog"]')).toBeTruthy()
    const close = [...host.querySelectorAll('button')].find((b) => b.getAttribute('aria-label') === '关闭')
    expect(close).toBeTruthy()
    await act(async () => { close!.click() })

    expect(host.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(trigger)
    expect(draft.value).toBe('未发送的草稿')
  })
})

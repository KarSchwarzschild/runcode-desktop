// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Sidebar } from './sidebar'

const bridge = vi.hoisted(() => ({
  openSession: vi.fn(),
  pickWorkspaceFolder: vi.fn(),
  resumeSession: vi.fn(),
  getAutomationDefaults: vi.fn(),
  listAutomationTasks: vi.fn(),
}))

vi.mock('@/core/bridge', () => ({
  openSession: bridge.openSession,
  pickWorkspaceFolder: bridge.pickWorkspaceFolder,
  resumeSession: bridge.resumeSession,
  getAutomationDefaults: bridge.getAutomationDefaults,
  listAutomationTasks: bridge.listAutomationTasks,
  errText: (e: unknown) => (e instanceof Error ? e.message : String(e)),
  Events: {
    AutomationTaskChanged: 'automation:task_changed',
    AutomationContextChanged: 'automation:context_changed',
    AutomationServiceChanged: 'automation:service_changed',
    AutomationRunChanged: 'automation:run_changed',
  },
  onEnvelope: () => () => {},
}))

import { AutomationWorkspaceDialog } from '@/pages/automation/workspace-dialog'

let root: Root
let host: HTMLDivElement

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  bridge.openSession.mockReset()
  bridge.pickWorkspaceFolder.mockReset()
  bridge.resumeSession.mockReset()
  bridge.getAutomationDefaults.mockReset()
  bridge.listAutomationTasks.mockReset()
  bridge.getAutomationDefaults.mockResolvedValue({
    context: { viewId: 'view', epoch: 1 },
    available: true,
    recentWorkspaces: [],
    model: { name: 'model-a', permissionMode: 'default' },
    backgroundSupported: false,
    bindingVersion: 3,
  })
  bridge.listAutomationTasks.mockResolvedValue({
    context: { viewId: 'view', epoch: 1 },
    items: [],
  })
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})

function Harness() {
  const [view, setView] = useState<'chat' | 'settings' | 'plugins' | 'market' | 'permissions' | 'memory' | 'usage'>('chat')
  const [open, setOpen] = useState(false)
  return (
    <>
      <Sidebar
        collapsed={false}
        recents={[]}
        openSessions={[]}
        onFocusSession={() => {}}
        onCloseSession={() => {}}
        recentWorkspaces={[]}
        onPickWorkspace={() => { bridge.openSession('D:\\should-not') }}
        onSwitchWorkspace={() => { bridge.pickWorkspaceFolder() }}
        onDelete={() => {}}
        view={view}
        onNav={setView}
        onNew={() => {}}
        onResume={() => {}}
        onLogout={() => {}}
        onOpenAutomation={() => setOpen(true)}
      />
      <div data-testid="view">{view}</div>
      <AutomationWorkspaceDialog open={open} onClose={() => setOpen(false)} onOpenRun={async () => {}} />
    </>
  )
}

function textOf(selector: string) {
  return [...host.querySelectorAll(selector)].map((el) => el.textContent?.trim() ?? '')
}

describe('more menu', () => {
  it('opens 自动化任务 under 市场 without leaving the current view or starting a session', async () => {
    await act(async () => { root.render(<Harness />) })
    const navText = textOf('nav button, nav div')
    const marketAt = navText.findIndex((t) => t.includes('市场'))
    const moreAt = navText.findIndex((t) => t === '更多' || t.includes('更多'))
    expect(marketAt).toBeGreaterThanOrEqual(0)
    expect(moreAt).toBeGreaterThan(marketAt)

    const more = [...host.querySelectorAll('button')].find((b) => b.textContent?.includes('更多'))
    expect(more).toBeTruthy()
    await act(async () => { more!.click() })
    expect(host.textContent).toContain('自动化任务')
    expect(host.textContent).not.toContain('浏览更多插件')
    expect(host.textContent).not.toContain('创建你的插件')
    expect(host.querySelector('[data-testid="view"]')?.textContent).toBe('chat')

    const entry = [...host.querySelectorAll('button')].find((b) => b.textContent?.includes('自动化任务'))
    expect(entry).toBeTruthy()
    await act(async () => { entry!.click() })
    expect(host.querySelector('[role="dialog"][aria-label="自动化任务"]')).toBeTruthy()
    expect(host.querySelector('[data-testid="view"]')?.textContent).toBe('chat')
    expect(bridge.openSession).not.toHaveBeenCalled()
    expect(bridge.pickWorkspaceFolder).not.toHaveBeenCalled()
    expect(bridge.resumeSession).not.toHaveBeenCalled()
  })
})

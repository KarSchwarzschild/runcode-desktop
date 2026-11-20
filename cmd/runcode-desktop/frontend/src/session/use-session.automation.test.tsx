// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const bridge = vi.hoisted(() => ({
  openAutomationSession: vi.fn(),
}))

vi.mock('@/core/bridge', () => ({
  deleteSession: vi.fn(),
  errText: (error: unknown) => error instanceof Error ? error.message : String(error),
  listSessions: vi.fn(async () => []),
  loadConfig: vi.fn(async () => ({})),
  newSession: vi.fn(),
  openSession: vi.fn(),
  openSessions: vi.fn(async () => []),
  focusSession: vi.fn(),
  closeSession: vi.fn(),
  sessionStatus: vi.fn(),
  onEnvelope: vi.fn(() => () => {}),
  Events: {
    SessionRenamed: 'session:renamed',
    SessionStatus: 'session:status',
    AutomationRunChanged: 'automation:run_changed',
    AutomationTaskChanged: 'automation:task_changed',
  },
  pickWorkspaceFolder: vi.fn(),
  resumeSession: vi.fn(),
  openAutomationSession: bridge.openAutomationSession,
  setPermissionMode: vi.fn(),
  setPlanMode: vi.fn(),
  setReasoningScenario: vi.fn(),
  setThinkingEffort: vi.fn(),
  startSession: vi.fn(),
  switchModel: vi.fn(),
}))

import { useSession, type Session } from './use-session'

let root: Root
let host: HTMLDivElement
let latest: Session

function Harness() {
  latest = useSession({
    busy: false,
    conversation: {
      reset: vi.fn(),
      applyResumed: vi.fn(async () => {}),
      pushError: vi.fn(),
      dropSession: vi.fn(),
      hasLiveBlocks: vi.fn(() => false),
      noteAutomationPending: vi.fn(),
      mergeAutomationHistory: vi.fn(),
    },
    showToast: vi.fn(),
    onEnterChat: vi.fn(),
  })
  return null
}

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  bridge.openAutomationSession.mockReset()
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})

describe('automation session integration', () => {
  it('opens a linked task session without switching to a replacement chat', async () => {
    const context = { viewId: 'view', epoch: 1 }
    const resumed = { info: { sessionId: 'session-linked', model: 'test-model' }, blocks: [] }
    bridge.openAutomationSession.mockResolvedValue({
      context,
      session: resumed,
      historyPending: false,
    })
    await act(async () => root.render(<Harness />))
    expect(typeof latest.openAutomationSession).toBe('function')

    await act(async () => {
      await latest.openAutomationSession({ context, taskId: 'task-1', runId: 'run-1' })
    })

    expect(bridge.openAutomationSession).toHaveBeenCalledWith({ context, taskId: 'task-1', runId: 'run-1' })
    expect(latest.info?.sessionId).toBe('session-linked')
    expect(latest.started).toBe(true)
  })
})

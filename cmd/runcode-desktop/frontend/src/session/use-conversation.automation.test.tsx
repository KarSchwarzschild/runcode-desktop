// @vitest-environment jsdom
import { act, useRef } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const callbacks = new Map<string, (envelope: unknown) => void>()

vi.mock('@/core/bridge', () => ({
  submitQuestion: vi.fn(),
  compact: vi.fn(),
  Events: {
    AssistantDelta: 'assistant:delta',
    AssistantThinking: 'assistant:thinking',
    Retry: 'retry',
    ToolEvent: 'tool:event',
    ContextCompaction: 'context:compaction',
    ContextUsage: 'context:usage',
    TurnEnd: 'turn:end',
    TurnError: 'turn:error',
    Warning: 'warning',
    SessionStatus: 'session:status',
    AutomationContextChanged: 'automation:context_changed',
    AutomationRunChanged: 'automation:run_changed',
  },
  errText: (error: unknown) => error instanceof Error ? error.message : String(error),
  interrupt: vi.fn(),
  listEdits: vi.fn(),
  getAutomationDefaults: vi.fn(async () => ({
    context: { viewId: 'view-a', epoch: 1 },
    available: true,
  })),
  getAutomationConversation: vi.fn(),
  listSessionAutomationRuns: vi.fn(),
  onEnvelope: (event: string, callback: (envelope: unknown) => void) => {
    callbacks.set(event, callback)
    return () => callbacks.delete(event)
  },
  revertEdit: vi.fn(),
  releaseArtifact: vi.fn(),
  parseToolInput: vi.fn(),
}))

import { useConversation, type Conversation } from './use-conversation'

let root: Root
let host: HTMLDivElement
let latest: Conversation

function Harness() {
  const infoRef = useRef({
    sessionId: 'session-1',
    cwd: 'C:\\workspace',
    model: 'test-model',
  } as never)
  latest = useConversation({
    focusedId: 'session-1',
    infoRef,
    permissions: { pending: null, enqueue: vi.fn(), clear: vi.fn() },
    onFilesChanged: vi.fn(),
    onOpenPreview: vi.fn(),
    showToast: vi.fn(),
  })
  return null
}

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  callbacks.clear()
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})

describe('automation event integration', () => {
  it('drops an output envelope from a stale automation context', async () => {
    await act(async () => root.render(<Harness />))
    await act(async () => {
      callbacks.get('automation:context_changed')?.({
        sessionId: '',
        payload: { context: { viewId: 'view-b', epoch: 1 } },
      })
    })

    await act(async () => {
      callbacks.get('assistant:delta')?.({
        sessionId: 'session-1',
        seq: 1,
        payload: { text: 'STALE' },
        automationSource: {
          context: { viewId: 'view-a', epoch: 1 },
          runId: 'run-stale',
        },
      })
    })

    expect(latest.blocks).toEqual([])
  })
})

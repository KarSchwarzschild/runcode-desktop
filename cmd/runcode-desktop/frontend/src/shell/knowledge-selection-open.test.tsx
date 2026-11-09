// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessionKnowledge } from '@/session/use-knowledge'
import type { KnowledgeState } from '@/session/knowledge-state'
import { KnowledgeSelection } from './knowledge-selection'

const openKnowledgeDirectory = vi.hoisted(() => vi.fn(async () => undefined))

vi.mock('@/core/bridge', () => ({
  openKnowledgeDirectory,
  errText: (error: unknown) => (error instanceof Error ? error.message : String(error)),
}))

const library = (id: string, title: string) => ({
  id, title, description: '', path: '', source: 'real', documentCount: 0, documentCountKnown: false,
  directoryUrl: `https://aibase-preview.ouc-online.com.cn/knowledge-base/${id}/data-collection`,
})

describe('KnowledgeSelection open directory', () => {
  let root: Root
  let host: HTMLDivElement
  beforeEach(() => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    openKnowledgeDirectory.mockReset()
    host = document.createElement('div')
    document.body.append(host)
    root = createRoot(host)
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    host.remove()
  })

  it('opensAuthorizedDirectoryInSystemBrowserOnRowClick', async () => {
    const notify = vi.fn()
    const state: KnowledgeState = {
      sessionId: 'A', contextVersion: 1, revision: 1,
      libraries: [
        library('08df3e2d-2959-4cbf-863c-9de41ffd0d7c', '金箍棒和bvvx'),
        library('8o9jbn', '8o9jbn'),
      ],
      pendingIds: null, items: [], previewItems: [], mode: 'real', catalogMode: 'real', unavailableReason: '',
      loading: false, saving: false, listing: false, refreshing: false, fromCache: false, catalogUpdatedAt: 0,
      loaded: true, error: '', listError: '', query: '', page: 1, pageSize: 10, total: 2, hasMore: false, maxSelection: 5,
    }
    const knowledge = {
      state,
      visibleLibraries: state.libraries,
      save: vi.fn(async () => {}),
      remove: vi.fn(async () => {}),
      reload: vi.fn(async () => {}),
      sendBlock: '',
      search: vi.fn(),
      loadMore: vi.fn(async () => {}),
    } as unknown as SessionKnowledge
    await act(async () => root.render(<KnowledgeSelection knowledge={knowledge} onFocusComposer={() => {}} onNotify={notify} />))
    await act(async () => {
      host.querySelector<HTMLButtonElement>('button[aria-label="已选 2 个知识库"]')!.click()
    })
    const openBtn = Array.from(host.querySelectorAll('button')).find((button) => button.getAttribute('aria-label') === '打开知识库 金箍棒和bvvx')
    expect(openBtn).toBeDefined()
    await act(async () => { openBtn!.click() })
    expect(openKnowledgeDirectory).toHaveBeenCalledWith('A', '08df3e2d-2959-4cbf-863c-9de41ffd0d7c')
    expect(notify).not.toHaveBeenCalled()
  })

  it('doesNotOpenDirectoryInDemoMode', async () => {
    const state: KnowledgeState = {
      sessionId: 'A', contextVersion: 1, revision: 1,
      libraries: [library('demo-1', '测试库')],
      pendingIds: null, items: [], previewItems: [], mode: 'demo', catalogMode: 'demo', unavailableReason: '',
      loading: false, saving: false, listing: false, refreshing: false, fromCache: false, catalogUpdatedAt: 0,
      loaded: true, error: '', listError: '', query: '', page: 1, pageSize: 10, total: 1, hasMore: false, maxSelection: 5,
    }
    const knowledge = {
      state,
      visibleLibraries: state.libraries,
      save: vi.fn(async () => {}),
      remove: vi.fn(async () => {}),
      reload: vi.fn(async () => {}),
      sendBlock: '',
      search: vi.fn(),
      loadMore: vi.fn(async () => {}),
    } as unknown as SessionKnowledge
    await act(async () => root.render(<KnowledgeSelection knowledge={knowledge} onFocusComposer={() => {}} />))
    await act(async () => {
      host.querySelector<HTMLButtonElement>('button[aria-label="已选 1 个知识库"]')!.click()
    })
    expect(host.querySelector('button[aria-label="打开知识库 测试库"]')).toBeNull()
    expect(openKnowledgeDirectory).not.toHaveBeenCalled()
  })
})

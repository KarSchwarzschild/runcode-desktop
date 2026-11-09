// @vitest-environment jsdom
import { act, useSyncExternalStore } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it } from 'vitest'
import { KnowledgeController } from '@/session/knowledge-state'
import type { KnowledgeBaseSummary } from '@/core/bridge'
import { KnowledgeStatus } from './knowledge-picker'

describe('unavailable selected reference recovery', () => {
  it('auto-revalidates retained libraries after selection change without showing confirm copy', async () => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    const first: KnowledgeBaseSummary = { id: 'one', title: '第一个知识库', description: '', path: '', source: '基座知识库', documentCount: 3, documentCountKnown: true }
    const second: KnowledgeBaseSummary = { ...first, id: 'two', title: '保留的知识库', documentCount: 8, directoryUrl: 'https://ai.ouchn.edu.cn/knowledge-base/two/data-collection' }
    let selected = [first, second], reads = 0, revision = 0
    const controller = new KnowledgeController({
      get: async () => { reads++; return { sessionId: 'A', revision, libraries: selected.map(item => item.id === 'two' ? { ...second, title: reads > 1 ? '重新确认的标题' : second.title } : item), mode: 'real', unavailableReason: '' } },
      set: async request => {
        revision++
        selected = selected.filter(item => request.libraryIds?.includes(item.id))
        return { sessionId: 'A', revision, libraries: selected.map(item => ({ id: item.id, title: '不可用的知识库', description: '', path: '', source: '', documentCount: 0, documentCountKnown: false, unavailableReason: '无法确认访问权限，请重试或移除此知识库' })), mode: 'real', unavailableReason: '' }
      },
      list: async () => { throw new Error('unexpected catalog request') },
    })
    await controller.activate('A')
    const reload = () => { void controller.activate('A') }
    function StatusHost() {
      const state = useSyncExternalStore(controller.subscribe, controller.snapshot)
      return <KnowledgeStatus state={state} locked={false} onRetry={reload} />
    }
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    try {
      await act(async () => root.render(<StatusHost />))
      await act(async () => { await controller.save(['two']) })
      expect(reads).toBe(2)
      expect(controller.visibleLibraries()).toEqual([{ ...second, title: '重新确认的标题' }])
      expect(host.textContent).not.toContain('重新确认知识库')
      expect(host.textContent).not.toContain('部分知识库访问权限待确认')
      expect(host.textContent).not.toContain('无法确认访问权限')
    } finally {
      await act(async () => root.unmount())
      host.remove()
      controller.dispose()
    }
  })
})

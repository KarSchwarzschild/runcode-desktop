// @vitest-environment jsdom
import { act, useSyncExternalStore } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { KnowledgeController } from '@/session/knowledge-state'
import { KnowledgePicker } from './knowledge-picker'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

beforeEach(() => {
  ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
})

const row = (id: string) => ({ id, title: `知识库 ${id}`, description: '', path: '', source: '基座知识库', documentCount: 0, documentCountKnown: false })

function setListMetrics(list: HTMLElement, metrics: { clientHeight: number; scrollHeight: number; scrollTop: number }) {
  let scrollTop = metrics.scrollTop
  Object.defineProperty(list, 'clientHeight', { configurable: true, get: () => metrics.clientHeight })
  Object.defineProperty(list, 'scrollHeight', { configurable: true, get: () => metrics.scrollHeight })
  Object.defineProperty(list, 'scrollTop', {
    configurable: true,
    get: () => scrollTop,
    set: (value: number) => { scrollTop = value },
  })
}

function listRegion(host: HTMLElement) {
  return host.querySelector<HTMLDivElement>('[aria-label="知识库列表"]')!
}

function wheelDown(list: HTMLElement, deltaY = 40) {
  list.dispatchEvent(new WheelEvent('wheel', { deltaY, bubbles: true, cancelable: true }))
}

async function mountPicker(controller: KnowledgeController) {
  const noop = () => {}
  function Picker() {
    const state = useSyncExternalStore(controller.subscribe, controller.snapshot)
    return <KnowledgePicker open state={state} locked={false} onClose={noop} onSearch={query => { void controller.search(query) }} onLoadMore={() => { void controller.loadMore() }} onToggle={noop} onRetry={noop} />
  }
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  await act(async () => root.render(<Picker />))
  return {
    host,
    async unmount() {
      await act(async () => root.unmount())
      host.remove()
      controller.dispose()
    },
  }
}

describe('knowledge picker scrolling', () => {
  it('removesTheNormalLoadMoreButton', async () => {
    const pages: number[] = []
    const controller = new KnowledgeController({
      get: async () => ({ sessionId: 'A', revision: 0, libraries: [], mode: 'real', unavailableReason: '' }),
      set: async () => { throw new Error('unexpected save') },
      list: async request => {
        pages.push(request.page)
        if (request.page === 2) return { items: [], page: 2, pageSize: 10, total: -1, hasMore: true, mode: 'unavailable', unavailableReason: '目录服务暂不可用', maxSelection: 5 }
        return { items: [row('first')], page: 1, pageSize: 10, total: -1, hasMore: true, mode: 'real', unavailableReason: '', maxSelection: 5 }
      },
    })
    await controller.activate('A')
    await controller.search('')
    const mounted = await mountPicker(controller)
    try {
      expect(Array.from(mounted.host.querySelectorAll('button')).some(button => button.textContent === '加载更多')).toBe(false)
      await act(async () => { void controller.loadMore() })
      expect(mounted.host.querySelector('[role="alert"]')?.textContent).toContain('目录服务暂不可用')
      expect(Array.from(mounted.host.querySelectorAll('button')).some(button => button.textContent === '重试')).toBe(true)
      expect(pages).toEqual([1, 2])
    } finally {
      await mounted.unmount()
    }
  })

  it('loadsOnePageOnDownwardGestureNearBottom', async () => {
    const pages: number[] = []
    const controller = new KnowledgeController({
      get: async () => ({ sessionId: 'A', revision: 0, libraries: [], mode: 'real', unavailableReason: '' }),
      set: async () => { throw new Error('unexpected save') },
      list: async request => {
        pages.push(request.page)
        return {
          items: Array.from({ length: 10 }, (_, index) => row(`${request.page}-${index}`)),
          page: request.page,
          pageSize: 10,
          total: 20,
          hasMore: request.page < 2,
          mode: 'real',
          unavailableReason: '',
          maxSelection: 5,
        }
      },
    })
    await controller.activate('A')
    await controller.search('')
    const mounted = await mountPicker(controller)
    try {
      const list = listRegion(mounted.host)
      setListMetrics(list, { clientHeight: 322, scrollHeight: 460, scrollTop: 0 })
      await act(async () => { wheelDown(list); list.dispatchEvent(new Event('scroll')) })
      expect(pages).toEqual([1])

      setListMetrics(list, { clientHeight: 322, scrollHeight: 460, scrollTop: 120 })
      await act(async () => { list.dispatchEvent(new Event('resize')) })
      expect(pages).toEqual([1])

      setListMetrics(list, { clientHeight: 322, scrollHeight: 460, scrollTop: 120 })
      await act(async () => { wheelDown(list) })
      expect(pages).toEqual([1, 2])
      expect(mounted.host.textContent).toContain('知识库 2-0')
    } finally {
      await mounted.unmount()
    }
  })

  it('continuesShortAndFilteredEmptyPagesOnlyOnNewGestures', async () => {
    vi.useFakeTimers()
    const pages: number[] = []
    const controller = new KnowledgeController({
      get: async () => ({ sessionId: 'A', revision: 0, libraries: [], mode: 'real', unavailableReason: '' }),
      set: async () => { throw new Error('unexpected save') },
      list: async request => {
        pages.push(request.page)
        // 权限过滤空页：可见列表为空但仍有后续页，必须靠新手势继续，不能静置连翻。
        if (request.page < 3) return { items: [], page: request.page, pageSize: 10, total: -1, hasMore: true, mode: 'real', unavailableReason: '', maxSelection: 5 }
        return { items: [row('tail')], page: 3, pageSize: 10, total: -1, hasMore: false, mode: 'real', unavailableReason: '', maxSelection: 5 }
      },
    })
    await controller.activate('A')
    await controller.search('')
    const mounted = await mountPicker(controller)
    try {
      const list = listRegion(mounted.host)
      expect(mounted.host.textContent).toContain('本页暂无可显示的知识库，向下滚动继续查看')
      expect(pages).toEqual([1])

      // 无溢出空页：向下滚轮仍算底部意图。
      setListMetrics(list, { clientHeight: 322, scrollHeight: 46, scrollTop: 0 })
      await act(async () => { wheelDown(list) })
      expect(pages).toEqual([1, 2])
      expect(mounted.host.textContent).toContain('本页暂无可显示的知识库，向下滚动继续查看')

      await act(async () => { await vi.advanceTimersByTimeAsync(50) })
      expect(pages).toEqual([1, 2])

      await act(async () => { await vi.advanceTimersByTimeAsync(180) })
      await act(async () => { wheelDown(list) })
      expect(pages).toEqual([1, 2, 3])
      expect(mounted.host.textContent).toContain('知识库 tail')
      expect(mounted.host.textContent).not.toContain('本页暂无可显示的知识库，向下滚动继续查看')

      await act(async () => { await vi.advanceTimersByTimeAsync(200); wheelDown(list) })
      expect(pages).toEqual([1, 2, 3])
    } finally {
      await mounted.unmount()
    }
  })

  it('loadsAnotherPageFromAShortNonEmptyViewportWithoutOverflow', async () => {
    const pages: number[] = []
    const controller = new KnowledgeController({
      get: async () => ({ sessionId: 'A', revision: 0, libraries: [], mode: 'real', unavailableReason: '' }),
      set: async () => { throw new Error('unexpected save') },
      list: async request => {
        pages.push(request.page)
        if (request.page === 1) return { items: [row('a'), row('b'), row('c')], page: 1, pageSize: 10, total: -1, hasMore: true, mode: 'real', unavailableReason: '', maxSelection: 5 }
        return { items: [row('more')], page: 2, pageSize: 10, total: -1, hasMore: false, mode: 'real', unavailableReason: '', maxSelection: 5 }
      },
    })
    await controller.activate('A')
    await controller.search('')
    const mounted = await mountPicker(controller)
    try {
      const list = listRegion(mounted.host)
      setListMetrics(list, { clientHeight: 322, scrollHeight: 138, scrollTop: 0 })
      await act(async () => { wheelDown(list) })
      expect(pages).toEqual([1, 2])
      expect(mounted.host.textContent).toContain('知识库 a')
      expect(mounted.host.textContent).toContain('知识库 more')
    } finally {
      await mounted.unmount()
    }
  })

  it('mergesWheelBurstsAndDiscardsInputWhileLoading', async () => {
    vi.useFakeTimers()
    let releasePage2: (() => void) | undefined
    const pages: number[] = []
    const controller = new KnowledgeController({
      get: async () => ({ sessionId: 'A', revision: 0, libraries: [], mode: 'real', unavailableReason: '' }),
      set: async () => { throw new Error('unexpected save') },
      list: async request => {
        pages.push(request.page)
        if (request.page === 1) {
          return { items: Array.from({ length: 10 }, (_, index) => row(`p1-${index}`)), page: 1, pageSize: 10, total: 30, hasMore: true, mode: 'real', unavailableReason: '', maxSelection: 5 }
        }
        if (request.page === 2) {
          await new Promise<void>(resolve => { releasePage2 = resolve })
          return { items: Array.from({ length: 10 }, (_, index) => row(`p2-${index}`)), page: 2, pageSize: 10, total: 30, hasMore: true, mode: 'real', unavailableReason: '', maxSelection: 5 }
        }
        return { items: Array.from({ length: 10 }, (_, index) => row(`p${request.page}-${index}`)), page: request.page, pageSize: 10, total: 30, hasMore: request.page < 3, mode: 'real', unavailableReason: '', maxSelection: 5 }
      },
    })
    await controller.activate('A')
    await controller.search('')
    const mounted = await mountPicker(controller)
    try {
      const list = listRegion(mounted.host)
      setListMetrics(list, { clientHeight: 322, scrollHeight: 460, scrollTop: 140 })
      await act(async () => {
        wheelDown(list)
        wheelDown(list)
        wheelDown(list)
      })
      expect(pages).toEqual([1, 2])

      await act(async () => { wheelDown(list); wheelDown(list) })
      expect(pages).toEqual([1, 2])

      await act(async () => { releasePage2?.() })
      expect(pages).toEqual([1, 2])
      expect(mounted.host.textContent).toContain('知识库 p2-0')

      // 响应完成后不自动续页；新手势才请求第三页。
      setListMetrics(list, { clientHeight: 322, scrollHeight: 920, scrollTop: 580 })
      await act(async () => { await vi.advanceTimersByTimeAsync(200); wheelDown(list) })
      expect(pages).toEqual([1, 2, 3])
    } finally {
      await mounted.unmount()
    }
  })

  it('keeps rows actionable and retries an unavailable continuation from the picker', async () => {
    const pages: number[] = []
    let fail = true, initialFail = false
    const controller = new KnowledgeController({
      get: async () => ({ sessionId: 'A', revision: 0, libraries: [], mode: 'real', unavailableReason: '' }),
      set: async () => { throw new Error('unexpected save') },
      list: async request => {
        pages.push(request.page)
        if (initialFail || (request.page === 2 && fail)) return { items: [], page: request.page, pageSize: 10, total: 0, hasMore: false, mode: 'unavailable', unavailableReason: '目录服务暂不可用', maxSelection: 5 }
        return { items: [row(request.page === 1 ? 'first' : 'last')], page: request.page, pageSize: 10, total: -1, hasMore: request.page === 1, mode: 'real', unavailableReason: '', maxSelection: 5 }
      },
    })
    await controller.activate('A')
    await controller.search('')
    const mounted = await mountPicker(controller)
    try {
      await act(async () => { void controller.loadMore() })
      expect(mounted.host.querySelector('[role="alert"]')?.textContent).toContain('目录服务暂不可用')
      expect(mounted.host.querySelector<HTMLButtonElement>('button[aria-pressed]')?.disabled).toBe(false)
      expect(mounted.host.textContent).not.toContain('0 个知识库')
      expect(Array.from(mounted.host.querySelectorAll('button')).some(button => button.textContent === '加载更多')).toBe(false)

      const list = listRegion(mounted.host)
      setListMetrics(list, { clientHeight: 322, scrollHeight: 46, scrollTop: 0 })
      await act(async () => { wheelDown(list) })
      expect(pages).toEqual([1, 2])

      fail = false
      await act(async () => mounted.host.querySelector<HTMLButtonElement>('[role="alert"] button')!.click())
      expect(pages).toEqual([1, 2, 2])
      expect(mounted.host.querySelector('[role="alert"]')).toBeNull()
      expect(mounted.host.textContent).toContain('知识库 first')
      expect(mounted.host.textContent).toContain('知识库 last')

      initialFail = true
      await act(async () => controller.search(''))
      expect(controller.snapshot()).toMatchObject({ mode: 'unavailable', page: 0, hasMore: false })
      expect(mounted.host.querySelector('[role="alert"]')?.textContent ?? '').toContain('目录服务暂不可用')
      expect(mounted.host.textContent).not.toContain('暂无知识库')
      expect(mounted.host.textContent).not.toContain('0 个知识库')
    } finally {
      await mounted.unmount()
    }
  })

  it('ignores late loadMore results after a newer search generation', async () => {
    let resolveSlow: ((page: { items: ReturnType<typeof row>[]; page: number; pageSize: number; total: number; hasMore: boolean; mode: string; unavailableReason: string; maxSelection: number }) => void) | undefined
    const pages: number[] = []
    const controller = new KnowledgeController({
      get: async () => ({ sessionId: 'A', revision: 0, libraries: [], mode: 'real', unavailableReason: '' }),
      set: async () => { throw new Error('unexpected save') },
      list: async request => {
        pages.push(request.page)
        if (request.query === '' && request.page === 2) {
          return await new Promise(resolve => { resolveSlow = resolve })
        }
        if (request.query === 'alpha') {
          return { items: [row('alpha')], page: 1, pageSize: 10, total: 1, hasMore: false, mode: 'real', unavailableReason: '', maxSelection: 5 }
        }
        return { items: [row('base')], page: 1, pageSize: 10, total: 2, hasMore: true, mode: 'real', unavailableReason: '', maxSelection: 5 }
      },
    })
    await controller.activate('A')
    await controller.search('')
    const mounted = await mountPicker(controller)
    try {
      const list = listRegion(mounted.host)
      setListMetrics(list, { clientHeight: 46, scrollHeight: 46, scrollTop: 0 })
      await act(async () => { wheelDown(list) })
      expect(pages).toEqual([1, 2])
      await act(async () => { void controller.search('alpha') })
      await act(async () => {
        resolveSlow?.({ items: [row('late')], page: 2, pageSize: 10, total: 2, hasMore: false, mode: 'real', unavailableReason: '', maxSelection: 5 })
      })
      expect(controller.snapshot().items.map(item => item.id)).toEqual(['alpha'])
      expect(mounted.host.textContent).toContain('知识库 alpha')
      expect(mounted.host.textContent).not.toContain('知识库 late')
    } finally {
      await mounted.unmount()
    }
  })
})

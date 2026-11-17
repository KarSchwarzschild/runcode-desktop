// @vitest-environment jsdom
import { act, createElement } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { InlinePreview } from './inline-preview'
import { InlinePreviewBudgetProvider } from './inline-preview-budget'

const mermaid = vi.hoisted(() => ({ render: vi.fn() }))
vi.mock('@/preview/static-mermaid-loader', () => ({
  renderMermaid: (...args: unknown[]) => mermaid.render(...args),
}))

let host: HTMLDivElement
let root: Root
const revoke = vi.fn()
const createUrl = vi.fn((blob: Blob) => `blob:test-${blob.size}-${Math.random()}`)

beforeEach(() => {
  ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  revoke.mockReset()
  createUrl.mockClear()
  mermaid.render.mockReset()
  vi.stubGlobal('URL', {
    ...URL,
    createObjectURL: createUrl,
    revokeObjectURL: revoke,
  })
  vi.stubGlobal('IntersectionObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.unstubAllGlobals()
})

const svgOk = '<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>'

describe('InlinePreview lifecycle', () => {
  it('SVG 源变更与卸载时释放 Blob URL', async () => {
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'svg', source: svgOk, defaultNode: createElement('code', null, 'fallback'),
      }))
    })
    expect(createUrl).toHaveBeenCalled()
    const firstUrl = createUrl.mock.results[0]?.value as string
    expect(host.querySelector('img')?.getAttribute('src')).toBe(firstUrl)

    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'svg',
        source: '<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><circle r="5"/></svg>',
        defaultNode: createElement('code', null, 'fallback'),
      }))
    })
    expect(revoke).toHaveBeenCalledWith(firstUrl)

    const secondUrl = createUrl.mock.results[createUrl.mock.results.length - 1]?.value as string
    await act(async () => root.unmount())
    root = createRoot(host)
    expect(revoke).toHaveBeenCalledWith(secondUrl)
  })

  it('Mermaid 过期异步结果不回填；卸载释放 URL', async () => {
    let resolve!: (v: { ok: true; svg: string; width: number; height: number }) => void
    mermaid.render.mockImplementation(() => new Promise((yes) => { resolve = yes }))
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'mermaid', source: 'graph TD;A-->B', defaultNode: createElement('code', null, 'src'),
      }))
    })
    // 换源使上一代作废
    let resolve2!: (v: { ok: true; svg: string; width: number; height: number }) => void
    mermaid.render.mockImplementation(() => new Promise((yes) => { resolve2 = yes }))
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'mermaid', source: 'graph TD;C-->D', defaultNode: createElement('code', null, 'src'),
      }))
    })
    await act(async () => resolve({ ok: true, svg: '<svg xmlns="http://www.w3.org/2000/svg"/>', width: 1, height: 1 }))
    expect(host.querySelector('img')).toBeNull()

    await act(async () => resolve2({ ok: true, svg: '<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>', width: 2, height: 2 }))
    expect(host.querySelector('img')).toBeTruthy()
    const url = host.querySelector('img')!.getAttribute('src')!
    await act(async () => root.unmount())
    root = createRoot(host)
    expect(revoke).toHaveBeenCalledWith(url)
  })

  it('HTML 在预算 Provider 内渲染占位或 iframe，失败才回退源码', async () => {
    const html = '<!DOCTYPE html><html><body><p>hi</p></body></html>'
    await act(async () => {
      root.render(createElement(
        InlinePreviewBudgetProvider,
        null,
        createElement(InlinePreview, {
          kind: 'html', source: html, defaultNode: createElement('pre', null, 'SOURCE'),
        }),
      ))
    })
    // 未触发 IntersectionObserver 回调前是占位，不是 SOURCE，也不是失败
    expect(host.textContent).not.toContain('SOURCE')
    expect(host.querySelector('[aria-hidden]')).toBeTruthy()
  })

  it('失败时保留源码并显示可读原因', async () => {
    mermaid.render.mockResolvedValue({ ok: false, reason: 'unsupported-type' })
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'mermaid',
        source: 'pie\n"a": 1',
        defaultNode: createElement('code', null, 'SRC'),
      }))
    })
    await act(async () => {})
    expect(host.textContent).toContain('SRC')
    expect(host.textContent).toContain('未显示预览')
    expect(host.textContent).toMatch(/暂不支持|unsupported-type|预览不可用/)
  })

  it('提供标题和仅代码/图表两态切换，并在预览态显示全屏入口', async () => {
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'svg', source: svgOk, defaultNode: createElement('code', null, 'SOURCE'),
      }))
    })
    expect(host.textContent).toContain('SVG 预览')
    expect(host.querySelector('button[title="全屏"]')).toBeTruthy()

    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '视图') as HTMLButtonElement).click())
    expect(host.textContent).toContain('仅显示图表')
    expect(host.textContent).toContain('仅显示代码')
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '仅显示代码') as HTMLButtonElement).click())
    expect(host.textContent).toContain('SOURCE')
    expect(host.querySelector('img')).toBeNull()
    expect(host.querySelector('button[title="全屏"]')).toBeNull()

    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '视图') as HTMLButtonElement).click())
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '仅显示图表') as HTMLButtonElement).click())
    expect(host.querySelector('img')).toBeTruthy()
    expect(host.querySelector('button[title="全屏"]')).toBeTruthy()
  })

  it('HTML 有双态切换但不提供图片全屏', async () => {
    await act(async () => {
      root.render(createElement(InlinePreviewBudgetProvider, null,
        createElement(InlinePreview, {
          kind: 'html',
          source: '<!doctype html><html><body><p>页面</p></body></html>',
          defaultNode: createElement('code', null, 'HTML SOURCE'),
        }),
      ))
    })
    await act(async () => { await Promise.resolve() })
    expect(host.textContent).toContain('HTML 预览')
    expect(host.querySelector('button[title="全屏"]')).toBeNull()
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '视图') as HTMLButtonElement).click())
    expect(host.textContent).toContain('仅显示页面')
    expect(host.textContent).toContain('仅显示代码')
  })

  it('Mermaid 成功渲染后使用 Mermaid 预览标题', async () => {
    mermaid.render.mockResolvedValue({
      ok: true,
      svg: '<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10"><rect width="20" height="10"/></svg>',
      width: 20,
      height: 10,
    })
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'mermaid', source: 'graph TD;A-->B', defaultNode: createElement('code', null, 'SOURCE'),
      }))
    })
    await act(async () => { await Promise.resolve() })
    expect(host.textContent).toContain('Mermaid 预览')
    expect(host.querySelector('button[title="全屏"]')).toBeTruthy()
  })

  it('全屏按钮打开客户端图片遮罩，单击图片后关闭', async () => {
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'svg', source: svgOk, defaultNode: createElement('code', null, 'SOURCE'),
      }))
    })
    await act(async () => (host.querySelector('button[title="全屏"]') as HTMLButtonElement).click())
    const dialog = document.body.querySelector('[role="dialog"]') as HTMLElement
    expect(dialog).toBeTruthy()
    const imageButton = dialog.querySelector('img')?.closest('button') as HTMLButtonElement
    await act(async () => imageButton.click())
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
  })
})

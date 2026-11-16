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
})

// @vitest-environment jsdom
import { act, createElement } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { InlinePreview } from './inline-preview'
import { InlinePreviewBudgetProvider } from './inline-preview-budget'
import { PREVIEW_LIMITS, htmlInlineScale } from '@/preview/static-detect'

const mermaid = vi.hoisted(() => ({ render: vi.fn() }))
const bridge = vi.hoisted(() => ({
  copyText: vi.fn(async (_text: string) => {}),
}))
vi.mock('@/preview/static-mermaid-loader', () => ({
  renderMermaid: (...args: unknown[]) => mermaid.render(...args),
}))
vi.mock('@/core/bridge', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/core/bridge')>()
  return { ...actual, copyText: (text: string) => bridge.copyText(text) }
})

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
  vi.restoreAllMocks()
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

  it('相同 Mermaid 源码在父组件重渲染时只调用一次 render', async () => {
    mermaid.render.mockResolvedValue({
      ok: true,
      svg: '<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10"><rect width="20" height="10"/></svg>',
      width: 20,
      height: 10,
    })
    const draw = (onOpenCodePreview: () => void) => root.render(createElement(InlinePreview, {
      kind: 'mermaid',
      source: 'graph TD;A-->B',
      defaultNode: createElement('code', null, 'SOURCE'),
      messageId: 'm1',
      previewId: '0:10',
      relatedPaths: [],
      onOpenCodePreview,
    }))
    await act(async () => draw(() => {}))
    await act(async () => { await Promise.resolve() })
    expect(mermaid.render).toHaveBeenCalledTimes(1)

    // 模拟输入框更新导致父组件重建回调；相同源码不应再次进入 Mermaid。
    await act(async () => draw(() => {}))
    await act(async () => { await Promise.resolve() })
    expect(mermaid.render).toHaveBeenCalledTimes(1)
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

  it('SVG／Mermaid 图面单击不再打开右侧预览；全屏仍可用；HTML 仍从标题进入', async () => {
    const open = vi.fn()
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'svg',
        source: svgOk,
        defaultNode: createElement('code', null, 'SOURCE'),
        messageId: 'm1',
        previewId: '0:10',
        sessionId: 'S',
        relatedPaths: ['charts/a.svg'],
        onOpenCodePreview: open,
      }))
    })
    const svgImg = host.querySelector('img') as HTMLImageElement
    expect(svgImg.closest('button[title="在右侧预览打开"]')).toBeNull()
    await act(async () => svgImg.click())
    expect(open).not.toHaveBeenCalled()
    expect(host.querySelector('button[title="全屏"]')).toBeTruthy()

    mermaid.render.mockResolvedValue({
      ok: true,
      svg: '<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10"><rect width="20" height="10"/></svg>',
      width: 20,
      height: 10,
    })
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'mermaid',
        source: 'graph TD;A-->B',
        defaultNode: createElement('code', null, 'SOURCE'),
        messageId: 'm1',
        previewId: '0:10',
        sessionId: 'S',
        relatedPaths: ['charts/flow.mmd'],
        onOpenCodePreview: open,
      }))
    })
    await act(async () => { await Promise.resolve() })
    await act(async () => (host.querySelector('img') as HTMLImageElement).click())
    expect(open).not.toHaveBeenCalled()

    await act(async () => {
      root.render(createElement(InlinePreviewBudgetProvider, null,
        createElement(InlinePreview, {
          kind: 'html',
          source: '<!doctype html><html><body><p>页面</p></body></html>',
          defaultNode: createElement('code', null, 'SOURCE'),
          messageId: 'm2',
          previewId: '11:20',
          sessionId: 'S',
          onOpenCodePreview: open,
        }),
      ))
    })
    await act(async () => { await Promise.resolve() })
    const titleButton = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'HTML 预览') as HTMLButtonElement
    expect(titleButton).toBeTruthy()
    await act(async () => titleButton.click())
    expect(open).toHaveBeenLastCalledWith(expect.objectContaining({
      kind: 'inline-static',
      ownerKey: 'inline:S:m2:11:20',
      sourceKind: 'html',
    }))
  })

  it('SVG 和 Mermaid 可下载清理后的 SVG，HTML 不显示下载', async () => {
    const anchorClick = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'svg',
        source: svgOk,
        defaultNode: createElement('code', null, 'SOURCE'),
        messageId: 'm1',
        previewId: '0:10',
      }))
    })
    const button = host.querySelector('button[title="下载 SVG"]') as HTMLButtonElement
    expect(button).toBeTruthy()
    await act(async () => button.click())
    expect(anchorClick).toHaveBeenCalledTimes(1)
    const download = anchorClick.mock.instances[0] as HTMLAnchorElement
    expect(download.download).toMatch(/^code-svg-[a-z0-9-]+\.svg$/)
    expect(revoke).toHaveBeenCalled()

    mermaid.render.mockResolvedValue({
      ok: true,
      svg: '<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10"><rect width="20" height="10"/></svg>',
      width: 20,
      height: 10,
    })
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'mermaid',
        source: 'graph TD;A-->B',
        defaultNode: createElement('code', null, 'SOURCE'),
        messageId: 'm2',
        previewId: '11:20',
      }))
    })
    expect(host.querySelector('button[title="下载 SVG"]')).toBeTruthy()
    anchorClick.mockRestore()

    await act(async () => {
      root.render(createElement(InlinePreviewBudgetProvider, null,
        createElement(InlinePreview, {
          kind: 'html',
          source: '<!doctype html><html><body><p>页面</p></body></html>',
          defaultNode: createElement('code', null, 'SOURCE'),
          messageId: 'm3',
          previewId: '21:30',
        }),
      ))
    })
    await act(async () => { await Promise.resolve() })
    expect(host.querySelector('button[title="下载 SVG"]')).toBeNull()
  })

  it('流式内容闭合后等待稳定窗口，再开始渲染', async () => {
    vi.useFakeTimers()
    try {
      await act(async () => {
        root.render(createElement(InlinePreview, {
          kind: 'svg',
          source: svgOk,
          defaultNode: createElement('code', null, 'STREAMING SOURCE'),
          streaming: true,
        }))
      })
      expect(createUrl).not.toHaveBeenCalled()
      await act(async () => { vi.advanceTimersByTime(199) })
      expect(createUrl).not.toHaveBeenCalled()
      await act(async () => { vi.advanceTimersByTime(1) })
      expect(createUrl).toHaveBeenCalledTimes(1)
      expect(host.querySelector('img')).toBeTruthy()
    } finally {
      vi.useRealTimers()
    }
  })

  it('流式内容更新时取消旧渲染，只展示稳定窗口后的新内容', async () => {
    vi.useFakeTimers()
    try {
      await act(async () => {
        root.render(createElement(InlinePreview, {
          kind: 'svg',
          source: svgOk,
          defaultNode: createElement('code', null, 'OLD SOURCE'),
          streaming: true,
        }))
      })
      await act(async () => { vi.advanceTimersByTime(200) })
      const oldUrl = host.querySelector('img')?.getAttribute('src')
      expect(oldUrl).toBeTruthy()

      await act(async () => {
        root.render(createElement(InlinePreview, {
          kind: 'svg',
          source: '<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><circle r="5"/></svg>',
          defaultNode: createElement('code', null, 'NEW SOURCE'),
          streaming: true,
        }))
      })
      expect(host.querySelector('img')).toBeNull()
      expect(revoke).toHaveBeenCalledWith(oldUrl)
      await act(async () => { vi.advanceTimersByTime(199) })
      expect(createUrl).toHaveBeenCalledTimes(1)
      await act(async () => { vi.advanceTimersByTime(1) })
      expect(createUrl).toHaveBeenCalledTimes(2)
      expect(host.querySelector('img')?.getAttribute('src')).not.toBe(oldUrl)
    } finally {
      vi.useRealTimers()
    }
  })

  it('TurnEnd 后沿用已渲染结果，不重新渲染也不闪回源码', async () => {
    vi.useFakeTimers()
    try {
      await act(async () => {
        root.render(createElement(InlinePreview, {
          kind: 'svg',
          source: svgOk,
          defaultNode: createElement('code', null, 'SOURCE'),
          streaming: true,
        }))
      })
      await act(async () => { vi.advanceTimersByTime(200) })
      expect(createUrl).toHaveBeenCalledTimes(1)
      expect(host.querySelector('img')).toBeTruthy()

      await act(async () => {
        root.render(createElement(InlinePreview, {
          kind: 'svg',
          source: svgOk,
          defaultNode: createElement('code', null, 'SOURCE'),
          streaming: false,
        }))
      })
      expect(createUrl).toHaveBeenCalledTimes(1)
      expect(host.querySelector('img')).toBeTruthy()
      expect(host.textContent).not.toContain('SOURCE')
    } finally {
      vi.useRealTimers()
    }
  })

  it('内部异常只显示安全短原因，不回显异常正文', async () => {
    mermaid.render.mockRejectedValue(new Error('internal-stack-secret'))
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'mermaid',
        source: 'graph TD;A-->B',
        defaultNode: createElement('code', null, 'SOURCE'),
      }))
    })
    await act(async () => { await Promise.resolve() })
    expect(host.textContent).toContain('SOURCE')
    expect(host.textContent).toContain('未显示预览')
    expect(host.textContent).not.toContain('internal-stack-secret')
  })

  it('预览态无复制；源码态出现幽灵复制钮且写入原始 source', async () => {
    bridge.copyText.mockClear()
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'svg', source: svgOk, defaultNode: createElement('code', null, 'SOURCE'),
      }))
    })
    expect(host.querySelector('button[title="复制源码"]')).toBeNull()
    expect(Array.from(host.querySelectorAll('button')).some((b) => b.textContent?.includes('复制'))).toBe(false)

    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '视图') as HTMLButtonElement).click())
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '仅显示代码') as HTMLButtonElement).click())
    const copyBtn = host.querySelector('button[title="复制源码"]') as HTMLButtonElement
    expect(copyBtn).toBeTruthy()
    expect(copyBtn.className).not.toMatch(/bg-ink/)
    expect(copyBtn.className).toMatch(/text-muted/)
    await act(async () => copyBtn.click())
    expect(bridge.copyText).toHaveBeenCalledWith(svgOk)

    mermaid.render.mockResolvedValue({
      ok: true,
      svg: '<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10"><rect width="20" height="10"/></svg>',
      width: 20,
      height: 10,
    })
    const mmd = 'flowchart LR\nA-->B'
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'mermaid', source: mmd, defaultNode: createElement('code', null, 'SOURCE'),
      }))
    })
    await act(async () => { await Promise.resolve() })
    // 同 root 重渲会保留 view state；先切回预览态再断言复制隐藏。
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '视图') as HTMLButtonElement).click())
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '仅显示图表') as HTMLButtonElement).click())
    expect(host.querySelector('button[title="复制源码"]')).toBeNull()
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '视图') as HTMLButtonElement).click())
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '仅显示代码') as HTMLButtonElement).click())
    bridge.copyText.mockClear()
    await act(async () => (host.querySelector('button[title="复制源码"]') as HTMLButtonElement).click())
    expect(bridge.copyText).toHaveBeenCalledWith(mmd)

    const html = '<!doctype html><html><body><p>页面</p></body></html>'
    await act(async () => {
      root.render(createElement(InlinePreviewBudgetProvider, null,
        createElement(InlinePreview, {
          kind: 'html', source: html, defaultNode: createElement('code', null, 'SOURCE'),
        }),
      ))
    })
    await act(async () => { await Promise.resolve() })
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '视图') as HTMLButtonElement).click())
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '仅显示页面') as HTMLButtonElement).click())
    expect(host.querySelector('button[title="复制源码"]')).toBeNull()
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '视图') as HTMLButtonElement).click())
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '仅显示代码') as HTMLButtonElement).click())
    bridge.copyText.mockClear()
    await act(async () => (host.querySelector('button[title="复制源码"]') as HTMLButtonElement).click())
    expect(bridge.copyText).toHaveBeenCalledWith(html)
  })

  it('源码视图限高并可显示折行，复制仍走原始 source', async () => {
    bridge.copyText.mockClear()
    const longLine = 'x'.repeat(200)
    await act(async () => {
      root.render(createElement(InlinePreview, {
        kind: 'svg',
        source: svgOk,
        defaultNode: createElement('pre', null, longLine),
      }))
    })
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '视图') as HTMLButtonElement).click())
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '仅显示代码') as HTMLButtonElement).click())
    const shell = host.querySelector('[data-inline-source-shell]') as HTMLElement
    expect(shell).toBeTruthy()
    expect(shell.className).toMatch(/overflow-y-auto/)
    expect(shell.className).toMatch(/break-words|whitespace-pre-wrap|overflow-wrap/)
    expect(shell.style.maxHeight || shell.className).toBeTruthy()
    await act(async () => (Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.includes('复制')) as HTMLButtonElement).click())
    expect(bridge.copyText).toHaveBeenCalledWith(svgOk)
  })

  it('htmlInlineScale 按壳宽铺满设计宽（可 >1），窄宽度时缩小', () => {
    expect(htmlInlineScale(390, 390)).toBe(1)
    expect(htmlInlineScale(780, 390)).toBe(2)
    expect(htmlInlineScale(195, 390)).toBe(0.5)
    expect(PREVIEW_LIMITS.htmlInlineMaxHeight).toBeLessThan(PREVIEW_LIMITS.htmlMaxHeight)
    expect(PREVIEW_LIMITS.htmlDesignWidth).toBeGreaterThanOrEqual(320)
  })

  it('HTML iframe 外壳使用内联限高与缩放标记', async () => {
    vi.stubGlobal('IntersectionObserver', class {
      constructor(private cb: IntersectionObserverCallback) {}
      observe(el: Element) {
        this.cb([{ isIntersecting: true, target: el } as IntersectionObserverEntry], this as unknown as IntersectionObserver)
      }
      unobserve() {}
      disconnect() {}
    })
    await act(async () => {
      root.render(createElement(InlinePreviewBudgetProvider, null,
        createElement(InlinePreview, {
          kind: 'html',
          source: '<!doctype html><html><body><p>页面</p></body></html>',
          defaultNode: createElement('code', null, 'SOURCE'),
        }),
      ))
    })
    await act(async () => { await Promise.resolve() })
    await act(async () => { await Promise.resolve() })
    const shell = host.querySelector('[data-html-inline-shell]') as HTMLElement
    expect(shell).toBeTruthy()
    expect(shell.className).toMatch(/overflow-y-auto/)
    expect(Number.parseInt(shell.style.maxHeight || '0', 10)).toBe(PREVIEW_LIMITS.htmlInlineMaxHeight)
    expect(host.querySelector('iframe')).toBeTruthy()
  })
})

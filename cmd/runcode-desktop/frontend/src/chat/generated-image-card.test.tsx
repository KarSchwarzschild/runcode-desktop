// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ImageGenerationResult, ToolEvent } from '@/core/bridge'

const api = vi.hoisted(() => ({ openArtifact: vi.fn(), releaseArtifact: vi.fn() }))
vi.mock('@/core/bridge', async () => {
  const actual = await vi.importActual<typeof import('@/core/bridge')>('@/core/bridge')
  return {
    ...actual,
    ...api,
    errText: (e: unknown) => (e instanceof Error ? e.message : String(e)),
    errCode: (e: unknown) => { try { return JSON.parse((e as Error).message).code ?? '' } catch { return '' } },
  }
})

import { GeneratedImageCard } from './generated-image-card'

const ok: ImageGenerationResult = {
  version: 1, generationId: 'gen_1', status: 'succeeded', model: 'image-model', size: '1024x1024', accountingStatus: 'unverified',
  requestId: 'brg-1', images: [{ path: '生成图片/gen_1/image.png', mimeType: 'image/png', bytes: 2048, width: 1024, height: 1024 }],
}
const toolEvent = (data?: ImageGenerationResult, type: ToolEvent['type'] = 'completed'): ToolEvent => ({ type, toolName: 'image_generate', toolUseID: 'tu_1', data } as ToolEvent)

let host: HTMLDivElement, root: Root
beforeEach(() => {
  ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  api.openArtifact.mockReset().mockResolvedValue({
    reference: 'ref-1', sessionId: 'S', generation: 'g', path: ok.images![0].path, url: 'http://127.0.0.1:9/ref-1',
  })
  api.releaseArtifact.mockReset().mockResolvedValue(undefined)
  vi.stubGlobal('IntersectionObserver', class {
    constructor(private cb: IntersectionObserverCallback) {}
    observe(el: Element) { this.cb([{ isIntersecting: true, target: el } as IntersectionObserverEntry], this as unknown as IntersectionObserver) }
    disconnect() {}
    unobserve() {}
  })
  HTMLElement.prototype.scrollIntoView = vi.fn()
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.unstubAllGlobals()
})

async function render(tool: ToolEvent, sessionId = 'S') {
  await act(async () => root.render(<GeneratedImageCard tool={tool} sessionId={sessionId} />))
  await act(async () => { await Promise.resolve() })
  await act(async () => { await Promise.resolve() })
}

describe('GeneratedImageCard', () => {
  it('信息卡无缩略图；意图／元信息／操作可见；下方有预览舞台', async () => {
    await render(toolEvent(ok), 'S')
    const card = host.querySelector('[data-generated-image-card]')!
    expect(card.querySelector('img')).toBeNull()
    expect(card.querySelector('.ig-card-thumb')).toBeNull()
    expect(card.querySelector('.ig-card-shimmer')).toBeNull()
    expect(card.textContent).toContain('生成图片')
    expect(card.textContent).toContain('1024 × 1024')
    expect(card.textContent).toContain('PNG')
    expect(card.textContent).toContain('预览')
    expect(card.textContent).toContain('在文件夹中显示')
    expect(card.textContent).toContain('用系统程序打开')
    expect(card.textContent).not.toContain('brg-1')
    expect(host.querySelector('[data-generated-image-preview]')).toBeTruthy()
  })

  it('「预览」定位下方舞台，不调用 onOpenFile 侧栏', async () => {
    const onOpen = vi.fn()
    await act(async () => root.render(<GeneratedImageCard tool={toolEvent(ok)} sessionId="S" onOpenFile={onOpen} />))
    await act(async () => { await Promise.resolve() })
    const preview = Array.from(host.querySelectorAll('[data-generated-image-card] button')).find((b) => b.textContent === '预览') as HTMLButtonElement | undefined
    expect(preview).toBeTruthy()
    await act(async () => { preview!.click() })
    expect(onOpen).not.toHaveBeenCalled()
    expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalled()
  })

  it('失败结果用「未生成」；无下方舞台；不含内部字段', async () => {
    await render(toolEvent({
      ...ok, status: 'unknown', code: 'unknown_outcome', images: [],
      message: '生成结果无法确认，可能已生成或计费，请勿重复提交',
    }, 'failed'))
    expect(api.openArtifact).not.toHaveBeenCalled()
    expect(host.textContent).toContain('生成图片 未生成 image-model')
    expect(host.textContent).not.toContain('可能已生成')
    expect(host.querySelector('[data-generated-image-preview]')).toBeNull()
  })

  it('生成中显示意图与下方扫光，卡片内无动画', async () => {
    await render({ type: 'progress', toolName: 'image_generate', toolUseID: 'tu_1', message: '一双高跟鞋在地毯上' } as ToolEvent)
    const card = host.querySelector('[data-generated-image-card]')!
    expect(card.textContent).toContain('一双高跟鞋在地毯上')
    expect(card.querySelector('.ig-preview-shimmer')).toBeNull()
    expect(host.querySelector('.ig-preview-shimmer')).toBeTruthy()
    expect(host.textContent).not.toMatch(/\d+%/)
  })

  it('JPEG 与 PNG 使用同一套卡片结构', async () => {
    await render(toolEvent({
      ...ok, model: 'other-model',
      images: [{ path: '生成图片/gen_1/image.jpg', mimeType: 'image/jpeg', bytes: 3000, width: 1280, height: 720 }],
    }))
    expect(host.querySelector('[data-generated-image-card]')).toBeTruthy()
    expect(host.querySelector('[data-generated-image-preview]')).toBeTruthy()
    expect(host.textContent).toContain('JPEG')
  })

  it('非栅格 MIME 不渲染新卡片', async () => {
    await render(toolEvent({
      ...ok, images: [{ path: '生成图片/g/x.svg', mimeType: 'image/svg+xml', bytes: 10, width: 1, height: 1 }],
    }))
    expect(host.querySelector('[data-generated-image-card]')).toBeNull()
  })
})

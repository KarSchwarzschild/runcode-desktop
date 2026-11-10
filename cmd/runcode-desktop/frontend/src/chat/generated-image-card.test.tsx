// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ImageGenerationResult, ToolEvent } from '@/core/bridge'

const api = vi.hoisted(() => ({ openArtifact: vi.fn(), releaseArtifact: vi.fn() }))
vi.mock('@/core/bridge', () => ({
  ...api,
  errText: (e: unknown) => (e instanceof Error ? e.message : String(e)),
  errCode: (e: unknown) => { try { return JSON.parse((e as Error).message).code ?? '' } catch { return '' } },
}))

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
  api.openArtifact.mockReset().mockResolvedValue({ reference: 'ref-1', sessionId: 'S', generation: 'g', path: ok.images![0].path, url: 'http://127.0.0.1:9/ref-1' })
  api.releaseArtifact.mockReset().mockResolvedValue(undefined)
  // Cards request a preview reference only once visible; report visible at once.
  vi.stubGlobal('IntersectionObserver', class {
    constructor(private cb: IntersectionObserverCallback) {}
    observe(el: Element) { this.cb([{ isIntersecting: true, target: el } as IntersectionObserverEntry], this as unknown as IntersectionObserver) }
    disconnect() {}
    unobserve() {}
  })
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.unstubAllGlobals()
})

async function render(tool: ToolEvent, sessionId = 'S', onOpenFile = vi.fn()) {
  await act(async () => root.render(<GeneratedImageCard tool={tool} sessionId={sessionId} onOpenFile={onOpenFile} />))
  await act(async () => { await Promise.resolve() })
  return onOpenFile
}

describe('GeneratedImageCard', () => {
  it('成功结果直接显示缩略图，不依赖回答正文提及路径，并用来源会话申请预览', async () => {
    const onOpen = await render(toolEvent(ok), 'S')
    expect(api.openArtifact).toHaveBeenCalledWith('S', '生成图片/gen_1/image.png')
    const img = host.querySelector('img')!
    expect(img.getAttribute('src')).toBe('http://127.0.0.1:9/ref-1')
    expect(host.textContent).toContain('image-model')
    expect(host.textContent).toContain('1024 × 1024')
    expect(host.textContent).toContain('计费待核验')
    await act(async () => img.closest('button')!.click())
    expect(onOpen).toHaveBeenCalledWith('生成图片/gen_1/image.png')
  })

  it('卸载或更换引用时释放旧引用', async () => {
    await render(toolEvent(ok), 'S')
    await act(async () => root.render(<GeneratedImageCard tool={toolEvent({ ...ok, generationId: 'gen_2', images: [{ ...ok.images![0], path: '生成图片/gen_2/image.png' }] })} sessionId="S" />))
    await act(async () => { await Promise.resolve() })
    expect(api.releaseArtifact).toHaveBeenCalledWith('ref-1')
    await act(async () => root.unmount())
    root = createRoot(host)
    expect(api.releaseArtifact).toHaveBeenCalledTimes(2)
  })

  it('文件已删除时显示文件不存在，不自动重新生成', async () => {
    api.openArtifact.mockRejectedValue(new Error(JSON.stringify({ code: 'not_found', message: 'gone' })))
    await render(toolEvent(ok))
    expect(host.textContent).toContain('文件不存在')
    expect(host.querySelector('img')).toBeNull()
    expect(api.openArtifact).toHaveBeenCalledTimes(1)
    expect([...host.querySelectorAll('button')].some((b) => /重试|重新生成/.test(b.textContent ?? ''))).toBe(false)
  })

  it('结果未知只显示状态和提示，不申请预览、不给重试按钮', async () => {
    await render(toolEvent({ ...ok, status: 'unknown', code: 'unknown_outcome', images: [], message: '生成结果无法确认，可能已生成或计费，请勿重复提交' }, 'failed'))
    expect(api.openArtifact).not.toHaveBeenCalled()
    expect(host.textContent).toContain('结果未知')
    expect(host.textContent).toContain('可能已生成')
    expect(host.textContent).toContain('计费待核验')
    expect([...host.querySelectorAll('button')].some((b) => /重试|重新生成/.test(b.textContent ?? ''))).toBe(false)
    expect(host.textContent).not.toMatch(/免费|不收费/)
  })

  it('进行中只显示真实阶段，不显示虚假百分比', async () => {
    await render({ type: 'progress', toolName: 'image_generate', toolUseID: 'tu_1', message: '正在生成图片' } as ToolEvent)
    expect(host.textContent).toContain('正在生成图片')
    expect(host.textContent).not.toMatch(/\d+%/)
  })

  it('没有来源会话时不猜测会话', async () => {
    await render(toolEvent(ok), '')
    expect(api.openArtifact).not.toHaveBeenCalled()
    expect(host.textContent).toContain('无法确定图片所属会话')
  })
})

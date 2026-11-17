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
  it('成功卡片只保留元信息与操作，不含文件名/路径/内部字段', async () => {
    const onOpen = await render(toolEvent(ok), 'S')
    expect(api.openArtifact).toHaveBeenCalledWith('S', '生成图片/gen_1/image.png')
    expect(host.querySelector('img')!.getAttribute('src')).toBe('http://127.0.0.1:9/ref-1')
    expect(host.textContent).toContain('1024 × 1024')
    expect(host.textContent).toContain('PNG')
    expect(host.textContent).not.toContain('image.png')
    expect(host.textContent).not.toContain('生成图片/gen_1')
    expect(host.textContent).not.toContain('brg-1')
    expect(host.textContent).not.toContain('计费待核验')
    expect(host.textContent).not.toMatch(/text-red/)
    // 「预览」仍走 onOpenFile；缩略图点击打开标注画板。
    const preview = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '预览')
    await act(async () => { preview!.click() })
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

  it('读不到文件时用「文件不存在」文案，不展示错误原文', async () => {
    api.openArtifact.mockRejectedValue(new Error(JSON.stringify({ code: 'not_found', message: 'gone-secret' })))
    await render(toolEvent(ok))
    expect(host.textContent).toContain('生成图片 文件不存在 image-model')
    expect(host.textContent).not.toContain('gone-secret')
    expect(host.querySelector('img')).toBeNull()
    expect(host.innerHTML).not.toContain('text-red')
  })

  it('失败结果用「未生成」中性文案，不含 Message/Code/SubmissionStatus', async () => {
    await render(toolEvent({
      ...ok, status: 'unknown', code: 'unknown_outcome', images: [],
      message: '生成结果无法确认，可能已生成或计费，请勿重复提交',
    }, 'failed'))
    expect(api.openArtifact).not.toHaveBeenCalled()
    expect(host.textContent).toContain('生成图片 未生成 image-model')
    expect(host.textContent).not.toContain('可能已生成')
    expect(host.textContent).not.toContain('unknown_outcome')
    expect(host.textContent).not.toContain('brg-1')
    expect(host.innerHTML).not.toContain('text-red')
  })

  it('明确未提交也归为未生成，不展示内部提交状态词', async () => {
    await render(toolEvent({
      ...ok, status: 'failed', code: 'auth', submissionStatus: 'not_submitted', images: [], message: '调用身份信息缺失',
    }, 'failed'))
    expect(host.textContent).toContain('生成图片 未生成 image-model')
    expect(host.textContent).not.toContain('未提交生成请求')
    expect(host.textContent).not.toContain('调用身份信息缺失')
  })

  it('进行中只显示阶段文案，不显示百分比', async () => {
    await render({ type: 'progress', toolName: 'image_generate', toolUseID: 'tu_1', message: '正在生成图片' } as ToolEvent)
    expect(host.textContent).toContain('正在生成图片')
    expect(host.textContent).not.toMatch(/\d+%/)
  })

  it('生成中显示 tool.message 意图行；完成后显示 result.displayIntent；缺失则不画意图行', async () => {
    await render({ type: 'progress', toolName: 'image_generate', toolUseID: 'tu_1', message: '一双高跟鞋在地毯上' } as ToolEvent)
    expect(host.textContent).toContain('一双高跟鞋在地毯上')

    await render(toolEvent({ ...ok, displayIntent: '完成后的意图说明' }))
    expect(host.textContent).toContain('完成后的意图说明')

    await render(toolEvent({ ...ok, displayIntent: undefined }))
    expect(host.textContent).not.toContain('完成后的意图说明')
    expect(host.textContent).not.toContain('一双高跟鞋在地毯上')
  })

  it('running 时有方形 shimmer 占位与连续动画 class', async () => {
    await render({ type: 'progress', toolName: 'image_generate', toolUseID: 'tu_1', message: '正在生成图片' } as ToolEvent)
    const shimmer = host.querySelector('.ig-card-shimmer')
    expect(shimmer).toBeTruthy()
    expect(shimmer!.classList.contains('ig-card-shimmer-animate')).toBe(true)
  })

  it('从 running 变成 ready 且有图片地址时播放百叶窗，animationend 后移除', async () => {
    await render({ type: 'progress', toolName: 'image_generate', toolUseID: 'tu_1', message: '正在生成图片' } as ToolEvent)
    expect(host.querySelector('.ig-card-shimmer')).toBeTruthy()

    await act(async () => root.render(<GeneratedImageCard tool={toolEvent(ok)} sessionId="S" />))
    await act(async () => { await Promise.resolve() })
    await act(async () => { await Promise.resolve() })

    const reveal = host.querySelector('.ig-card-reveal')
    expect(reveal).toBeTruthy()
    expect(reveal!.classList.contains('ig-card-reveal-animate')).toBe(true)

    // 动画结束挂在最后一条条带上，避免前几条提前清遮罩。
    const lastStrip = reveal!.querySelector('span:last-child')
    expect(lastStrip).toBeTruthy()
    await act(async () => {
      lastStrip!.dispatchEvent(new Event('animationend', { bubbles: true }))
    })
    expect(host.querySelector('.ig-card-reveal')).toBeNull()
  })

  it('直接以 ready 挂载时没有百叶窗 class', async () => {
    await render(toolEvent(ok))
    expect(host.querySelector('.ig-card-reveal')).toBeNull()
    expect(host.querySelector('.ig-card-shimmer')).toBeNull()
  })

  it('没有图片地址时不开始百叶窗动画', async () => {
    await render({ type: 'progress', toolName: 'image_generate', toolUseID: 'tu_1', message: '生成中' } as ToolEvent)
    // 成功结果但缩略图尚未就绪（openArtifact 挂起）：不应出现 reveal。
    let resolveOpen: (v: unknown) => void = () => {}
    api.openArtifact.mockReset().mockImplementation(() => new Promise((resolve) => { resolveOpen = resolve }))
    await act(async () => root.render(<GeneratedImageCard tool={toolEvent(ok)} sessionId="S" />))
    await act(async () => { await Promise.resolve() })
    expect(host.querySelector('.ig-card-reveal')).toBeNull()
    await act(async () => {
      resolveOpen({ reference: 'ref-late', sessionId: 'S', generation: 'g', path: ok.images![0].path, url: 'http://127.0.0.1:9/late' })
      await Promise.resolve()
    })
    await act(async () => { await Promise.resolve() })
    expect(host.querySelector('.ig-card-reveal')).toBeTruthy()
  })

  it('prefers-reduced-motion 时不挂连续动画 class', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: String(query).includes('prefers-reduced-motion'),
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }))
    await render({ type: 'progress', toolName: 'image_generate', toolUseID: 'tu_1', message: '正在生成图片' } as ToolEvent)
    const shimmer = host.querySelector('.ig-card-shimmer')
    expect(shimmer).toBeTruthy()
    expect(shimmer!.classList.contains('ig-card-shimmer-animate')).toBe(false)

    await act(async () => root.render(<GeneratedImageCard tool={toolEvent(ok)} sessionId="S" />))
    await act(async () => { await Promise.resolve() })
    await act(async () => { await Promise.resolve() })
    expect(host.querySelector('.ig-card-reveal')).toBeNull()
  })
})

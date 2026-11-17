// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  saveImageGenerationEdit: vi.fn(),
}))
vi.mock('@/core/bridge', () => ({
  ...api,
  errText: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}))

import { ImageEditCanvas } from './image-edit-canvas'
import { IMAGE_EDIT_MAX_BYTES } from './image-edit-ops'

let host: HTMLDivElement, root: Root
beforeEach(() => {
  ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  api.saveImageGenerationEdit.mockReset().mockResolvedValue({
    path: '生成图片/gen_1/edited-20261117-120000001.png', mimeType: 'image/png', bytes: 100, width: 64, height: 64,
  })
  // jsdom 无真实图像度量：桩出合成路径用的 toDataURL / Image / 布局盒。
  HTMLCanvasElement.prototype.toDataURL = vi.fn(() => 'data:image/png;base64,aaaa')
  HTMLCanvasElement.prototype.getContext = vi.fn(() => ({
    drawImage: vi.fn(),
    clearRect: vi.fn(),
    beginPath: vi.fn(),
    moveTo: vi.fn(),
    lineTo: vi.fn(),
    stroke: vi.fn(),
    fillText: vi.fn(),
    set lineWidth(_v: number) {},
    set strokeStyle(_v: string) {},
    set fillStyle(_v: string) {},
    set font(_v: string) {},
    set lineCap(_v: string) {},
    set lineJoin(_v: string) {},
  })) as unknown as typeof HTMLCanvasElement.prototype.getContext
  vi.stubGlobal('Image', class {
    onload: null | (() => void) = null
    onerror: null | (() => void) = null
    naturalWidth = 100
    naturalHeight = 100
    set src(_u: string) { queueMicrotask(() => this.onload?.()) }
  })
  Element.prototype.getBoundingClientRect = vi.fn(() => ({
    x: 0, y: 0, top: 0, left: 0, bottom: 200, right: 200, width: 200, height: 200, toJSON: () => ({}),
  }))
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})

describe('ImageEditCanvas', () => {
  it('Escape 对未保存面板提示；确认后关闭', async () => {
    const onClose = vi.fn()
    await act(async () => root.render(
      <ImageEditCanvas
        sessionId="S"
        sourcePath="生成图片/gen_1/image.png"
        imageUrl="http://127.0.0.1:9/img"
        naturalWidth={100}
        naturalHeight={100}
        onClose={onClose}
        onSaved={vi.fn()}
      />,
    ))
    // 先画一笔制造 dirty
    const stage = host.querySelector('[data-edit-stage]') as HTMLElement
    expect(stage).toBeTruthy()
    await act(async () => {
      stage.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, clientX: 10, clientY: 10, pointerId: 1 }))
      stage.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, clientX: 40, clientY: 40, pointerId: 1 }))
      stage.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, clientX: 40, clientY: 40, pointerId: 1 }))
    })
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(host.textContent).toContain('尚未保存')
    expect(onClose).not.toHaveBeenCalled()
    const confirm = Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.includes('放弃'))
    expect(confirm).toBeTruthy()
    await act(async () => { confirm!.click() })
    expect(onClose).toHaveBeenCalled()
  })

  it('保存请求带源路径；成功回调 edited 副本路径', async () => {
    const onSaved = vi.fn()
    // 让 toDataURL 返回足够短的假 PNG base64（宿主侧由 mock 接住）
    HTMLCanvasElement.prototype.toDataURL = vi.fn(() => 'data:image/png;base64,' + 'A'.repeat(32))
    await act(async () => root.render(
      <ImageEditCanvas
        sessionId="S"
        sourcePath="生成图片/gen_1/image.png"
        imageUrl="http://127.0.0.1:9/img"
        naturalWidth={100}
        naturalHeight={100}
        onClose={vi.fn()}
        onSaved={onSaved}
      />,
    ))
    const save = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '保存副本')
    expect(save).toBeTruthy()
    await act(async () => { save!.click() })
    await act(async () => { await Promise.resolve() })
    expect(api.saveImageGenerationEdit).toHaveBeenCalledWith(expect.objectContaining({
      sessionId: 'S',
      sourcePath: '生成图片/gen_1/image.png',
    }))
    expect(onSaved).toHaveBeenCalledWith(expect.objectContaining({
      path: '生成图片/gen_1/edited-20261117-120000001.png',
    }))
  })

  it('合成结果超过字节上限时提示且不发送', async () => {
    HTMLCanvasElement.prototype.toDataURL = vi.fn(() => 'data:image/png;base64,' + 'A'.repeat(IMAGE_EDIT_MAX_BYTES + 64))
    await act(async () => root.render(
      <ImageEditCanvas
        sessionId="S"
        sourcePath="生成图片/gen_1/image.png"
        imageUrl="http://127.0.0.1:9/img"
        naturalWidth={100}
        naturalHeight={100}
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    ))
    const save = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === '保存副本')
    await act(async () => { save!.click() })
    await act(async () => { await Promise.resolve() })
    expect(api.saveImageGenerationEdit).not.toHaveBeenCalled()
    expect(host.textContent).toMatch(/超过|太大|上限/)
  })
})

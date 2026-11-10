import { describe, expect, it, vi } from 'vitest'
import type { SessionImageGenerationView, SetSessionImageGenerationRequest } from '@/core/bridge'
import { ImageGenerationController } from './use-image-generation'

const ref = { kind: 'platform', name: 'image-model', bridge: 'https://bridge.example', tenantId: 't1' }

function fakeBackend() {
  const store: Record<string, SessionImageGenerationView> = {}
  const view = (id: string): SessionImageGenerationView => store[id] ?? { sessionId: id, enabled: false, revision: 0, available: true, model: ref, size: '1024x1024' }
  const get = vi.fn(async (id: string) => view(id))
  const set = vi.fn(async (req: SetSessionImageGenerationRequest) => {
    const cur = view(req.sessionId)
    if (cur.enabled === req.enabled) return cur
    if (cur.revision !== req.expectedRevision) throw new Error('图片生成开关已变化，请刷新后重试')
    store[req.sessionId] = { ...cur, enabled: req.enabled, revision: cur.revision + 1 }
    return store[req.sessionId]
  })
  return { get, set, store }
}

describe('ImageGenerationController', () => {
  it('A 会话开启不影响 B，切换会话后重新读取', async () => {
    const backend = fakeBackend()
    const c = new ImageGenerationController(backend)
    await c.activate('A')
    await c.setEnabled(true)
    expect(c.snapshot().view?.enabled).toBe(true)
    await c.activate('B')
    expect(c.snapshot().view).toMatchObject({ sessionId: 'B', enabled: false })
    expect(backend.set).toHaveBeenCalledTimes(1)
  })

  it('切走后迟到的旧会话结果被丢弃', async () => {
    let release!: (v: SessionImageGenerationView) => void
    const get = vi.fn((id: string) => id === 'A'
      ? new Promise<SessionImageGenerationView>((r) => { release = r })
      : Promise.resolve({ sessionId: id, enabled: false, revision: 0, available: false, reason: '尚未选择生图模型' }))
    const c = new ImageGenerationController({ get, set: vi.fn() })
    const first = c.activate('A')
    await c.activate('B')
    release({ sessionId: 'A', enabled: true, revision: 3, available: true })
    await first
    expect(c.snapshot().view).toMatchObject({ sessionId: 'B', enabled: false })
  })

  it('新会话草稿：会话创建后先保存成功才放行首条消息', async () => {
    const backend = fakeBackend()
    const c = new ImageGenerationController(backend)
    await c.activate('')
    await c.setEnabled(true)
    expect(backend.set).not.toHaveBeenCalled()
    expect(c.snapshot().draft).toBe(true)
    await c.activate('NEW')
    await expect(c.ensureCommitted()).resolves.toBe(true)
    expect(backend.set).toHaveBeenCalledWith({ sessionId: 'NEW', enabled: true, expectedRevision: 0 })
    expect(c.snapshot().draft).toBeNull()
  })

  it('草稿保存失败时不放行发送、保留草稿并给出原因', async () => {
    const backend = fakeBackend()
    backend.set.mockRejectedValueOnce(new Error('图片生成记录无法读取或保存'))
    const c = new ImageGenerationController(backend)
    await c.activate('')
    await c.setEnabled(true)
    await c.activate('NEW')
    await expect(c.ensureCommitted()).resolves.toBe(false)
    expect(c.snapshot().draft).toBe(true)
    expect(c.snapshot().error).toContain('无法读取或保存')
  })

  it('回合进行中禁止切换', async () => {
    const backend = fakeBackend()
    const c = new ImageGenerationController(backend)
    await c.activate('A')
    await c.setEnabled(true, { busy: true })
    expect(backend.set).not.toHaveBeenCalled()
    expect(c.snapshot().view?.enabled).toBe(false)
    expect(c.snapshot().error).toContain('回合进行中')
  })

  it('保存失败保留已确认值', async () => {
    const backend = fakeBackend()
    backend.set.mockRejectedValueOnce(new Error('写盘失败'))
    const c = new ImageGenerationController(backend)
    await c.activate('A')
    await c.setEnabled(true)
    expect(c.snapshot().view?.enabled).toBe(false)
    expect(c.snapshot().error).toBe('写盘失败')
  })

  it('没有草稿时无需保存即可发送', async () => {
    const backend = fakeBackend()
    const c = new ImageGenerationController(backend)
    await c.activate('A')
    await expect(c.ensureCommitted()).resolves.toBe(true)
    expect(backend.set).not.toHaveBeenCalled()
  })
})

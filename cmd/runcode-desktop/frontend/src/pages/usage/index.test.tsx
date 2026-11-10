// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PassportStatus } from '@/core/bridge'
import { UsagePage } from './index'
import { pickInitialTenant } from './tenant'
import { fixtureOverview, fixtureRequests } from './fixtures'

const native = vi.hoisted(() => ({
  overview: vi.fn(),
  requests: vi.fn(),
  dimensions: vi.fn(),
  tenants: vi.fn(),
  active: vi.fn(),
  setActive: vi.fn(),
}))
vi.mock('@/core/bridge', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/core/bridge')>()),
  getUsageOverview: native.overview,
  getUsageRequests: native.requests,
  getUsageDimensions: native.dimensions,
  passportTenants: native.tenants,
  activeTenant: native.active,
  setActiveTenant: native.setActive,
}))

let host: HTMLDivElement
let root: Root
const user: PassportStatus = { loggedIn: true, userId: 'u-1', name: '张三' }

async function render(passport: PassportStatus | null, onOpenSettings = () => {}) {
  await act(async () => root.render(<UsagePage passport={passport} onOpenSettings={onOpenSettings} />))
}

beforeEach(() => {
  ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  localStorage.clear()
  for (const fn of Object.values(native)) fn.mockReset()
  native.overview.mockResolvedValue(fixtureOverview())
  native.requests.mockResolvedValue(fixtureRequests())
  native.dimensions.mockResolvedValue({ items: [], groupBy: 'model', page: 1, pageSize: 50, totalCount: '0', hasMore: false, effectiveRange: fixtureOverview().effectiveRange, queryKey: 'qk' })
  native.tenants.mockResolvedValue([{ id: 'tenant-a', name: '租户甲' }])
  native.active.mockResolvedValue('tenant-a')
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})

describe('pickInitialTenant', () => {
  const list = [{ id: 'a', name: 'A' }, { id: 'b', name: 'B' }]
  it('活动租户优先，其次令牌租户，唯一租户兜底；多租户不确定时要求选择', () => {
    expect(pickInitialTenant(list, 'b', 'a')).toBe('b')
    expect(pickInitialTenant(list, 'zzz', 'a')).toBe('a')
    expect(pickInitialTenant([list[0]], '', '')).toBe('a')
    expect(pickInitialTenant(list, '', '')).toBe('')
    expect(pickInitialTenant([], 'a', 'a')).toBe('')
  })
})

describe('UsagePage', () => {
  it('未登录：显示登录说明与前往账号设置，不发任何统计请求', async () => {
    const onOpenSettings = vi.fn()
    await render({ loggedIn: false }, onOpenSettings)
    expect(host.textContent).toContain('登录通行证后')
    const go = [...host.querySelectorAll('button')].find((b) => b.textContent === '前往账号设置')!
    await act(async () => go.click())
    expect(onOpenSettings).toHaveBeenCalled()
    expect(native.overview).not.toHaveBeenCalled()
    expect(native.tenants).not.toHaveBeenCalled()
  })

  it('已登录：范围条写明账号、租户、时区与口径限制；不修改聊天租户', async () => {
    await render(user)
    const t = host.textContent ?? ''
    expect(t).toContain('张三')
    expect(t).toContain('租户甲')
    expect(t).toContain('Asia/Shanghai')
    expect(t).toContain('暂不含嵌套子调用')
    expect(t).toContain('1.1万')
    expect(native.overview).toHaveBeenCalledWith(expect.objectContaining({ tenantId: 'tenant-a', range: 'today' }))
    expect(native.setActive).not.toHaveBeenCalled()
    expect(t).not.toMatch(/成本|费用|价格|¥/)
  })

  it('多租户且无法确定时先要求选择，不以空租户查询', async () => {
    native.tenants.mockResolvedValue([{ id: 'a', name: '甲' }, { id: 'b', name: '乙' }])
    native.active.mockResolvedValue('')
    await render(user)
    expect(host.textContent).toContain('请选择要统计的租户')
    expect(native.overview).not.toHaveBeenCalled()
  })

  it.each([
    ['usage_forbidden', '无权查看'],
    ['usage_unsupported', '不代表零用量'],
    ['usage_timeout', '缩小时间范围'],
    ['usage_unavailable', '暂不可用'],
  ])('失败 %s 有各自的说明与重试，不显示成零用量', async (code, phrase) => {
    native.overview.mockRejectedValue(new Error(JSON.stringify({ code, message: '服务端说明', details: { requestId: 'req-9' } })))
    await render(user)
    const t = host.textContent ?? ''
    expect(t).toContain(phrase)
    expect(t).toContain('req-9')
    expect(t).not.toContain('暂无记录')
    expect([...host.querySelectorAll('button')].some((b) => b.textContent === '重试')).toBe(true)
  })
})

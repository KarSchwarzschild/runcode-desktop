import { describe, expect, it } from 'vitest'
import { networkRouteLabel } from './network-route'

describe('network route display', () => {
  it('explains automatic recovery and fixed routes', () => {
    expect(networkRouteLabel(undefined)).toContain('不绕过固定配置')
    expect(networkRouteLabel('configured')).toContain('不自动切换')
    expect(networkRouteLabel('http://127.0.0.1:7890')).toBe('http://127.0.0.1:7890（仅本次请求）')
  })
  it('does not echo credentials or malformed inputs', () => {
    for (const raw of ['http://user:secret@127.0.0.1:80', 'http://127.0.0.1:80?token=secret', 'secret']) {
      expect(networkRouteLabel(raw)).toBe('不支持的代理配置')
    }
  })
})

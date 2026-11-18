// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const bridge = vi.hoisted(() => ({
  pickWorkspaceFolder: vi.fn(),
  openSession: vi.fn(),
  resumeSession: vi.fn(),
}))

vi.mock('@/core/bridge', () => ({
  pickWorkspaceFolder: bridge.pickWorkspaceFolder,
  openSession: bridge.openSession,
  resumeSession: bridge.resumeSession,
  errText: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}))

import { WorkspacePicker } from './workspace-picker'

let root: Root
let host: HTMLDivElement

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  bridge.pickWorkspaceFolder.mockReset()
  bridge.openSession.mockReset()
  bridge.resumeSession.mockReset()
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})

function Harness({ recent = ['D:\\资料', 'C:\\Work Files'] }: { recent?: string[] }) {
  const [value, setValue] = useState('D:\\资料')
  return (
    <>
      <WorkspacePicker value={value} recent={recent} onChange={setValue} />
      <div data-value={value} />
    </>
  )
}

describe('workspace picker', () => {
  it('filters history and keeps the previous path when the native dialog is cancelled', async () => {
    await act(async () => { root.render(<Harness />) })
    const search = host.querySelector('input') as HTMLInputElement
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search, 'work')
      search.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(host.textContent).toContain('C:\\Work Files')
    expect(host.textContent).not.toContain('D:\\资料')
    expect(host.textContent).toContain('浏览其他目录...')

    bridge.pickWorkspaceFolder.mockResolvedValue('')
    const browse = [...host.querySelectorAll('button')].find((b) => b.textContent?.includes('浏览其他目录...'))
    await act(async () => { browse!.click() })
    expect(host.querySelector('[data-value]')?.getAttribute('data-value')).toBe('D:\\资料')
    expect(bridge.openSession).not.toHaveBeenCalled()
    expect(bridge.resumeSession).not.toHaveBeenCalled()
  })

  it('reports a chosen directory only through onChange', async () => {
    await act(async () => { root.render(<Harness />) })
    bridge.pickWorkspaceFolder.mockResolvedValue('D:\\新 目录\\项目')
    const browse = [...host.querySelectorAll('button')].find((b) => b.textContent?.includes('浏览其他目录...'))
    await act(async () => { browse!.click() })
    expect(host.querySelector('[data-value]')?.getAttribute('data-value')).toBe('D:\\新 目录\\项目')
    expect(bridge.openSession).not.toHaveBeenCalled()
    expect(bridge.resumeSession).not.toHaveBeenCalled()
  })

  it('shows a banner when the native picker fails', async () => {
    await act(async () => { root.render(<Harness />) })
    bridge.pickWorkspaceFolder.mockRejectedValue(new Error('无法打开目录选择器'))
    const browse = [...host.querySelectorAll('button')].find((b) => b.textContent?.includes('浏览其他目录...'))
    await act(async () => { browse!.click() })
    expect(host.textContent).toContain('无法打开目录选择器')
    expect(host.querySelector('[data-value]')?.getAttribute('data-value')).toBe('D:\\资料')
  })
})

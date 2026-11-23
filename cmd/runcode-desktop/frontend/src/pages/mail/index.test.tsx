// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  getMailSettings: vi.fn(),
  listMailDrafts: vi.fn(),
  listMailContacts: vi.fn(),
  listMailGroups: vi.fn(),
  listMailOutbox: vi.fn(),
  getMailDraft: vi.fn(),
  createMailDraft: vi.fn(),
  saveMailDraft: vi.fn(),
  saveMailContact: vi.fn(),
  deleteMailContact: vi.fn(),
  saveMailGroup: vi.fn(),
  previewMailContactImport: vi.fn(),
  commitMailContactImport: vi.fn(),
}))

vi.mock('@/core/bridge', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/core/bridge')>()
  return {
    ...actual,
    ...api,
    errText: (e: unknown) => (e instanceof Error ? e.message : String(e)),
  }
})

import { MailPage } from './index'

let host: HTMLDivElement
let root: Root
const onOpenSettings = vi.fn()

const draftA = {
  id: 'd1',
  revision: 1,
  status: 'draft',
  accountId: '',
  subject: '主题甲',
  bodyMarkdown: '正文 **甲**',
  to: [{ address: 'a@example.com' }],
  cc: [],
  bcc: [],
  sendMode: 'together',
  dispatchMode: 'immediate',
  updatedAtUtc: '2026-01-01T00:00:00Z',
  createdAtUtc: '2026-01-01T00:00:00Z',
}

beforeEach(() => {
  ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  for (const fn of Object.values(api)) fn.mockReset()
  onOpenSettings.mockReset()
  api.getMailSettings.mockResolvedValue({ secretStorageOk: true })
  api.listMailDrafts.mockResolvedValue([draftA])
  api.listMailContacts.mockResolvedValue([])
  api.listMailGroups.mockResolvedValue([])
  api.listMailOutbox.mockResolvedValue([])
  api.getMailDraft.mockResolvedValue(draftA)
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
})

async function flush() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function mount() {
  await act(async () => { root.render(<MailPage onOpenSettings={onOpenSettings} />) })
  await flush()
}

async function click(label: string) {
  const btn = Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.includes(label))
  if (!btn) throw new Error(`button ${label} missing in ${host.textContent}`)
  await act(async () => { (btn as HTMLButtonElement).click() })
  await flush()
}

describe('MailPage', () => {
  it('shows no-account guidance without a password form', async () => {
    await mount()
    expect(host.querySelector('[data-testid="mail-no-account"]')).toBeTruthy()
    expect(host.textContent).toContain('去设置接入邮箱')
    expect(host.querySelector('input[type="password"]')).toBeNull()
    await click('前往邮箱设置')
    expect(onOpenSettings).toHaveBeenCalled()
  })

  it('renders tabs including drafts contacts groups and outbox', async () => {
    await mount()
    expect(host.textContent).toContain('草稿（1）')
    expect(host.textContent).toContain('通信录')
    expect(host.textContent).toContain('分组')
    expect(host.textContent).toContain('发件箱（0）')
  })

  it('loads draft and keeps local edits when save fails', async () => {
    api.saveMailDraft.mockRejectedValue(new Error('冲突：草稿已被其它位置更新'))
    await mount()
    await click('主题甲')
    const subject = host.querySelector('[data-testid="mail-draft-subject"]') as HTMLInputElement
    const body = host.querySelector('[data-testid="mail-draft-body"]') as HTMLTextAreaElement
    expect(subject.value).toBe('主题甲')
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
      setter.call(subject, '本地新主题')
      subject.dispatchEvent(new InputEvent('input', { bubbles: true }))
      const tsetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!
      tsetter.call(body, '本地新正文')
      body.dispatchEvent(new InputEvent('input', { bubbles: true }))
    })
    await click('保存草稿')
    expect(api.saveMailDraft).toHaveBeenCalled()
    expect((host.querySelector('[data-testid="mail-draft-subject"]') as HTMLInputElement).value).toBe('本地新主题')
    expect((host.querySelector('[data-testid="mail-draft-body"]') as HTMLTextAreaElement).value).toBe('本地新正文')
    expect(host.textContent).toMatch(/保存失败|冲突/)
  })

  it('preview uses the current user-edited markdown', async () => {
    await mount()
    await click('主题甲')
    const body = host.querySelector('[data-testid="mail-draft-body"]') as HTMLTextAreaElement
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!
      setter.call(body, '预览用 **修订**')
      body.dispatchEvent(new InputEvent('input', { bubbles: true }))
    })
    await click('预览')
    const preview = host.querySelector('[data-testid="mail-preview"]')
    expect(preview?.textContent).toContain('修订')
    expect(preview?.textContent).not.toContain('正文')
  })

  it('outbox tab lists delivery rows without sending', async () => {
    api.listMailOutbox.mockResolvedValue([{
      itemId: 'item1',
      submissionId: 'sub1',
      draftId: 'd1',
      draftRevision: 1,
      subject: '请假申请',
      recipient: 'a@example.com',
      status: 'accepted',
      createdAtUtc: '2026-01-01T00:00:00Z',
      attemptCount: 1,
    }])
    await mount()
    await click('发件箱（1）')
    expect(host.querySelector('[data-testid="mail-outbox"]')).toBeTruthy()
    expect(host.textContent).toContain('请假申请')
    expect(host.textContent).toContain('已送达')
    expect(api.saveMailDraft).not.toHaveBeenCalled()
    expect(api.createMailDraft).not.toHaveBeenCalled()
  })
})

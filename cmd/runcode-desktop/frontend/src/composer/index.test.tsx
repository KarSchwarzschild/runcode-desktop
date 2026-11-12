// @vitest-environment jsdom
import { act, createRef } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CONTROLLED_TEXT_INSERTION_COMMAND, getNearestEditorFromDOMNode } from 'lexical'
import type { KnowledgeBaseSummary, SessionInfo } from '@/core/bridge'
import type { Attachment } from './paste'
import type { SessionKnowledge } from '@/session/use-knowledge'
import { Composer } from './index'
import type { ComposerEditorHandle } from './inline-editor'

const directoryCommand = vi.hoisted(() => vi.fn())
const imageApi = vi.hoisted(() => ({ get: vi.fn(), set: vi.fn() }))

vi.mock('@/core/bridge', () => ({
  listAgents: async () => ({ agents: [] }),
  listSkills: async () => ({ skills: [] }),
  listCustomModels: async () => [],
  sessionModels: async () => [],
  pickImageAttachment: async () => '',
  savePastedFile: async () => '',
  errText: (e: unknown) => (e instanceof Error ? e.message : String(e)),
  openKnowledgeDirectory: directoryCommand,
  getSessionImageGeneration: imageApi.get,
  setSessionImageGeneration: imageApi.set,
  onEvent: () => () => {},
  Events: { PassportChanged: 'passport:changed' },
}))

const imageRef = { kind: 'platform', name: 'image-model', bridge: 'https://bridge.example', tenantId: 't1' }
function imageView(sessionId: string, patch: Record<string, unknown> = {}) {
  return { sessionId, enabled: false, revision: 0, available: true, model: imageRef, size: '1024x1024', ...patch }
}

const kb: KnowledgeBaseSummary = { id: 'private-library-id', title: '测试知识库标题', description: '测试简介', path: '测试目录/产品', source: 'demo', documentCount: 2, documentCountKnown: true }
const noop = () => {}
let host: HTMLDivElement, root: Root

beforeEach(() => {
  directoryCommand.mockReset()
  imageApi.get.mockReset().mockImplementation(async (id: string) => imageView(id))
  imageApi.set.mockReset().mockImplementation(async (req: { sessionId: string; enabled: boolean; expectedRevision: number }) => imageView(req.sessionId, { enabled: req.enabled, revision: req.expectedRevision + 1 }))
  ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.stubGlobal('IntersectionObserver', class { observe() {} unobserve() {} disconnect() {} })
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: noop })
  Range.prototype.getBoundingClientRect = () => new DOMRect()
  Range.prototype.getClientRects = () => [] as unknown as DOMRectList
  Selection.prototype.modify = function (_alter, direction) {
    const walker = document.createTreeWalker(host.querySelector('[role="textbox"]')!, NodeFilter.SHOW_TEXT)
    const texts: Text[] = []
    while (walker.nextNode()) texts.push(walker.currentNode as Text)
    let index = texts.indexOf(this.focusNode as Text), offset = this.focusOffset
    if (index < 0) return
    if (direction === 'backward') {
      if (offset > 0) offset--
      else if (index > 0) { index--; offset = texts[index].length - 1 }
    } else {
      if (offset < texts[index].length) offset++
      else if (index < texts.length - 1) { index++; offset = 1 }
    }
    this.collapse(texts[index], offset)
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

async function mount(initialLibrary = kb, options: { kind?: string; busy?: boolean; attachments?: Attachment[]; sendBlock?: string; sessionId?: string } = {}) {
  const handle = createRef<ComposerEditorHandle>(), chat = createRef<HTMLDivElement>()
  let text = 'abcd'
  let libraries = initialLibrary ? [initialLibrary] : []
  let toast = ''
  let sessionId = options.sessionId ?? 'A'
  const contextVersion = 1
  const saves: string[][] = []
  const sends: string[] = []
  const refreshFiles = vi.fn()
  const render = () => {
    const knowledge: SessionKnowledge = {
      state: { sessionId, contextVersion, revision: 1, libraries, pendingIds: null, items: [kb], previewItems: [], mode: 'demo', catalogMode: 'demo', unavailableReason: '', loading: false, saving: false, listing: false, refreshing: false, fromCache: false, catalogUpdatedAt: 0, loaded: true, error: '', listError: '', query: '', page: 1, pageSize: 10, total: 1, hasMore: false, maxSelection: 5 },
      openCatalog: () => {},
      refreshCatalog: () => {},
      closeCatalog: () => {},
      visibleLibraries: libraries,
      save: async (ids) => { saves.push(ids); libraries = ids.map((id) => ({ ...kb, id })); render() },
      toggle: async (id) => { const next = libraries.some((item) => item.id === id) ? libraries.filter((item) => item.id !== id).map((item) => item.id) : [...libraries.map((item) => item.id), id]; await knowledge.save(next) },
      search: noop, loadMore: noop, reload: noop,
      sendBlock: options.sendBlock ?? (libraries.length ? '测试知识库暂不参与回答' : ''),
    }
    root.render(<main><div ref={chat} /><Composer knowledge={knowledge} attachments={options.attachments ?? []} setAttachments={noop}
      retry={undefined} retryDisabled={false} submitting={false} onCancelRetry={noop} onRemoveOriginal={noop}
      input={text} onInputChange={(value) => { text = value; render() }} taRef={handle} chatScrollRef={chat}
      busy={options.busy ?? false} toast={toast} onNotify={(message) => { toast = message; render() }} info={options.kind ? { kind: options.kind } as SessionInfo : null} files={['name']} sessionId={sessionId} onRefreshFiles={refreshFiles}
      onSend={async body => { sends.push(body); return true }} onStop={noop} onToggleMode={noop} onTogglePlan={noop}
      onChooseReasoning={noop} onChooseThinking={noop} onPickModel={noop} onPickScenario={noop} /></main>)
  }
  await act(async () => render())
  const textbox = host.querySelector<HTMLElement>('[role="textbox"]')!
  const editor = getNearestEditorFromDOMNode(textbox)!
  const switchSession = async (id: string) => { sessionId = id; await act(async () => render()) }
  return { handle, textbox, editor, text: () => text, saves, sends, refreshFiles, toast: () => toast, switchSession }
}

const findButton = (label: string) => Array.from(host.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent?.trim().startsWith(label))
async function openAddMenu() {
  await act(async () => host.querySelector<HTMLButtonElement>('button[title^="添加："]')!.click())
}

async function nextFrame() {
  await act(async () => { await new Promise<void>((resolve) => requestAnimationFrame(() => resolve())) })
}

describe('Composer inline editor integration', () => {
  it('selectionNeverInjectsKnowledgeText', async () => {
    const app = await mount(kb)
    expect(app.textbox.querySelector('[data-knowledge-id]')).toBeNull()
    expect(app.text()).toBe('abcd')
    expect(app.textbox.textContent).not.toContain(kb.title)
    expect(host.textContent).not.toContain('知识库问答')
  })

  it('keepsFileAndAgentActionsAvailableWithSelection', async () => {
    const app = await mount(kb, { kind: 'knowledge', busy: false })
    await act(async () => host.querySelector<HTMLButtonElement>('button[title^="添加："]')!.click())
    const file = Array.from(host.querySelectorAll('button')).find(button => button.textContent?.trim() === '文件')!
    expect(file.disabled).toBe(false)
    expect(app.text()).toBe('abcd')
  })

  it('honorsSendBlockWithoutKnowledgeBanner', async () => {
    const app = await mount(kb, { kind: 'knowledge', sendBlock: '测试知识库暂不参与回答' })
    await act(async () => app.textbox.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })))
    expect(app.sends).toEqual([])
    expect(app.toast()).toContain('测试知识库暂不参与回答')
    expect(host.textContent).not.toContain('知识库问答 · 仅支持文字')
  })

  it('moves a middle caret to the appended file trigger before typing its name', async () => {
    const app = await mount()
    await act(async () => app.handle.current!.setSelectionRange(2, 2))
    expect(app.handle.current!.getSelection()).toEqual({ start: 2, end: 2 })
    await act(async () => { host.querySelector<HTMLButtonElement>('button[title^="添加："]')!.click() })
    const fileOption = Array.from(host.querySelectorAll<HTMLButtonElement>('button')).find((element) => element.textContent?.trim() === '文件')!
    await act(async () => { fileOption.click() })
    await nextFrame()
    expect(app.text()).toBe('abcd #')
    await act(async () => { app.editor.dispatchCommand(CONTROLLED_TEXT_INSERTION_COMMAND, 'name') })
    expect(app.text()).toBe('abcd #name')
    expect(app.refreshFiles).toHaveBeenCalledOnce()
    expect(app.text()).not.toContain(kb.title)
    expect(app.textbox.textContent).not.toContain(kb.title)
  })
})

describe('Composer image generation entry', () => {
  it('「+」里开启生成图片：只改本会话开关，不动知识库，显示状态标记', async () => {
    const app = await mount(kb)
    await openAddMenu()
    const item = findButton('生成图片')!
    expect(item.getAttribute('aria-checked')).toBe('false')
    await act(async () => item.click())
    expect(imageApi.set).toHaveBeenCalledWith({ sessionId: 'A', enabled: true, expectedRevision: 0 })
    expect(app.saves).toEqual([])
    expect(host.textContent).toContain('生图 · image-model')
  })

  it('回合进行中禁止切换', async () => {
    await mount(kb, { busy: true })
    await openAddMenu()
    expect(findButton('生成图片')!.disabled).toBe(true)
  })

  it('未配置模型时标记提示去设置选择，不自动选择模型', async () => {
    imageApi.get.mockImplementation(async (id: string) => imageView(id, { enabled: true, available: false, model: undefined, size: '', reason: '尚未选择生图模型，请在「设置 → 图片生成」中选择' }))
    await mount(kb)
    expect(host.textContent).toContain('生图 · 未就绪')
    await act(async () => findButton('生图')!.click())
    expect(host.textContent).toContain('尚未选择生图模型')
    expect(host.textContent).toContain('选择生图模型')
    expect(imageApi.set).not.toHaveBeenCalled()
  })

  it('开关保存失败时提示原因且不显示已开启', async () => {
    imageApi.set.mockRejectedValueOnce(new Error('图片生成记录无法读取或保存'))
    const app = await mount(kb)
    await openAddMenu()
    await act(async () => findButton('生成图片')!.click())
    expect(app.toast()).toContain('无法读取或保存')
    expect(host.textContent).not.toContain('生图 · image-model')
  })

  it('新会话草稿保存失败时不发送首条消息', async () => {
    const app = await mount(null as unknown as KnowledgeBaseSummary, { sessionId: '' })
    await openAddMenu()
    await act(async () => findButton('生成图片')!.click())
    expect(imageApi.set).not.toHaveBeenCalled()
    await app.switchSession('NEW')
    imageApi.set.mockRejectedValueOnce(new Error('草稿未能保存'))
    await act(async () => app.textbox.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })))
    expect(imageApi.set).toHaveBeenCalledWith({ sessionId: 'NEW', enabled: true, expectedRevision: 0 })
    expect(app.sends).toEqual([])
    expect(app.toast()).toContain('草稿未能保存')

    await act(async () => app.textbox.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })))
    expect(app.sends).toEqual(['abcd'])
  })
})

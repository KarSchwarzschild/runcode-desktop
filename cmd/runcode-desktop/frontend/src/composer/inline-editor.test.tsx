// @vitest-environment jsdom
import { act, createRef } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { CONTROLLED_TEXT_INSERTION_COMMAND, getNearestEditorFromDOMNode } from 'lexical'
import { InlineEditor, type ComposerEditorHandle, type InlineEditorProps } from './inline-editor'
import type { KnowledgeBaseSummary } from '@/core/bridge'

const kb: KnowledgeBaseSummary = { id: 'private-id', title: '产品知识库', description: '', path: '', source: 'demo', documentCount: 2, documentCountKnown: true }
let host: HTMLDivElement, root: Root
beforeEach(() => {
  ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  host = document.createElement('div'); document.body.append(host); root = createRoot(host)
  Range.prototype.getBoundingClientRect = () => new DOMRect()
  Range.prototype.getClientRects = () => [] as unknown as DOMRectList
  Selection.prototype.modify = function (_alter, direction) {
    const walker = document.createTreeWalker(host.querySelector('[contenteditable]')!, NodeFilter.SHOW_TEXT)
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
afterEach(async () => { await act(async () => root.unmount()); host.remove() })

async function mount(initial = 'hello', initialLibraries = [kb], handlers: Partial<InlineEditorProps> = {}) {
  const handle = createRef<ComposerEditorHandle>(), element = createRef<HTMLDivElement>()
  let value = initial, libraries = initialLibraries
  const changes: string[] = [], knowledgeChanges: string[][] = []
  const render = () => root.render(<InlineEditor ref={handle} elementRef={element} id="test-input" placeholder="请输入" className="" value={value} libraries={libraries} knowledgeReadOnly readOnly={false}
    onChange={(text) => { value = text; changes.push(text); render() }} onKnowledgeChange={(ids) => { knowledgeChanges.push(ids); libraries = ids.map(id => ({ ...kb, id })); render() }} {...handlers} />)
  await act(async () => render())
  const editor = getNearestEditorFromDOMNode(element.current!)!
  return { handle, element, editor, changes, knowledgeChanges, text: () => value,
    update: async (text: string, libs = libraries) => { await act(async () => { value = text; libraries = libs; render() }) },
    select: async (start: number, end = start) => { await act(async () => handle.current!.setSelectionRange(start, end)) },
  }
}

describe('inline composer editor', () => {
  it('selectionNeverInjectsKnowledgeText', async () => {
    const app = await mount('问题', [kb])
    expect(app.element.current!.querySelector('[data-knowledge-id]')).toBeNull()
    expect(app.element.current!.textContent).toBe('问题')
    expect(app.text()).toBe('问题')
    expect(app.knowledgeChanges).toEqual([])
  })

  it('editorUndoDoesNotRestoreKnowledgeSelection', async () => {
    const app = await mount('abcd', [kb])
    await app.select(2)
    await act(async () => app.editor.dispatchCommand(CONTROLLED_TEXT_INSERTION_COMMAND, '！'))
    expect(app.text()).toBe('ab！cd')
    await app.update('abcd', [])
    expect(app.element.current!.querySelector('[data-knowledge-id]')).toBeNull()
    expect(app.knowledgeChanges).toEqual([])
  })

  it('keepsPlainTextEditingAndCaret', async () => {
    const app = await mount('hello', [])
    await app.select(5)
    await act(async () => app.editor.dispatchCommand(CONTROLLED_TEXT_INSERTION_COMMAND, '!'))
    expect(app.text()).toBe('hello!')
    expect(app.handle.current!.getSelection()).toEqual({ start: 6, end: 6 })
  })
})

import {
  $applyNodeReplacement, $createLineBreakNode, $createParagraphNode, $createRangeSelection,
  $createTextNode, $getRoot, $isElementNode, $isLineBreakNode, $isTextNode, $setSelection,
  TextNode, type EditorConfig, type LexicalNode, type NodeKey, type PointType, type SerializedTextNode,
} from 'lexical'
import type { KnowledgeBaseSummary } from '@/core/bridge'

type SerializedKnowledgeNode = SerializedTextNode & { knowledgeId: string }

/** A token participates in Lexical selection/history, but never in the question projection. */
export class KnowledgeNode extends TextNode {
  __knowledgeId: string
  static getType() { return 'composer-knowledge' }
  static clone(node: KnowledgeNode) { return new KnowledgeNode(node.__knowledgeId, node.__text, node.__key) }
  constructor(id: string, title: string, key?: NodeKey) { super(title, key); this.__knowledgeId = id }
  static importJSON(serialized: SerializedKnowledgeNode) {
    return $createKnowledgeNode({ id: serialized.knowledgeId, title: serialized.text }).updateFromJSON(serialized)
  }
  exportJSON(): SerializedKnowledgeNode { return { ...super.exportJSON(), knowledgeId: this.__knowledgeId, type: KnowledgeNode.getType(), version: 1 } }
  createDOM(config: EditorConfig) {
    const element = super.createDOM(config)
    element.className = 'composer-knowledge-reference'
    element.dataset.knowledgeId = this.__knowledgeId
    element.style.fontSize = '0.85em'
    element.style.color = 'var(--color-faint, #8b8e95)'
    element.style.marginInline = '0.2em'
    element.setAttribute('spellcheck', 'false')
    return element
  }
  exportDOM() {
    // Clipboard HTML must carry visible text only, never reference identity.
    const element = document.createElement('span')
    element.textContent = this.getTextContent()
    return { element }
  }
  canInsertTextBefore() { return false }
  canInsertTextAfter() { return false }
  isTextEntity() { return true }
  getKnowledgeId() { return this.getLatest().__knowledgeId }
}

export function $createKnowledgeNode(library: Pick<KnowledgeBaseSummary, 'id' | 'title'>) {
  return $applyNodeReplacement(new KnowledgeNode(library.id, library.title)).setMode('token')
}

export function $isKnowledgeNode(node: LexicalNode | null | undefined): node is KnowledgeNode { return node instanceof KnowledgeNode }

function $text(node: LexicalNode): string {
  if ($isKnowledgeNode(node)) return ''
  if ($isElementNode(node)) return node.getChildren().map($text).join(node.getType() === 'root' ? '\n' : '')
  return node.getTextContent()
}

export function $questionText() { return $text($getRoot()) }

export function $references() {
  const references: { node: KnowledgeNode; offset: number }[] = []
  let offset = 0
  const walk = (node: LexicalNode) => {
    if ($isKnowledgeNode(node)) references.push({ node, offset })
    else if ($isElementNode(node)) node.getChildren().forEach((child, index) => {
      if (node.getType() === 'root' && index > 0) offset++
      walk(child)
    })
    else offset += node.getTextContentSize()
  }
  walk($getRoot())
  return references
}

export function $pointOffset(point: PointType): number {
  let offset = 0, found = false
  const walk = (node: LexicalNode) => {
    if (found) return
    if (node.getKey() === point.key) {
      if ($isElementNode(node)) {
        offset += node.getChildren().slice(0, point.offset).reduce((sum, child) => sum + $text(child).length, 0)
        if (node.getType() === 'root') offset += Math.min(point.offset, Math.max(0, node.getChildrenSize() - 1))
      }
      else if (!$isKnowledgeNode(node)) offset += point.offset
      found = true
    } else if ($isElementNode(node)) node.getChildren().forEach((child, index) => {
      if (!found && node.getType() === 'root' && index > 0) offset++
      walk(child)
    })
    else offset += $text(node).length
  }
  walk($getRoot())
  return offset
}

export function $selectTextRange(start: number, end = start) {
  const selection = $createRangeSelection()
  const set = (point: PointType, requested: number) => {
    let remaining = Math.max(0, Math.min(requested, $questionText().length)), done = false
    const walk = (node: LexicalNode) => {
      if (done || $isKnowledgeNode(node)) return
      if ($isTextNode(node)) {
        const size = node.getTextContentSize()
        if (remaining <= size) { point.set(node.getKey(), remaining, 'text'); done = true }
        else remaining -= size
      } else if ($isLineBreakNode(node)) {
        if (remaining === 0) { point.set(node.getParentOrThrow().getKey(), node.getIndexWithinParent(), 'element'); done = true }
        else remaining--
      } else if ($isElementNode(node)) node.getChildren().forEach((child, index) => {
        if (!done && node.getType() === 'root' && index > 0) remaining--
        walk(child)
      })
    }
    walk($getRoot())
    if (!done) {
      const last = $getRoot().getLastChild()
      const parent = $isElementNode(last) ? last : $getRoot()
      point.set(parent.getKey(), parent.getChildrenSize(), 'element')
    }
  }
  set(selection.anchor, start); set(selection.focus, end); $setSelection(selection)
  return selection
}

function $appendText(parent: ReturnType<typeof $createParagraphNode>, text: string) {
  text.split('\n').forEach((line, index) => {
    if (index) parent.append($createLineBreakNode())
    if (line) parent.append($createTextNode(line))
  })
}

/** Controlled text changes retain the same reference nodes and their relative text positions. */
export function $replaceQuestion(text: string) {
  const previous = $questionText()
  let prefix = 0, suffix = 0
  while (prefix < previous.length && prefix < text.length && previous[prefix] === text[prefix]) prefix++
  while (suffix < previous.length - prefix && suffix < text.length - prefix && previous[previous.length - 1 - suffix] === text[text.length - 1 - suffix]) suffix++
  const references = $references().map(({ node, offset }) => ({ node, offset: offset <= prefix ? offset : offset >= previous.length - suffix ? offset + text.length - previous.length : Math.min(offset, text.length - suffix) }))
  const paragraph = $createParagraphNode()
  $getRoot().clear().append(paragraph)
  let offset = 0
  for (const reference of references) {
    const next = Math.max(offset, Math.min(reference.offset, text.length))
    $appendText(paragraph, text.slice(offset, next)); paragraph.append(reference.node); offset = next
  }
  $appendText(paragraph, text.slice(offset))
}

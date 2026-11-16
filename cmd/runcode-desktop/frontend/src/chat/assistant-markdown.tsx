// AssistantMarkdown 是主助手正文的 Markdown 渲染器：在通用 Markdown 插槽上叠加
// 静态预览识别（围栏完整性、类型、去重与单消息上限）。流式输出期间一律显示源码；
// textOnly（知识库）不启用预览。不处理生图工具卡片——那是 GeneratedImageCard 的事。
import { useMemo, type ReactNode } from 'react'
import { unified } from 'unified'
import remarkParse from 'remark-parse'
import remarkGfm from 'remark-gfm'
import { visit } from 'unist-util-visit'
import { Markdown, type CodeBlockRenderInfo, type MarkdownProps } from '@/ui/markdown'
import {
  classifyCodeBlock, contentDigest, fenceClosed, planMessagePreviews, type StaticPreviewKind,
} from '@/preview/static-detect'
import { InlinePreview } from './inline-preview'

export type AssistantMarkdownProps = Omit<MarkdownProps, 'renderCodeBlock'> & {
  messageId: string
  streaming?: boolean
  onContentHeightChange?: () => void
}

export type FenceBlock = { lang: string; meta: string; value: string; start: number; end: number }

// listFenceBlocks 用与界面相同的 remark-gfm 解析出围栏代码块及原文偏移。
export function listFenceBlocks(source: string): FenceBlock[] {
  const tree = unified().use(remarkParse).use(remarkGfm).parse(source)
  const out: FenceBlock[] = []
  visit(tree, 'code', (node) => {
    const start = node.position?.start?.offset
    const end = node.position?.end?.offset
    if (typeof start !== 'number' || typeof end !== 'number') return
    out.push({
      lang: node.lang ?? '',
      meta: node.meta ?? '',
      value: node.value,
      start,
      end,
    })
  })
  return out
}

export function selectPreviewBlocks(source: string, streaming: boolean): Map<string, StaticPreviewKind> {
  const selected = new Map<string, StaticPreviewKind>()
  if (streaming) return selected
  const candidates = []
  for (const b of listFenceBlocks(source)) {
    const closed = fenceClosed(source, b.start, b.end)
    const decision = classifyCodeBlock({ lang: b.lang, meta: b.meta, value: b.value, closed })
    if (!decision.kind) continue
    candidates.push({ id: `${b.start}:${b.end}`, kind: decision.kind, digest: contentDigest(b.value.trim()) })
  }
  const chosen = planMessagePreviews(candidates)
  for (const c of candidates) {
    if (chosen.has(c.id)) selected.set(c.id, c.kind)
  }
  return selected
}

export function AssistantMarkdown({ messageId: _messageId, streaming = false, onContentHeightChange, children, textOnly, ...rest }: AssistantMarkdownProps) {
  const enable = !streaming && !textOnly
  const selected = useMemo(
    () => (enable ? selectPreviewBlocks(children, false) : new Map<string, StaticPreviewKind>()),
    [enable, children],
  )

  const renderCodeBlock = useMemo(() => {
    if (!enable || selected.size === 0) return undefined
    return (info: CodeBlockRenderInfo): ReactNode | null => {
      const id = `${info.start}:${info.end}`
      const kind = selected.get(id)
      if (!kind) return null
      return (
        <InlinePreview
          kind={kind}
          source={info.value}
          defaultNode={info.defaultNode}
          onHeightChange={onContentHeightChange}
        />
      )
    }
  }, [enable, selected, onContentHeightChange])

  return (
    <Markdown {...rest} textOnly={textOnly} renderCodeBlock={renderCodeBlock}>
      {children}
    </Markdown>
  )
}

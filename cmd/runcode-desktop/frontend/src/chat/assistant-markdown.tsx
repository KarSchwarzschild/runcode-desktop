// AssistantMarkdown 是主助手正文的 Markdown 渲染器：在通用 Markdown 插槽上叠加
// 静态预览识别（围栏完整性、类型、去重与单消息上限）。流式输出期间一律显示源码；
// textOnly 纯文本渲染不启用预览。不处理生图工具卡片——那是 GeneratedImageCard 的事。
import { useMemo, type ReactNode } from 'react'
import { unified } from 'unified'
import remarkParse from 'remark-parse'
import remarkGfm from 'remark-gfm'
import { visit } from 'unist-util-visit'
import { Markdown, type CodeBlockRenderInfo, type MarkdownProps } from '@/ui/markdown'
import {
  classifyCodeBlock, contentDigest, fenceClosed, planMessagePreviews, type StaticPreviewKind,
} from '@/preview/static-detect'
import { extractFilePaths } from '@/preview/classify'
import { isStaticInlinePath } from '@/session/inline-preview-status'
import type { CodePreviewTarget } from '@/preview/code-preview'
import { InlinePreview } from './inline-preview'

export type AssistantMarkdownProps = Omit<MarkdownProps, 'renderCodeBlock'> & {
  messageId: string
  streaming?: boolean
  onContentHeightChange?: () => void
  /** 接通 inline-preview-status：成功内联后抑制同路径右侧自动预览。 */
  sessionId?: string
  onOpenCodePreview?: (target: CodePreviewTarget) => void
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

function extOf(path: string): string {
  return path.replace(/\\/g, '/').split('/').pop()?.split('.').pop()?.toLowerCase() ?? ''
}

/** 同条消息里提到的、扩展名与预览类型相符的静态路径（不猜裸字符串是否存在）。 */
export function relatedPathsForKind(source: string, kind: StaticPreviewKind): string[] {
  const want = kind === 'svg' ? new Set(['svg'])
    : kind === 'mermaid' ? new Set(['mmd', 'mermaid'])
    : new Set(['html', 'htm'])
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of extractFilePaths(source)) {
    const p = raw.replace(/\\/g, '/')
    if (!isStaticInlinePath(p) || !want.has(extOf(p)) || seen.has(p)) continue
    seen.add(p)
    out.push(p)
  }
  return out
}

export function AssistantMarkdown({
  messageId, streaming = false, onContentHeightChange, children, textOnly, sessionId,
  onOpenCodePreview, ...rest
}: AssistantMarkdownProps) {
  const enable = !streaming && !textOnly
  const selected = useMemo(
    () => (enable ? selectPreviewBlocks(children, false) : new Map<string, StaticPreviewKind>()),
    [enable, children],
  )
  const pathsByKind = useMemo(() => {
    if (!enable || !sessionId) return null
    return {
      svg: relatedPathsForKind(children, 'svg'),
      mermaid: relatedPathsForKind(children, 'mermaid'),
      html: relatedPathsForKind(children, 'html'),
    }
  }, [enable, sessionId, children])

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
          messageId={messageId}
          previewId={id}
          sessionId={sessionId}
          relatedPaths={pathsByKind?.[kind]}
          onOpenCodePreview={onOpenCodePreview}
        />
      )
    }
  }, [enable, selected, onContentHeightChange, messageId, sessionId, pathsByKind, onOpenCodePreview])

  return (
    <Markdown {...rest} textOnly={textOnly} renderCodeBlock={renderCodeBlock}>
      {children}
    </Markdown>
  )
}

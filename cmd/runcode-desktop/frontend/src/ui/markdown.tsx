import type { ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'

// CodeBlockRenderInfo 把围栏代码块的原始信息交给可选插槽。value 来自 hast 文本节点
// 拼接（rehype-highlight 之后仍保留原文），不能用 String(React children)——高亮后
// children 是带样式的元素树。start/end 是原文偏移，供围栏完整性判断使用。
export type CodeBlockRenderInfo = {
  lang: string
  meta: string
  value: string
  start: number
  end: number
  defaultNode: ReactNode
}

export type MarkdownProps = {
  children: string
  onOpenFile?: (relPath: string) => void
  resolveFile?: (token: string) => string | null
  textOnly?: boolean
  // 未传或返回 null 时保持原代码块；只影响本实例，不改变其它 Markdown 调用点。
  renderCodeBlock?: (info: CodeBlockRenderInfo) => ReactNode | null
}

function hastText(node: unknown): string {
  if (!node || typeof node !== 'object') return ''
  const n = node as { type?: string; value?: string; children?: unknown[] }
  if (n.type === 'text') return n.value ?? ''
  if (Array.isArray(n.children)) return n.children.map(hastText).join('')
  return ''
}

function classList(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String)
  if (typeof value === 'string') return value.split(/\s+/).filter(Boolean)
  return []
}

// Markdown renders assistant text as GitHub-flavored Markdown with code syntax
// highlighting, styled to match the app theme. Block code keeps the highlight.js
// theme colors; inline code gets a compact chip.
export function Markdown({ children, onOpenFile, resolveFile, textOnly = false, renderCodeBlock }: MarkdownProps) {
  const source = children
  return (
    <div className="mdx">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[[rehypeHighlight, { detect: false, ignoreMissing: true }]]}
        components={{
          // 知识库回答中的模型自报链接/图片没有来源权威；只显示文本，避免自动图片请求
          // 或本地文件预览。可点击的真实来源统一由 KnowledgeSources 按宿主元数据提供。
          ...(textOnly ? { img: ({ alt }: { alt?: string }) => <span>{alt || '图片'}</span> } : {}),
          p: (props) => <p className="my-2 first:mt-0 last:mb-0 leading-[1.75]" {...props} />,
          ul: (props) => <ul className="my-2 pl-5 list-disc space-y-1" {...props} />,
          ol: (props) => <ol className="my-2 pl-5 list-decimal space-y-1" {...props} />,
          li: (props) => <li className="leading-[1.7]" {...props} />,
          strong: (props) => <strong className="font-semibold text-ink" {...props} />,
          a: ({ href, children: aChildren, ...props }) => {
            if (textOnly) return <span>{aChildren}</span>
            const rel = href && resolveFile ? resolveFile(href) : null
            if (rel && onOpenFile) {
              return <a className="text-primaryink underline underline-offset-2 cursor-pointer" href={href} onClick={(e) => { e.preventDefault(); onOpenFile(rel) }}>{aChildren}</a>
            }
            return <a className="text-primaryink underline underline-offset-2" target="_blank" rel="noreferrer" href={href} {...props}>{aChildren}</a>
          },
          h1: (props) => <h1 className="text-[18px] font-bold mt-3 mb-1.5" {...props} />,
          h2: (props) => <h2 className="text-[16px] font-bold mt-3 mb-1.5" {...props} />,
          h3: (props) => <h3 className="text-[15px] font-semibold mt-2.5 mb-1" {...props} />,
          blockquote: (props) => <blockquote className="border-l-2 border-line2 pl-3 my-2 text-muted" {...props} />,
          hr: () => <hr className="my-3 border-line" />,
          table: (props) => <table className="my-2 border-collapse text-[13px] block overflow-auto" {...props} />,
          th: (props) => <th className="border border-line2 px-2 py-1 bg-surface2 text-left" {...props} />,
          td: (props) => <td className="border border-line2 px-2 py-1" {...props} />,
          pre: ({ node, children: preChildren, ...props }) => {
            const defaultNode = <pre className="my-2 bg-inset border border-line2 rounded-lg p-3 overflow-auto text-[13px] font-mono leading-[1.55]" {...props}>{preChildren}</pre>
            if (textOnly || !renderCodeBlock || !node) return defaultNode
            const code = node.children?.[0]
            if (!code || code.type !== 'element' || code.tagName !== 'code') return defaultNode
            const start = node.position?.start?.offset
            const end = node.position?.end?.offset
            if (typeof start !== 'number' || typeof end !== 'number') return defaultNode
            const langClass = classList(code.properties?.className).find((c) => c.startsWith('language-'))
            const lang = langClass ? langClass.slice('language-'.length) : ''
            const meta = typeof (code.data as { meta?: unknown } | undefined)?.meta === 'string'
              ? (code.data as { meta: string }).meta
              : ''
            // highlight 后的文本末尾常带换行；classify 会 trim，这里保留解析器原文形状。
            const value = hastText(code).replace(/\n$/, '')
            // 用原文切片核对偏移仍落在本条消息里，防止错位时误判围栏。
            if (start < 0 || end > source.length || end <= start) return defaultNode
            const rendered = renderCodeBlock({ lang, meta, value, start, end, defaultNode })
            return rendered ?? defaultNode
          },
          code: ({ className, children: codeChildren, ...rest }) => {
            const text = String(codeChildren ?? '')
            const isBlock = /language-/.test(className || '') || text.includes('\n')
            if (isBlock) {
              return <code className={className} {...rest}>{codeChildren}</code>
            }
            const rel = !textOnly && resolveFile ? resolveFile(text) : null
            if (rel && onOpenFile) {
              return (
                <code
                  className="bg-inset text-primaryink px-1.5 py-0.5 rounded text-[0.9em] font-mono break-words cursor-pointer hover:underline"
                  title={'预览 ' + rel}
                  onClick={() => onOpenFile(rel)}
                >
                  {codeChildren}
                </code>
              )
            }
            return (
              <code className="bg-inset text-[#7c3aed] px-1.5 py-0.5 rounded text-[0.9em] font-mono break-words" {...rest}>
                {codeChildren}
              </code>
            )
          },
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}

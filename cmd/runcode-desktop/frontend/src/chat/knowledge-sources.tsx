import type { KnowledgeSource, QuestionKnowledge } from '@/core/bridge'
import { SystemNote } from '@/ui/feedback'

export function knowledgeSourceItems(knowledge: QuestionKnowledge): KnowledgeSource[] {
  if (!knowledge.applied) return []
  return [...new Map((knowledge.sources ?? []).filter(source => source.id && source.libraryId && source.documentId && source.chunkId).map(source => [source.id, source])).values()]
}

function sourceURL(value?: string): string | undefined {
  // 只使用宿主提供的真实文档 URL；缺失或非 HTTP(S) 时保留纯文本，绝不拼接假链接。
  if (!value) return
  try { const url = new URL(value); return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password ? url.href : undefined }
  catch { return }
}

// 来源只有宿主验证的元数据，没有检索正文；标题作为文本转义，链接单独校验协议。
export function KnowledgeSources({ knowledge }: { knowledge?: QuestionKnowledge }) {
  if (!knowledge) return null
  const sources = knowledgeSourceItems(knowledge)
  const status = knowledge.status === 'interrupted' ? '上次回答已中断，请重新生成；部分回答不作为完成历史使用'
    : knowledge.status === 'failed' ? knowledge.error || '知识库查询失败；普通回答仍可继续'
    : knowledge.status === 'cancelled' ? '知识库查询已取消；停止或失败回合的资料不会自动复用'
    : knowledge.status === 'empty' ? '本次未检索到相关资料（未检索到可用参考资料）'
    : knowledge.status === 'matched' && knowledge.applied ? sources.length > 0 ? '已向模型提供参考资料' : '未向模型提供可用参考资料'
    : knowledge.applied ? sources.length > 0 ? '已向模型提供参考资料' : '未向模型提供可用参考资料'
    : knowledge.status === 'searching' || knowledge.status === 'retrieving' ? '正在检索知识库…'
      : knowledge.status === 'not_used' ? ''
        : knowledge.status === 'ready' ? '参考资料已准备，尚未用于回答'
          : ['pending', 'accepted'].includes(knowledge.status) ? ''
            : ''
  return <div className="max-w-[82%] text-[11px] text-muted" role="status">
    {(knowledge.libraries?.length ?? 0) > 0 && <p>{knowledge.libraries?.map(library => library.title).join('、')}</p>}
    <SystemNote tone={knowledge.status === 'failed' ? 'warning' : 'neutral'} selectable>{status}</SystemNote>
    {knowledge.truncated && <p>回答已达到输出上限，内容可能不完整。</p>}
    {sources.length > 0 && <details className="mt-1"><summary className="cursor-pointer">参考资料 · {sources.length} 条</summary>
      <p>检索得到的参考资料，不代表模型逐句引用。</p>
      <ol>{sources.map((source, index) => <li key={source.id} className="break-words" title={`知识库 ${source.libraryId} · 文档 ${source.documentId} · 来源 ${source.id}`}>
        [{index + 1}] {sourceURL(source.sourceUrl) ? <a href={sourceURL(source.sourceUrl)} target="_blank" rel="noopener noreferrer" className="text-primary underline">{source.title}</a> : source.title} · 片段 {source.chunkId}
      </li>)}</ol>
    </details>}
  </div>
}

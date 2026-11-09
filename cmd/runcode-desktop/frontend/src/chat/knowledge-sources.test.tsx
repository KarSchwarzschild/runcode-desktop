import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { KnowledgeSources, knowledgeSourceItems } from './knowledge-sources'

const source = { id: '1', libraryId: 'lib', documentId: 'doc', chunkId: 'chunk-1', title: '<img src=x onerror=alert(1)>', sourceUrl: 'javascript:alert(1)' }
const knowledge = { libraries: [], status: 'ready', applied: false, sources: [source] }
describe('knowledge reference presentation', () => {
  it('shows interrupted, failed and truncated recovery without treating partial answers as completed', () => {
    const interrupted = renderToStaticMarkup(<KnowledgeSources knowledge={{ ...knowledge, status: 'interrupted', applied: true }} />)
    expect(interrupted).toContain('已中断')
    const failed = renderToStaticMarkup(<KnowledgeSources knowledge={{ ...knowledge, status: 'failed', applied: true, error: '回答未保存' }} />)
    expect(failed).toContain('回答未保存')
    const truncated = renderToStaticMarkup(<KnowledgeSources knowledge={{ ...knowledge, status: 'completed', applied: true, truncated: true }} />)
    expect(truncated).toContain('输出上限')
  })
  it('links only a real HTTP source URL and never invents a link for a missing URL', () => {
    const markup = renderToStaticMarkup(<KnowledgeSources knowledge={{ ...knowledge, applied: true, sources: [{ ...source, sourceUrl: 'https://example.com/document/1' }, { ...source, id: '2', sourceUrl: '' }] }} />)
    expect(markup).toContain('href="https://example.com/document/1"')
    expect(markup.match(/href=/g)).toHaveLength(1)
  })
  it('distinguishes preparation from actually applied reference material', () => {
    const prepared = renderToStaticMarkup(<KnowledgeSources knowledge={knowledge} />)
    expect(prepared).toContain('尚未用于回答')
    expect(prepared).not.toContain('已参考')
    expect(knowledgeSourceItems(knowledge)).toEqual([])
    const used = renderToStaticMarkup(<KnowledgeSources knowledge={{ ...knowledge, applied: true }} />)
    expect(used).toContain('参考资料')
    expect(used).toContain('不代表模型逐句引用')
    expect(used).toContain('chunk-1')
    expect(used).toContain('&lt;img')
    expect(used).not.toContain('href=')
  })
  it('exposes only actual numbered source IDs, deduplicates them and rejects unknown citation lookup', () => {
    const items = knowledgeSourceItems({ ...knowledge, applied: true, sources: [source, source, { ...source, id: '2', chunkId: 'chunk-2' }] })
    expect(items.map(item => item.id)).toEqual(['1', '2'])
    expect(items.find(item => item.id === '99')).toBeUndefined()
  })
  it('numbers the displayed source list without exposing opaque engine citation IDs as labels', () => {
    const markup = renderToStaticMarkup(<KnowledgeSources knowledge={{ ...knowledge, applied: true, sources: [{ ...source, id: 'kb:opaque-source-identity' }] }} />)
    expect(markup).toContain('[1]')
    expect(markup.replace(/<[^>]*>/g, '')).not.toContain('kb:opaque-source-identity')
  })
  it('shows empty, failed and cancelled preparation without pretending sources were applied', () => {
    for (const status of ['empty', 'failed', 'cancelled', 'not_used']) {
      const markup = renderToStaticMarkup(<KnowledgeSources knowledge={{ ...knowledge, status }} />)
      expect(markup).not.toContain('已参考')
      expect(markup).not.toContain('chunk-1')
      expect(markup).not.toContain('检索上下文尚未接入')
    }
  })
  it('does not show retrieving placeholder for not_used selection-only state', () => {
    const markup = renderToStaticMarkup(<KnowledgeSources knowledge={{ ...knowledge, status: 'not_used', applied: false, sources: [] }} />)
    expect(markup).not.toContain('正在检索')
  })
  it('shows no matches when an empty retrieval was applied to the request, including reopened metadata', () => {
    const empty = { ...knowledge, status: 'empty', applied: true, sources: [] }
    const markup = renderToStaticMarkup(<KnowledgeSources knowledge={empty} />)
    expect(markup).toContain('未检索到可用参考资料')
    expect(markup).not.toContain('已向模型提供参考资料')
    expect(markup).not.toContain('<details')
    expect(knowledgeSourceItems(empty)).toEqual([])
  })
})

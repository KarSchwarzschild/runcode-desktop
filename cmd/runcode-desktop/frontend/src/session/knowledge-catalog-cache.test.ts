import { describe, expect, it } from 'vitest'
import { KnowledgeCatalogCache, catalogKey } from './knowledge-catalog-cache'
import type { KnowledgeBaseSummary } from '@/core/bridge'

const library = (id: string, title = id): KnowledgeBaseSummary => ({
  id, title, description: '', path: '', source: 'demo', documentCount: 0, documentCountKnown: false,
})

const page = (items: KnowledgeBaseSummary[]) => ({
  items, page: 1, pageSize: 10, total: items.length, hasMore: false, mode: 'real' as const, maxSelection: 5,
})

describe('KnowledgeCatalogCache', () => {
  it('reusesExactQueryWithoutCaseFolding', () => {
    const cache = new KnowledgeCatalogCache()
    const lower = catalogKey('ns', 's', 'Ab', 10)
    const upper = catalogKey('ns', 's', 'ab', 10)
    expect(lower).not.toBe(upper)
    cache.put(lower, { pages: [page([library('one', 'Ab')])], updatedAt: 1 })
    expect(cache.get(upper)).toBeUndefined()
    expect(cache.get(lower)?.pages[0].items?.[0].id).toBe('one')
  })

  it('trims query in the cache key but keeps interior spaces', () => {
    const cache = new KnowledgeCatalogCache()
    const key = catalogKey('ns', 's', '  课程 资料  ', 10)
    expect(key).toBe(catalogKey('ns', 's', '课程 资料', 10))
    cache.put(key, { pages: [page([library('a')])], updatedAt: 1 })
    expect(cache.get(catalogKey('ns', 's', '课程 资料', 10))).toBeTruthy()
  })

  it('evictsWholeQueriesWithinAllThreeLimits', () => {
    const cache = new KnowledgeCatalogCache()
    for (let i = 0; i < 8; i++) {
      expect(cache.put(catalogKey('ns', 's', `q${i}`, 10), { pages: [page([library(`id-${i}`)])], updatedAt: i + 1 })).toBe(true)
    }
    expect(cache.put(catalogKey('ns', 's', 'q8', 10), { pages: [page([library('id-8')])], updatedAt: 9 })).toBe(true)
    expect(cache.get(catalogKey('ns', 's', 'q0', 10))).toBeUndefined()
    expect(cache.get(catalogKey('ns', 's', 'q8', 10))).toBeTruthy()
  })

  it('doesNotMutateStoredSnapshot', () => {
    const cache = new KnowledgeCatalogCache()
    const key = catalogKey('ns', 's', '', 10)
    const items = [library('one')]
    cache.put(key, { pages: [page(items)], updatedAt: 1 })
    items[0].title = 'mutated'
    const got = cache.get(key)!
    got.pages[0].items![0].title = 'again'
    expect(cache.get(key)!.pages[0].items![0].title).toBe('one')
  })

  it('invalidatesOneLibraryAcrossQueries', () => {
    const cache = new KnowledgeCatalogCache()
    cache.put(catalogKey('ns', 's', '', 10), { pages: [page([library('one'), library('two')])], updatedAt: 100 })
    cache.put(catalogKey('ns', 's', 'x', 10), { pages: [page([library('two'), library('three')])], updatedAt: 100 })
    cache.invalidateLibrary('two')
    expect(cache.get(catalogKey('ns', 's', '', 10))!.pages[0].items?.map((i) => i.id)).toEqual(['one'])
    expect(cache.get(catalogKey('ns', 's', 'x', 10))!.pages[0].items?.map((i) => i.id)).toEqual(['three'])
    expect(cache.get(catalogKey('ns', 's', '', 10))!.updatedAt).toBe(0)
  })

  it('rejects oversized single snapshot without storing it', () => {
    const cache = new KnowledgeCatalogCache()
    const many = Array.from({ length: 501 }, (_, i) => library(`id-${i}`))
    expect(cache.put(catalogKey('ns', 's', 'big', 10), { pages: [page(many)], updatedAt: 1 })).toBe(false)
    expect(cache.get(catalogKey('ns', 's', 'big', 10))).toBeUndefined()
  })
})

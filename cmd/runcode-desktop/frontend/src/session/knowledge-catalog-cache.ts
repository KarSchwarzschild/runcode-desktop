import type { KnowledgeBaseSummary, KnowledgePage } from '@/core/bridge'

/** 缓存键：命名空间＋会话＋与 Go 一致的 trim 查询＋页大小；不折叠大小写。 */
export type CatalogKey = string

export type CachedCatalogPage = Pick<KnowledgePage, 'items' | 'page' | 'pageSize' | 'total' | 'hasMore' | 'mode' | 'maxSelection'>

export interface CatalogSnapshot {
  pages: CachedCatalogPage[]
  updatedAt: number
}

const MAX_QUERIES = 8
const MAX_ENTRIES = 500
const MAX_BYTES = 2 * 1024 * 1024

export function catalogKey(namespace: string, sessionId: string, query: string, pageSize: number): CatalogKey {
  return JSON.stringify([namespace, sessionId, query.trim(), pageSize])
}

function clonePage(page: CachedCatalogPage): CachedCatalogPage {
  return {
    ...page,
    items: (page.items ?? []).map((item) => ({ ...item })),
  }
}

function cloneSnapshot(value: CatalogSnapshot): CatalogSnapshot {
  return { updatedAt: value.updatedAt, pages: value.pages.map(clonePage) }
}

function entryCount(value: CatalogSnapshot): number {
  return value.pages.reduce((sum, page) => sum + (page.items?.length ?? 0), 0)
}

function byteSize(value: CatalogSnapshot): number {
  return new TextEncoder().encode(JSON.stringify(value)).length
}

/** 只保存成功目录快照；不访问网络、不保存授权结论。 */
export class KnowledgeCatalogCache {
  private readonly order: CatalogKey[] = []
  private readonly store = new Map<CatalogKey, CatalogSnapshot>()

  get(key: CatalogKey): CatalogSnapshot | undefined {
    const hit = this.store.get(key)
    if (!hit) return undefined
    const idx = this.order.indexOf(key)
    if (idx >= 0) {
      this.order.splice(idx, 1)
      this.order.push(key)
    }
    return cloneSnapshot(hit)
  }

  put(key: CatalogKey, value: CatalogSnapshot): boolean {
    const snapshot = cloneSnapshot(value)
    const entries = entryCount(snapshot)
    const bytes = byteSize(snapshot)
    if (entries > MAX_ENTRIES || bytes > MAX_BYTES) return false
    if (this.store.has(key)) {
      this.store.set(key, snapshot)
      const idx = this.order.indexOf(key)
      if (idx >= 0) {
        this.order.splice(idx, 1)
        this.order.push(key)
      }
      return true
    }
    while (this.order.length >= MAX_QUERIES || this.totalEntries() + entries > MAX_ENTRIES || this.totalBytes() + bytes > MAX_BYTES) {
      if (this.order.length === 0) return false
      const evict = this.order.shift()
      if (evict) this.store.delete(evict)
    }
    this.store.set(key, snapshot)
    this.order.push(key)
    return true
  }

  invalidateLibrary(id: string): void {
    for (const key of [...this.order]) {
      const snap = this.store.get(key)
      if (!snap) continue
      let changed = false
      const pages = snap.pages.map((page) => {
        const items = (page.items ?? []).filter((item) => item.id !== id)
        if (items.length !== (page.items?.length ?? 0)) changed = true
        return { ...page, items }
      })
      if (!changed) continue
      // 失效后标记过期，强制下次打开后台刷新。
      this.store.set(key, { pages, updatedAt: 0 })
      const idx = this.order.indexOf(key)
      if (idx >= 0) {
        this.order.splice(idx, 1)
        this.order.push(key)
      }
    }
  }

  clear(): void {
    this.store.clear()
    this.order.length = 0
  }

  /** 在本命名空间已缓存标题中做临时匹配（精确键未命中时的预览）。 */
  previewMatches(namespace: string, sessionId: string, query: string): KnowledgeBaseSummary[] {
    const needle = query.trim().toLowerCase()
    if (!needle) return []
    const seen = new Set<string>()
    const out: KnowledgeBaseSummary[] = []
    for (const key of this.order) {
      let parsed: unknown
      try { parsed = JSON.parse(key) } catch { continue }
      if (!Array.isArray(parsed) || parsed[0] !== namespace || parsed[1] !== sessionId) continue
      const snap = this.store.get(key)
      if (!snap) continue
      for (const page of snap.pages) {
        for (const item of page.items ?? []) {
          if (seen.has(item.id)) continue
          if (!item.title?.toLowerCase().includes(needle)) continue
          seen.add(item.id)
          out.push({ ...item })
        }
      }
    }
    return out
  }

  private totalEntries(): number {
    let sum = 0
    for (const snap of this.store.values()) sum += entryCount(snap)
    return sum
  }

  private totalBytes(): number {
    let sum = 0
    for (const snap of this.store.values()) sum += byteSize(snap)
    return sum
  }
}

export const CATALOG_FRESH_MS = 30_000

import type { KnowledgeBaseSummary, KnowledgeListRequest, KnowledgePage, SessionKnowledgeView, SetSessionKnowledgeRequest } from '@/core/bridge'
import { CATALOG_FRESH_MS, KnowledgeCatalogCache, catalogKey, type CatalogSnapshot } from './knowledge-catalog-cache'

export interface KnowledgeAPI {
  get: (sessionId: string) => Promise<SessionKnowledgeView>
  /** 本地快照：重启后先展示标题，再后台重验。 */
  snapshot?: (sessionId: string) => Promise<SessionKnowledgeView>
  list: (request: KnowledgeListRequest) => Promise<KnowledgePage>
  set: (request: SetSessionKnowledgeRequest) => Promise<SessionKnowledgeView>
}
export interface KnowledgeState {
  sessionId: string
  contextVersion: number
  revision: number
  libraries: KnowledgeBaseSummary[]
  pendingIds: string[] | null
  items: KnowledgeBaseSummary[]
  previewItems: KnowledgeBaseSummary[]
  mode: string
  catalogMode: string
  unavailableReason: string
  loading: boolean
  saving: boolean
  listing: boolean
  refreshing: boolean
  fromCache: boolean
  catalogUpdatedAt: number
  loaded: boolean
  error: string
  listError: string
  query: string
  page: number
  pageSize: number
  total: number
  hasMore: boolean
  maxSelection: number
}
const emptyState = (sessionId = '', contextVersion = 0): KnowledgeState => ({
  sessionId, contextVersion, revision: 0, libraries: [], pendingIds: null, items: [], previewItems: [],
  mode: '', catalogMode: '', unavailableReason: '', loading: false, saving: false, listing: false,
  refreshing: false, fromCache: false, catalogUpdatedAt: 0, loaded: false, error: '', listError: '',
  query: '', page: 0, pageSize: 10, total: 0, hasMore: false, maxSelection: 5,
})

interface KnowledgeMutation {
  desiredIds: string[]
  promise: Promise<void>
}

interface InflightList {
  key: string
  promise: Promise<KnowledgePage>
}

export class KnowledgeController {
  private state = emptyState()
  private generation = 0
  private searchGeneration = 0
  private searchTimer: ReturnType<typeof setTimeout> | undefined
  private listeners = new Set<() => void>()
  private mutations = new Map<string, KnowledgeMutation>()
  private accountGeneration = 0
  private knownLibraries = new Map<string, KnowledgeBaseSummary>()
  private confirmedLibraries = new Map<string, KnowledgeBaseSummary>()
  private cache = new KnowledgeCatalogCache()
  private namespace = `ns-${Math.random().toString(36).slice(2)}`
  private catalogOpen = false
  private inflight = new Map<string, InflightList>()
  private refreshPromise: Promise<void> | null = null
  private now: () => number = () => Date.now()
  constructor(private api: KnowledgeAPI, options?: { now?: () => number }) {
    if (options?.now) this.now = options.now
  }
  snapshot = () => this.state
  // Deleted references retain their metadata for editor undo, even after a
  // confirmed deletion or a catalog search has removed them from visible rows.
  visibleLibraries = (): KnowledgeBaseSummary[] => {
    const ids = this.state.pendingIds ?? this.state.libraries.map((library) => library.id)
    return ids.flatMap((id) => {
      // A list row cannot supersede the selected reference's authorized detail.
      const library = this.confirmedLibraries.get(id) ?? this.knownLibraries.get(id)
      return library ? [library] : []
    })
  }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener) } }
  /** 即时选择集合：保存途中以 pending 为准，否则用已确认 libraries。 */
  async toggle(id: string): Promise<void> {
    const ids = effectiveKnowledgeIds(this.state)
    return this.save(ids.includes(id) ? ids.filter((chosen) => chosen !== id) : [...ids, id])
  }
  /** 幂等移除：未选中的 ID 不发请求。 */
  async remove(id: string): Promise<void> {
    const ids = effectiveKnowledgeIds(this.state)
    if (!ids.includes(id)) return
    return this.save(ids.filter((chosen) => chosen !== id))
  }
  private update(patch: Partial<KnowledgeState>) {
    this.state = { ...this.state, ...patch }
    this.listeners.forEach((listener) => listener())
  }
  private adopt(view: SessionKnowledgeView) {
    if (view.sessionId !== this.state.sessionId) throw new Error('知识库会话不匹配，请重试')
    const libraries = (view.libraries ?? []).map(library => {
      if (!library.unavailableReason) return library
      const cached = this.confirmedLibraries.get(library.id) ?? this.knownLibraries.get(library.id)
      // Offline removals return conservative placeholders for retained IDs.
      // Their last title is display-only; no cached authority or count survives.
      return { ...library, title: cached?.title || library.title, directoryUrl: undefined, documentCount: 0, documentCountKnown: false }
    })
    this.remember(libraries)
    libraries.forEach(library => this.confirmedLibraries.set(library.id, library))
    this.update({ revision: view.revision, libraries, mode: view.mode, unavailableReason: view.unavailableReason ?? '', loaded: true })
  }
  private remember(libraries: KnowledgeBaseSummary[]) {
    libraries.forEach((library) => this.knownLibraries.set(library.id, library))
  }
  private rotateNamespace() {
    this.cache.clear()
    this.inflight.clear()
    this.namespace = `ns-${Math.random().toString(36).slice(2)}`
  }
  async activate(sessionId: string): Promise<void> {
    this.cancelSearch()
    this.rotateNamespace()
    const generation = ++this.generation
    ++this.searchGeneration
    this.knownLibraries.clear()
    this.confirmedLibraries.clear()
    this.state = emptyState(sessionId, generation)
    this.update({ loading: !!sessionId })
    if (!sessionId) return
    // Restoring A must read after its earlier mutation settles, even if the user
    // visited B in between. The old response itself remains invalidated.
    const pendingMutation = this.mutations.get(sessionId)
    if (pendingMutation) await pendingMutation.promise
    if (generation !== this.generation) return
    // 先本地快照：重启可立刻显示名称并可取消；再后台实时重验权限。
    if (this.api.snapshot) {
      try {
        const snap = await this.api.snapshot(sessionId)
        if (generation !== this.generation) return
        this.adopt(snap)
        this.update({ loading: true })
      } catch {
        // 无快照时继续走在线读取。
      }
    }
    await this.read(generation)
  }
  private async read(generation: number, opts?: { settleMutation?: boolean }) {
    try {
      const view = await this.api.get(this.state.sessionId)
      if (generation !== this.generation) return
      // 后台重验的迟到结果不得覆盖用户刚完成的取消／改选；保存失败后的重读要放过。
      if (!opts?.settleMutation) {
        if (this.mutations.has(this.state.sessionId)) return
        if (view.revision < this.state.revision) return
      }
      this.adopt(view)
    } catch (error) {
      if (generation !== this.generation) return
      // 本地已恢复时在线失败不清空选择，只提示重验失败。
      if (this.state.loaded && this.state.libraries.length > 0) {
        this.update({ error: message(error) })
      } else {
        this.update({ loaded: false, error: message(error) })
      }
    } finally {
      if (generation === this.generation) this.update({ loading: false })
    }
  }
  async save(ids: string[]): Promise<void> {
    // 本地快照已 loaded 后允许取消／改选；在线重验期间的 loading 不得锁死顶部 ×。
    if (!this.state.sessionId || !this.state.loaded) return
    const unique = [...new Set(ids)]
    if (unique.length > this.state.maxSelection) {
      this.update({ error: `最多选择 ${this.state.maxSelection} 个知识库` })
      return
    }
    const active = this.mutations.get(this.state.sessionId)
    if (active) {
      active.desiredIds = unique
      this.update({ pendingIds: unique, error: '' })
      return active.promise
    }
    if (sameIds(unique, this.state.libraries.map((library) => library.id))) {
      this.update({ error: '' })
      return
    }
    const generation = this.generation
    const sessionId = this.state.sessionId
    const accountGeneration = this.accountGeneration
    const confirmed = { revision: this.state.revision, ids: this.state.libraries.map((library) => library.id) }
    let finish!: () => void
    const mutation: KnowledgeMutation = { desiredIds: unique, promise: new Promise<void>((resolve) => { finish = resolve }) }
    this.mutations.set(sessionId, mutation)
    this.update({ saving: true, pendingIds: unique, error: '' })
    void this.flushMutation(mutation, sessionId, generation, accountGeneration, confirmed).finally(finish)
    return mutation.promise
  }
  private async flushMutation(mutation: KnowledgeMutation, sessionId: string, generation: number, accountGeneration: number, confirmed: { revision: number; ids: string[] }) {
    try {
      while (accountGeneration === this.accountGeneration && !sameIds(mutation.desiredIds, confirmed.ids)) {
        const sentIds = [...mutation.desiredIds]
        const view = await this.api.set({ sessionId, libraryIds: sentIds, expectedRevision: confirmed.revision })
        if (accountGeneration !== this.accountGeneration) return
        if (view.sessionId !== sessionId) throw new Error('知识库会话不匹配，请重试')
        confirmed.revision = view.revision
        confirmed.ids = (view.libraries ?? []).map((library) => library.id)
        // 中间成功也发布真实落盘；若队列里还有更新意图，继续挂着 pending，避免短暂回显旧集合。
        if (generation === this.generation) {
          this.adopt(view)
          if (!sameIds(mutation.desiredIds, confirmed.ids)) this.update({ pendingIds: [...mutation.desiredIds], saving: true })
        }
        if (sameIds(mutation.desiredIds, sentIds)) break
      }
    } catch (error) {
      if (generation !== this.generation) return
      // 失败后读真实落盘：可能已写入（超时），也可能仍是旧集合；不假定回到保存前，也不自动用旧目标续写。
      const text = message(error)
      this.update({ error: text, loading: true, saving: true })
      await this.read(generation, { settleMutation: true })
      // 重读成功也不能吞掉保存失败提示；用户需显式重试。
      if (generation === this.generation) this.update({ error: text })
    } finally {
      if (this.mutations.get(sessionId) === mutation) this.mutations.delete(sessionId)
      if (generation === this.generation) this.update({ saving: false, pendingIds: null })
    }
    // Set 对保留库只返回占位详情；落盘后自动 Get 重验，save 在重验结束后才 resolve，
    // 这样界面不必再点「重新确认知识库」。
    if (generation === this.generation && accountGeneration === this.accountGeneration
      && this.state.sessionId === sessionId
      && this.state.libraries.some((library) => library.unavailableReason)) {
      await this.read(generation)
    }
  }
  /** 打开选择器：命中 30s 内快照则立即展示；过期先展示再后台刷新。 */
  async openCatalog(): Promise<void> {
    if (!this.state.sessionId) return
    this.catalogOpen = true
    clearTimeout(this.searchTimer)
    const query = this.state.query
    const key = catalogKey(this.namespace, this.state.sessionId, query, this.state.pageSize)
    const cached = this.cache.get(key)
    if (cached && cached.pages.length > 0) {
      this.applySnapshot(cached, true)
      const age = this.now() - cached.updatedAt
      if (age >= 0 && age <= CATALOG_FRESH_MS) {
        this.update({ listing: false, refreshing: false, listError: '' })
        return
      }
      await this.refreshCatalog()
      return
    }
    this.update({ listing: true, listError: '', fromCache: false, previewItems: [] })
    await this.fetchPage(1, false)
  }
  /** 手动刷新：无视新鲜期，失败保留成功目录。 */
  async refreshCatalog(): Promise<void> {
    if (!this.state.sessionId || !this.catalogOpen) return
    if (this.refreshPromise) return this.refreshPromise
    const generation = this.generation
    const searchGeneration = this.searchGeneration
    const hadItems = this.state.items.length > 0
    this.update({ refreshing: hadItems, listing: !hadItems, listError: '' })
    this.refreshPromise = (async () => {
      try {
        const result = await this.listPage(1, this.state.query)
        if (generation !== this.generation || searchGeneration !== this.searchGeneration || !this.catalogOpen) return
        if (result.mode === 'unavailable') {
          if (failureClearsCatalog(result)) {
            this.rotateNamespace()
            this.update({ items: [], previewItems: [], page: 0, hasMore: false, fromCache: false, catalogUpdatedAt: 0, catalogMode: result.mode, mode: result.mode, unavailableReason: result.unavailableReason ?? '', listError: result.unavailableReason || '知识库目录暂不可用，请重试' })
            return
          }
          this.update({ listError: '更新失败，显示上次加载的目录', catalogMode: result.mode, unavailableReason: result.unavailableReason ?? '' })
          return
        }
        this.applyPageResult(result, false)
        this.storeCurrentSnapshot()
        this.update({ listError: '', fromCache: false })
      } catch (error) {
        if (generation === this.generation && searchGeneration === this.searchGeneration) {
          this.update({ listError: hadItems ? '更新失败，显示上次加载的目录' : message(error) })
        }
      } finally {
        if (generation === this.generation && searchGeneration === this.searchGeneration) {
          this.update({ refreshing: false, listing: false })
        }
        this.refreshPromise = null
      }
    })()
    return this.refreshPromise
  }
  closeCatalog(): void {
    this.catalogOpen = false
    clearTimeout(this.searchTimer)
    ++this.searchGeneration
    this.update({ listing: false, refreshing: false })
  }
  async search(query: string): Promise<void> {
    if (!this.state.sessionId) return
    clearTimeout(this.searchTimer)
    ++this.searchGeneration
    const preview = query.trim() ? this.cache.previewMatches(this.namespace, this.state.sessionId, query) : []
    this.update({
      query, page: 0, total: 0, hasMore: false, listing: true, listError: '',
      items: [], previewItems: preview, fromCache: false,
    })
    await this.fetchPage(1, false)
  }
  queueSearch(query: string): void {
    this.cancelSearch()
    const preview = query.trim() ? this.cache.previewMatches(this.namespace, this.state.sessionId, query) : []
    this.update({ query, page: 0, items: [], previewItems: preview, hasMore: false, total: -1, listing: true, listError: '' })
    this.searchTimer = setTimeout(() => { void this.search(query) }, 300)
  }
  // Wails 查询没有 AbortSignal 契约：关闭选择器立即作废在途结果，并取消尚未发出的
  // 防抖请求；已发出的原生 HTTP 请求由宿主超时结束，不能宣称浏览器已物理中断它。
  cancelSearch = (): void => {
    this.closeCatalog()
  }
  async loadMore(): Promise<void> {
    if (!this.state.sessionId || this.state.listing || this.state.refreshing || this.state.page === 0 || !this.state.hasMore) return
    this.update({ listing: true, listError: '' })
    await this.fetchPage(this.state.page + 1, true)
  }
  private applySnapshot(snapshot: CatalogSnapshot, fromCache: boolean) {
    const items: KnowledgeBaseSummary[] = []
    for (const page of snapshot.pages) {
      for (const item of page.items ?? []) items.push(item)
    }
    const last = snapshot.pages[snapshot.pages.length - 1]
    this.remember(items)
    this.update({
      items,
      previewItems: [],
      page: last?.page ?? 0,
      pageSize: last?.pageSize ?? this.state.pageSize,
      total: last?.total ?? -1,
      hasMore: last?.hasMore ?? false,
      mode: last?.mode || this.state.mode,
      catalogMode: last?.mode || this.state.catalogMode,
      maxSelection: Math.max(1, Math.min(5, last?.maxSelection || this.state.maxSelection || 5)),
      fromCache,
      catalogUpdatedAt: snapshot.updatedAt,
      unavailableReason: '',
    })
  }
  private storeCurrentSnapshot() {
    if (!this.state.sessionId || this.state.items.length === 0 && this.state.page === 0) return
    const pages = [{
      items: this.state.items,
      page: this.state.page || 1,
      pageSize: this.state.pageSize,
      total: this.state.total,
      hasMore: this.state.hasMore,
      mode: this.state.catalogMode || this.state.mode,
      maxSelection: this.state.maxSelection,
    }]
    const updatedAt = this.now()
    this.cache.put(catalogKey(this.namespace, this.state.sessionId, this.state.query, this.state.pageSize), { pages, updatedAt })
    this.update({ catalogUpdatedAt: updatedAt, fromCache: false })
  }
  private applyPageResult(result: KnowledgePage, append: boolean) {
    const items = append
      ? [...new Map([...this.state.items, ...(result.items ?? [])].map((item) => [item.id, item])).values()]
      : result.items ?? []
    this.remember(items)
    this.update({
      items,
      previewItems: [],
      page: result.page,
      pageSize: result.pageSize,
      total: result.total,
      hasMore: result.hasMore,
      mode: result.mode,
      catalogMode: result.mode,
      unavailableReason: result.unavailableReason ?? '',
      maxSelection: Math.max(1, Math.min(5, result.maxSelection || 5)),
    })
  }
  private async listPage(page: number, query: string): Promise<KnowledgePage> {
    const requestKey = `${this.namespace}|${this.state.sessionId}|${query.trim()}|${this.state.pageSize}|${page}`
    const existing = this.inflight.get(requestKey)
    if (existing) return existing.promise
    const promise = this.api.list({ sessionId: this.state.sessionId, query, page, pageSize: this.state.pageSize })
    this.inflight.set(requestKey, { key: requestKey, promise })
    try {
      return await promise
    } finally {
      this.inflight.delete(requestKey)
    }
  }
  private async fetchPage(page: number, append: boolean): Promise<void> {
    const generation = this.generation
    const searchGeneration = this.searchGeneration
    const query = this.state.query
    try {
      const result = await this.listPage(page, query)
      if (generation !== this.generation || searchGeneration !== this.searchGeneration) return
      if (result.mode === 'unavailable') {
        // The native boundary returns an unavailable envelope for HTTP failures.
        // Keep a successful cursor intact so retry requests the same next page.
        if (!append) {
          if (failureClearsCatalog(result)) {
            this.rotateNamespace()
            this.update({ mode: result.mode, catalogMode: result.mode, unavailableReason: result.unavailableReason ?? '', total: -1, items: [], previewItems: [], fromCache: false })
          } else if (this.state.items.length === 0) {
            this.update({ mode: result.mode, catalogMode: result.mode, unavailableReason: result.unavailableReason ?? '', total: -1 })
          }
        }
        const keepPreview = !append && this.state.previewItems.length > 0
        throw new Error(keepPreview ? '完整搜索失败，当前仅显示已加载目录中的匹配' : (result.unavailableReason || '知识库目录暂不可用，请重试'))
      }
      this.applyPageResult(result, append)
      if (!append || page === 1) this.storeCurrentSnapshot()
      else this.storeCurrentSnapshot()
    } catch (error) {
      if (generation === this.generation && searchGeneration === this.searchGeneration) this.update({ listError: message(error) })
    } finally {
      if (generation === this.generation && searchGeneration === this.searchGeneration) this.update({ listing: false, refreshing: false })
    }
  }
  resetAccount(): Promise<void> {
    ++this.accountGeneration
    this.mutations.clear()
    this.rotateNamespace()
    return this.activate(this.state.sessionId)
  }
  dispose(): void {
    ++this.generation
    this.closeCatalog()
    this.rotateNamespace()
  }
}
function sameIds(left: string[], right: string[]): boolean {
  return left.length === right.length && left.every((id) => right.includes(id))
}
/** 列表勾选／上限／计数的唯一即时来源：pending 优先于已确认 libraries。 */
export function effectiveKnowledgeIds(state: Pick<KnowledgeState, 'pendingIds' | 'libraries'>): string[] {
  return state.pendingIds ? [...state.pendingIds] : state.libraries.map((library) => library.id)
}
function failureClearsCatalog(result: KnowledgePage): boolean {
  const reason = `${result.unavailableReason ?? ''} ${(result as { failureKind?: string }).failureKind ?? ''}`.toLowerCase()
  return reason.includes('authentication') || reason.includes('forbidden') || reason.includes('登录') || reason.includes('无权') || reason.includes('context')
}
export function knowledgeSendBlock(state: KnowledgeState): string {
  if (state.loading) return '正在读取会话知识库，请稍候'
  if (state.saving) return '正在保存知识库选择，请稍候'
  const pending = state.pendingIds
  if (pending && !sameIds(pending, state.libraries.map((library) => library.id))) return '正在保存知识库选择，请稍候'
  if (state.sessionId && !state.loaded) return '知识库选择读取失败，请重试后发送'
  if (state.error) return '知识库选择保存失败，请重试读取后发送'
  // demo 目录不能冒充真实回答；失权库仅提示，不因选库封锁同会话普通／OA／工具发送。
  if (state.libraries.length > 0 && state.mode === 'demo') return '测试数据仅验证选择与保存，暂不参与回答。请先移除知识库后发送；输入草稿会保留。'
  return ''
}

function message(error: unknown): string {
  const value = error instanceof Error ? error.message : String(error)
  try {
    const parsed = JSON.parse(value) as { message?: unknown }
    if (typeof parsed.message === 'string') return parsed.message
  } catch { /* Plain errors are valid bridge failures too. */ }
  return value
}

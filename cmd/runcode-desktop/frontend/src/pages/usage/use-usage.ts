// useUsage 管用量页的数据生命周期：按「身份 + 租户」分代、总览先固定范围再读日志与分组、
// 整套数据一次性替换、第二页以后冻结、可见时 60 秒自动刷新且从不叠加请求。
//
// 所有读取串行发出：Bridge 每个副本的统计并发上限是 2，同一页面并行打四条会被自己挤成
// 503。identityKey 只用来丢弃旧响应，不是授权凭证——身份由 Bridge 按令牌独立校验。
import { useCallback, useEffect, useRef, useState } from 'react'
import {
  getUsageDimensions, getUsageOverview, getUsageRequests,
  type ResolvedUsageQuery, type UsageDimensionRow, type UsageDimensionsPage, type UsageOverview, type UsageRequestsPage,
} from '@/core/bridge'
import { usePersistentBool } from '@/hooks/use-persistent-state'
import { buildUsageQuery, usageFailure, type UsageFailure, type UsageFilters, type UsageRangeSelection } from '@/core/usage'

export const USAGE_AUTO_REFRESH_MS = 60_000
export type UsagePageSize = 20 | 50 | 100
export type UsageGroupBy = 'model' | 'provider'
const DIMENSION_PAGE_SIZE = 50
const OPTION_PAGE_SIZE = 100

// Snapshot 是一次「刷新」得到的整套数据：卡片、趋势、日志、分组与筛选项来自同一个
// effectiveRange，只能整体替换。cursors[i] 是读取第 i+1 页用的游标（第一页为空串）。
interface Snapshot {
  resolved: ResolvedUsageQuery
  overview: UsageOverview
  requests: UsageRequestsPage
  cursors: string[]
  dimensions: UsageDimensionsPage
  dimensionPage: number
  modelOptions: UsageDimensionRow[]
  providerOptions: UsageDimensionRow[]
}

interface LoadParams {
  tenantId: string
  selection: UsageRangeSelection
  filters: UsageFilters
  pageSize: UsagePageSize
  groupBy: UsageGroupBy
}

function resolvedFrom(tenantId: string, overview: UsageOverview, filters: UsageFilters): ResolvedUsageQuery {
  const q: ResolvedUsageQuery = { tenantId, effectiveRange: overview.effectiveRange }
  if (filters.providerId) q.providerId = filters.providerId
  if (filters.model) q.model = filters.model
  return q
}

const withoutFilter = (q: ResolvedUsageQuery, drop: ('model' | 'providerId')[]): ResolvedUsageQuery => {
  const copy = { ...q }
  for (const key of drop) delete copy[key]
  return copy
}

function requestsQuery(resolved: ResolvedUsageQuery, pageSize: UsagePageSize, cursor: string) {
  return cursor ? { query: resolved, pageSize, cursor } : { query: resolved, pageSize }
}

// loadSnapshot 读取一整套数据。模型下拉不应用当前模型筛选、供应商下拉不应用供应商与
// 模型筛选（设计 8.3），只有能复用分组结果时才省掉那一趟。
async function loadSnapshot(p: LoadParams): Promise<Snapshot> {
  const overview = await getUsageOverview(buildUsageQuery(p.tenantId, p.selection, p.filters))
  const resolved = resolvedFrom(p.tenantId, overview, p.filters)
  const providerBreakdown = overview.capabilities.providerBreakdown
  const groupBy: UsageGroupBy = p.groupBy === 'provider' && providerBreakdown ? 'provider' : 'model'
  const requests = await getUsageRequests(requestsQuery(resolved, p.pageSize, ''))
  const dimensions = await getUsageDimensions({ query: resolved, groupBy, page: 1, pageSize: DIMENSION_PAGE_SIZE })
  const modelOptions = groupBy === 'model' && !p.filters.model
    ? dimensions.items ?? []
    : (await getUsageDimensions({ query: withoutFilter(resolved, ['model']), groupBy: 'model', page: 1, pageSize: OPTION_PAGE_SIZE })).items ?? []
  const providerOptions = providerBreakdown
    ? (await getUsageDimensions({ query: withoutFilter(resolved, ['model', 'providerId']), groupBy: 'provider', page: 1, pageSize: OPTION_PAGE_SIZE })).items ?? []
    : []
  return { resolved, overview, requests, cursors: [''], dimensions, dimensionPage: 1, modelOptions, providerOptions }
}

export function useUsage(identityKey: string, tenantId: string) {
  const scopeKey = identityKey && tenantId ? `${identityKey}\u0000${tenantId}` : ''
  const [selection, setSelection] = useState<UsageRangeSelection>({ kind: 'today' })
  // 筛选按「身份 + 租户」记：换租户后旧租户的模型名不能带过去。
  const [filterState, setFilterState] = useState<{ key: string; filters: UsageFilters }>({ key: '', filters: {} })
  const filters = filterState.key === scopeKey ? filterState.filters : {}
  const [pageSize, setPageSizeState] = useState<UsagePageSize>(20)
  const [groupBy, setGroupByState] = useState<UsageGroupBy>('model')
  const [snap, setSnap] = useState<Snapshot | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<UsageFailure | null>(null)
  const [refreshedAt, setRefreshedAt] = useState<Date | null>(null)
  const [hasUpdate, setHasUpdate] = useState(false)
  const [nonce, setNonce] = useState(0)
  const [autoRefresh, toggleAutoRefresh] = usePersistentBool('usage.autoRefresh', true)

  // gen 每次换身份 / 租户 / 范围 / 筛选或手动刷新时加一；返回时代际不符的响应一律丢弃。
  const gen = useRef(0)
  // busy 保证同一时刻只有一趟读取（含自动刷新的探测），不叠加请求。
  const busy = useRef(false)
  const latest = useRef({ snap, tenantId, selection, filters, pageSize, groupBy })
  latest.current = { snap, tenantId, selection, filters, pageSize, groupBy }

  const selectionKey = JSON.stringify(selection)
  const filtersKey = JSON.stringify(filters)

  // 身份或租户一变就清空全部数据并作废在途请求；登出时不再发起任何读取。
  useEffect(() => {
    gen.current++
    busy.current = false
    setSnap(null)
    setError(null)
    setHasUpdate(false)
    setRefreshedAt(null)
    setLoading(false)
  }, [scopeKey])

  useEffect(() => {
    if (!scopeKey) return
    const my = ++gen.current
    const { tenantId: tenant, selection: sel, filters: f, pageSize: size, groupBy: g } = latest.current
    busy.current = true
    setLoading(true)
    loadSnapshot({ tenantId: tenant, selection: sel, filters: f, pageSize: size, groupBy: g })
      .then((next) => {
        if (gen.current !== my) return
        setSnap(next)
        setError(null)
        setHasUpdate(false)
        setRefreshedAt(new Date())
      })
      .catch((e: unknown) => {
        if (gen.current !== my) return
        const failure = usageFailure(e)
        // 账号在读取途中变了：身份那条 effect 会清场，这里不再报错。
        if (failure.kind !== 'identityChanged') setError(failure)
      })
      .finally(() => {
        if (gen.current !== my) return
        busy.current = false
        setLoading(false)
      })
  }, [scopeKey, selectionKey, filtersKey, nonce])

  // partial 在已固定的范围上做一次局部读取（翻页、每页条数、分组）。它不推进代际：换范围
  // 或刷新会让它的结果作废；busy 期间直接忽略，避免并发改同一份快照。
  const partial = useCallback(async (run: (s: Snapshot) => Promise<Snapshot>) => {
    const current = latest.current.snap
    if (!current || busy.current) return
    const my = gen.current
    busy.current = true
    setLoading(true)
    try {
      const next = await run(current)
      if (gen.current !== my) return
      setSnap(next)
      setError(null)
    } catch (e: unknown) {
      if (gen.current !== my) return
      const failure = usageFailure(e)
      if (failure.kind !== 'identityChanged') setError(failure)
    } finally {
      if (gen.current === my) {
        busy.current = false
        setLoading(false)
      }
    }
  }, [])

  const nextPage = useCallback(() => partial(async (s) => {
    if (!s.requests.hasMore || !s.requests.nextCursor) return s
    const cursor = s.requests.nextCursor
    const requests = await getUsageRequests(requestsQuery(s.resolved, latest.current.pageSize, cursor))
    return { ...s, requests, cursors: [...s.cursors, cursor] }
  }), [partial])

  const prevPage = useCallback(() => partial(async (s) => {
    if (s.cursors.length <= 1) return s
    const cursors = s.cursors.slice(0, -1)
    const requests = await getUsageRequests(requestsQuery(s.resolved, latest.current.pageSize, cursors[cursors.length - 1]))
    return { ...s, requests, cursors }
  }), [partial])

  const setPageSize = useCallback((size: UsagePageSize) => {
    setPageSizeState(size)
    void partial(async (s) => {
      const requests = await getUsageRequests(requestsQuery(s.resolved, size, ''))
      return { ...s, requests, cursors: [''] }
    })
  }, [partial])

  const loadDimensions = useCallback((g: UsageGroupBy, page: number) => partial(async (s) => {
    const dimensions = await getUsageDimensions({ query: s.resolved, groupBy: g, page, pageSize: DIMENSION_PAGE_SIZE })
    return { ...s, dimensions, dimensionPage: page }
  }), [partial])

  const setGroupBy = useCallback((g: UsageGroupBy) => {
    setGroupByState(g)
    void loadDimensions(g, 1)
  }, [loadDimensions])

  const setDimensionPage = useCallback((page: number) => {
    void loadDimensions(latest.current.snap?.dimensions.groupBy === 'provider' ? 'provider' : 'model', page)
  }, [loadDimensions])

  const refresh = useCallback(() => setNonce((n) => n + 1), [])

  const applySelection = useCallback((sel: UsageRangeSelection) => {
    setSelection(sel)
    // 选同一个相对范围也要重新固定结束时刻。
    setNonce((n) => n + 1)
  }, [])

  const setModel = useCallback((model: string) => {
    setFilterState({ key: scopeKey, filters: { ...latest.current.filters, model: model || undefined } })
  }, [scopeKey])

  // 供应商一变，旧模型可能不属于新供应商，按设计清掉模型筛选。
  const setProvider = useCallback((providerId: string) => {
    setFilterState({ key: scopeKey, filters: { providerId: providerId || undefined } })
  }, [scopeKey])

  // 第二页以后的自动刷新只探测：拿当前范围重新读一次总览，有变化就亮提示，不替换数据。
  const probe = useCallback(async () => {
    const { snap: s, tenantId: tenant, selection: sel, filters: f } = latest.current
    if (!s || busy.current) return
    const my = gen.current
    busy.current = true
    try {
      const o = await getUsageOverview(buildUsageQuery(tenant, sel, f))
      if (gen.current !== my) return
      const before = s.overview.summary
      if (o.summary.requestCount !== before.requestCount || o.summary.totalTokens !== before.totalTokens) setHasUpdate(true)
    } catch {
      /* 探测失败不打扰：用户仍在看冻结的那一页 */
    } finally {
      if (gen.current === my) busy.current = false
    }
  }, [])

  // 自动刷新：仅在开启、已有身份与租户、窗口可见时计时；每次触发前若有读取在途就跳过，
  // 所以永远不会叠加。刷新成功（refreshedAt 变化）后重新计时。
  useEffect(() => {
    if (!autoRefresh || !scopeKey) return
    let timer: ReturnType<typeof setTimeout> | undefined
    const schedule = () => {
      clearTimeout(timer)
      if (document.visibilityState === 'visible') timer = setTimeout(tick, USAGE_AUTO_REFRESH_MS)
    }
    const tick = () => {
      if (!busy.current) {
        if ((latest.current.snap?.cursors.length ?? 1) > 1) void probe()
        else setNonce((n) => n + 1)
      }
      schedule()
    }
    const onVisibility = () => {
      if (document.visibilityState === 'visible') schedule()
      else clearTimeout(timer)
    }
    schedule()
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [autoRefresh, scopeKey, refreshedAt, probe])

  return {
    selection, setSelection: applySelection,
    filters, setModel, setProvider,
    pageSize, setPageSize,
    groupBy, setGroupBy, setDimensionPage,
    overview: snap?.overview ?? null,
    requests: snap?.requests ?? null,
    requestPage: snap?.cursors.length ?? 0,
    dimensions: snap?.dimensions ?? null,
    dimensionPage: snap?.dimensionPage ?? 1,
    modelOptions: snap?.modelOptions ?? [],
    providerOptions: snap?.providerOptions ?? [],
    loading, error, stale: !!error && !!snap,
    refreshedAt, hasUpdate,
    refresh, nextPage, prevPage,
    autoRefresh, toggleAutoRefresh,
  }
}

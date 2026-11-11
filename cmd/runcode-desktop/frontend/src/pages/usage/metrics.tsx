// 指标卡与数据新鲜度。数值全部来自 Bridge 的合计，不在当前分页里重算；null 显示「—」并
// 说明原因，"0" 才是 0。没有任何成本、价格或货币。
import { type ReactNode } from 'react'
import type { UsageCapabilities, UsageOverview, UsageTotals } from '@/core/bridge'
import { COVERAGE_LABELS, formatCompact, formatDurationMs, formatExact, formatRate, formatShanghai, parseCount } from '@/core/usage'

const CARD = 'bg-surface border border-line2 rounded-card px-4 py-3.5 flex flex-col gap-1 min-w-0 shadow-xs'

function Card({ label, value, exact, sub }: { label: string; value: string; exact?: string; sub: ReactNode }) {
  return (
    <div className={CARD}>
      <div className="text-[12px] text-muted">{label}</div>
      <div className="text-[24px] font-bold tracking-tight text-ink tabular-nums truncate" title={exact && exact !== '—' ? exact : undefined}>{value}</div>
      <div className="text-[12px] text-muted leading-[1.5]">{sub}</div>
    </div>
  )
}

function tokenQuality(t: UsageTotals): string {
  const parts = [COVERAGE_LABELS[t.quality.tokenCoverage] ?? '覆盖情况未知']
  if (t.quality.containsEstimates) parts.push('含估算')
  if (t.quality.containsLegacyUnknown) parts.push('含历史口径')
  return parts.join(' · ')
}

function cacheCard(t: UsageTotals, caps: UsageCapabilities): { value: string; sub: string } {
  if (!caps.cacheMetrics) return { value: '—', sub: '当前数据不支持缓存统计' }
  if (t.cacheHitRate == null) return { value: '—', sub: '缓存信息未知：历史记录里的 0 无法区分未命中与未上报' }
  const coverage = `缓存信息覆盖 ${formatExact(t.cacheKnownRequestCount)}/${formatExact(t.cacheEligibleRequestCount)} 次`
  return { value: formatRate(t.cacheHitRate), sub: t.quality.containsLegacyInferredCache ? `${coverage} · 含历史推导` : coverage }
}

export function UsageMetrics({ totals, capabilities, empty }: {
  totals: UsageTotals
  capabilities: UsageCapabilities
  empty: boolean
}) {
  const cache = cacheCard(totals, capabilities)
  const partialCache = totals.quality.cacheCoverage === 'partial' ? '（已知部分）' : ''
  const invalid = parseCount(totals.quality.invalidRecordCount) ?? 0n
  const more: [string, string][] = [
    ['输入 Token', formatExact(totals.inputTokens)],
    [`缓存命中${partialCache}`, formatExact(totals.cacheHitTokens)],
    [`未命中输入${partialCache}`, formatExact(totals.cacheMissTokens)],
    ['思考 Token', formatExact(totals.reasoningTokens)],
    ['Token 计量请求数', formatExact(totals.tokenRequestCount)],
    ['缓存信息覆盖请求数', `${formatExact(totals.cacheKnownRequestCount)} / ${formatExact(totals.cacheEligibleRequestCount)}`],
    ['平均总耗时', formatDurationMs(totals.averageDurationMs)],
  ]
  return (
    <section aria-label="用量指标" className="@container flex flex-col gap-2">
      <div className="grid grid-cols-1 @md:grid-cols-2 @3xl:grid-cols-4 gap-3">
        <Card label="请求数" value={formatCompact(totals.requestCount)} exact={formatExact(totals.requestCount)} sub="按平台已入账调用计数，可能不含失败或中断请求" />
        <Card
          label="消耗 Token"
          value={formatCompact(totals.totalTokens)}
          exact={formatExact(totals.totalTokens)}
          sub={totals.totalTokens == null ? '暂无可统计的 Token 记录' : tokenQuality(totals)}
        />
        <Card label="输出 Token" value={formatCompact(totals.outputTokens)} exact={formatExact(totals.outputTokens)} sub={`输入 ${formatCompact(totals.inputTokens)}`} />
        <Card label="缓存命中率" value={cache.value} sub={cache.sub} />
      </div>
      {empty && <p className="m-0 text-[12px] text-muted">所选时间段暂无记录。</p>}
      {invalid > 0n && (
        <p className="m-0 text-[12px] text-amberink">{formatExact(totals.quality.invalidRecordCount)} 条记录计量异常，已排除出 Token 与缓存统计。</p>
      )}
      <details className="group">
        <summary className="cursor-pointer select-none text-[13px] text-muted hover:text-ink w-fit">更多指标</summary>
        <dl className="mt-2 grid grid-cols-1 @md:grid-cols-2 @3xl:grid-cols-4 gap-x-6 gap-y-1.5 text-[13px]">
          {more.map(([label, value]) => (
            <div key={label} className="flex justify-between gap-3 border-b border-line py-1">
              <dt className="text-muted">{label}</dt>
              <dd className="m-0 text-ink tabular-nums">{value}</dd>
            </div>
          ))}
        </dl>
      </details>
    </section>
  )
}

const SOURCE_KIND: Record<string, string> = { primary: '主库', replica: '只读副本' }

// UsageFreshness 说明这份数据「什么时候、从哪读的」：dataAsOf 是服务端读取时刻，不是入账
// 水位；副本延迟拿不到时如实写未知，不让人以为刷新后所有调用都已经出现。
export function UsageFreshness({ overview, refreshedAt }: { overview: UsageOverview; refreshedAt: Date | null }) {
  const source = SOURCE_KIND[overview.sourceKind ?? ''] ?? '数据源类型未说明'
  const lag = overview.sourceKind === 'primary'
    ? ''
    : overview.replicaLagSeconds == null ? '副本延迟未知' : `延迟约 ${overview.replicaLagSeconds} 秒`
  const coverage = overview.coverageStatus === 'known' && overview.coverageStart
    ? `记录自 ${formatShanghai(overview.coverageStart)} 起可查询`
    : '覆盖起点未知，仅统计当前可查询的记录'
  return (
    <p className="m-0 text-[12px] text-faint leading-[1.6]">
      数据读取于 {formatShanghai(overview.dataAsOf, true)}（服务端读取时刻，不是入账水位）· {source}{lag ? ` · ${lag}` : ''} · {coverage}
      {refreshedAt && <> · 本机最后刷新 {formatShanghai(refreshedAt.toISOString(), true)}</>}
    </p>
  )
}

import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { UsageFreshness, UsageMetrics } from './metrics'
import { fixtureOverview, fixtureTotals } from './fixtures'

const text = (markup: string) => markup.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ')
const caps = fixtureOverview().capabilities
const COST_WORDS = ['成本', '费用', '价格', '单价', '余额', '¥', '$', '元']

describe('UsageMetrics', () => {
  it('消耗 Token 是输入加输出，缓存与思考不重复相加；精确值在 tooltip 里', () => {
    const markup = renderToStaticMarkup(<UsageMetrics totals={fixtureTotals({ reasoningTokens: '300' })} capabilities={caps} empty={false} />)
    expect(text(markup)).toContain('1.1万')
    expect(markup).toContain('title="11,000"')
    expect(markup).not.toContain('12,700')
    expect(markup).not.toContain('11,300')
    expect(text(markup)).toContain('17.0%')
    expect(text(markup)).toContain('缓存信息覆盖 2/2 次')
  })

  it('null 显示 —，"0" 显示 0；历史缓存 0 绝不显示成已知 0%', () => {
    const totals = fixtureTotals({
      cacheHitTokens: null, cacheMissTokens: null, cacheEligibleInputTokens: null, cacheHitRate: null,
      cacheKnownRequestCount: '0', inputTokens: '0', outputTokens: '0', totalTokens: '0',
      quality: { ...fixtureTotals().quality, cacheCoverage: 'none' },
    })
    const markup = renderToStaticMarkup(<UsageMetrics totals={totals} capabilities={caps} empty={false} />)
    expect(markup).not.toContain('0.0%')
    expect(text(markup)).toContain('缓存命中率 —')
    expect(text(markup)).toContain('缓存信息未知')
    expect(text(markup)).toMatch(/消耗 Token 0/)
  })

  it('部分覆盖与历史口径可见，已知部分不冒充全量', () => {
    const totals = fixtureTotals({
      cacheKnownRequestCount: '1', cacheEligibleRequestCount: '2',
      quality: { tokenCoverage: 'partial', cacheCoverage: 'partial', containsEstimates: true, containsLegacyUnknown: true, containsLegacyInferredCache: true, invalidRecordCount: '3' },
    })
    const t = text(renderToStaticMarkup(<UsageMetrics totals={totals} capabilities={caps} empty={false} />))
    expect(t).toContain('部分记录可统计')
    expect(t).toContain('缓存信息覆盖 1/2 次')
    expect(t).toContain('含估算')
    expect(t).toContain('含历史口径')
    expect(t).toContain('含历史推导')
    expect(t).toContain('已知部分')
    expect(t).toContain('3 条记录计量异常')
  })

  it('不支持缓存统计时说明原因；思考 Token 未知显示 —', () => {
    const t = text(renderToStaticMarkup(<UsageMetrics totals={fixtureTotals({ cacheHitRate: null, cacheHitTokens: null, cacheMissTokens: null, cacheEligibleInputTokens: null })} capabilities={{ ...caps, cacheMetrics: false }} empty={false} />))
    expect(t).toContain('当前数据不支持缓存统计')
    expect(t).toContain('思考 Token —')
  })

  it('空范围：请求与 Token 为 0，命中率 —，并说明暂无记录', () => {
    const totals = fixtureTotals({
      requestCount: '0', tokenRequestCount: '0', inputTokens: '0', outputTokens: '0', totalTokens: '0',
      cacheHitTokens: null, cacheMissTokens: null, cacheEligibleInputTokens: null, cacheHitRate: null,
      cacheKnownRequestCount: '0', cacheEligibleRequestCount: '0', durationSampleCount: '0', averageDurationMs: null,
    })
    const t = text(renderToStaticMarkup(<UsageMetrics totals={totals} capabilities={caps} empty />))
    expect(t).toContain('所选时间段暂无记录')
    expect(t).toContain('缓存命中率 —')
  })

  it('没有任何成本、价格或货币', () => {
    const markup = renderToStaticMarkup(<UsageMetrics totals={fixtureTotals()} capabilities={caps} empty={false} />)
    for (const w of COST_WORDS) expect(text(markup)).not.toContain(w)
  })
})

describe('UsageFreshness', () => {
  it('副本延迟未知如实说明；dataAsOf 是服务端读取时刻，不是本机刷新时刻', () => {
    const t = text(renderToStaticMarkup(<UsageFreshness overview={fixtureOverview()} refreshedAt={new Date('2026-11-10T04:05:00Z')} />))
    expect(t).toContain('副本延迟未知')
    expect(t).toContain('2026-11-10 11:59:30')
    expect(t).toContain('不是入账水位')
    expect(t).toContain('12:05:00')
  })
  it('有延迟值时显示秒数与来源', () => {
    const t = text(renderToStaticMarkup(<UsageFreshness overview={fixtureOverview({ sourceKind: 'replica', replicaLagSeconds: 12 })} refreshedAt={null} />))
    expect(t).toContain('只读副本')
    expect(t).toContain('延迟约 12 秒')
  })
})

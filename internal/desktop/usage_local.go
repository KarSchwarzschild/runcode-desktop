package desktop

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wt68/runcode/internal/localusage"
	"github.com/wt68/runcode/internal/protocol"
)

// 本文件是 S09 本机用量查询层。采集事件已经冻结了账号、Bridge、租户和模型，
// 这里仅按当前查询身份读取 SQLite；不请求 Bridge，也不接受前端传入的账号摘要。

const localUsageMappingVersion = "s09-local-v1"

func validLocalTenant(s string) bool {
	if s == "" || len([]rune(s)) > 128 {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == ':' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func (a *App) localUsageIdentity() (string, error) {
	if a.tokens == nil || !a.tokens.LoggedIn() {
		return "", usageNotLoggedIn()
	}
	issuer, sub := a.tokens.identityParts()
	if issuer == "" || sub == "" {
		return "", usageUnavailable("")
	}
	return digestPair(issuer, sub), nil
}

func (a *App) localUsageScope(tenant string) (localusage.Scope, error) {
	if !validLocalTenant(tenant) {
		return localusage.Scope{}, usageInvalidArgument("请先选择一个租户")
	}
	account, err := a.localUsageIdentity()
	if err != nil {
		return localusage.Scope{}, err
	}
	bridge := passportConfig().BridgeBaseURL
	return localusage.Scope{
		ScopeID:       digestPair(account, digestBridge(bridge)+":"+tenant),
		AccountDigest: account,
		BridgeDigest:  digestBridge(bridge),
		TenantID:      tenant,
	}, nil
}

func localUsageTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

func localUsageRange(q protocol.UsageQuery, now time.Time) (protocol.UsageEffectiveRange, usageInstantRange, error) {
	if q.Timezone != "" && q.Timezone != usageTimezone {
		return protocol.UsageEffectiveRange{}, usageInstantRange{}, usageInvalidArgument("统计时区固定为 Asia/Shanghai")
	}
	loc := usageLoc
	n := now.In(loc)
	end := n
	var start time.Time
	if q.Range != "custom" && (q.StartAt != "" || q.EndAt != "") {
		return protocol.UsageEffectiveRange{}, usageInstantRange{}, usageInvalidArgument("快捷范围不接受起止时间")
	}
	switch q.Range {
	case "today":
		start = time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	case "last24h":
		start = n.Add(-24 * time.Hour)
	case "last7d":
		start = n.AddDate(0, 0, -7)
	case "last14d":
		start = n.AddDate(0, 0, -14)
	case "last30d":
		start = n.AddDate(0, 0, -30)
	case "all":
		start = time.Unix(0, 0).In(loc)
	case "custom":
		var ok bool
		if start, ok = localUsageTime(q.StartAt); !ok {
			return protocol.UsageEffectiveRange{}, usageInstantRange{}, usageInvalidArgument("请填写带时区的开始时间")
		}
		if q.FollowNow {
			if q.EndAt != "" {
				return protocol.UsageEffectiveRange{}, usageInstantRange{}, usageInvalidArgument("跟随当前时刻时不能再指定结束时间")
			}
		} else if end, ok = localUsageTime(q.EndAt); !ok {
			return protocol.UsageEffectiveRange{}, usageInstantRange{}, usageInvalidArgument("请填写带时区的结束时间")
		}
	default:
		return protocol.UsageEffectiveRange{}, usageInstantRange{}, usageInvalidArgument("时间范围无效")
	}
	latest := now.Add(2 * time.Minute)
	if !start.Before(latest) || end.After(latest) {
		return protocol.UsageEffectiveRange{}, usageInstantRange{}, usageInvalidArgument("查询时间不能晚于当前时刻")
	}
	if !start.Before(end) {
		return protocol.UsageEffectiveRange{}, usageInstantRange{}, usageInvalidArgument("开始时间必须早于结束时间")
	}
	r := protocol.UsageEffectiveRange{StartAt: start.UTC().Format(time.RFC3339Nano), EndAt: end.UTC().Format(time.RFC3339Nano), Timezone: usageTimezone}
	return r, usageInstantRange{start: start.UTC(), end: end.UTC()}, nil
}

func localResolvedRange(q protocol.ResolvedUsageQuery) (protocol.UsageEffectiveRange, usageInstantRange, error) {
	r := q.EffectiveRange
	start, ok1 := localUsageTime(r.StartAt)
	end, ok2 := localUsageTime(r.EndAt)
	if !ok1 || !ok2 || !start.Before(end) || (r.Timezone != "" && r.Timezone != usageTimezone) {
		return protocol.UsageEffectiveRange{}, usageInstantRange{}, usageInvalidArgument("查询范围无效，请刷新总览后重试")
	}
	r.Timezone = usageTimezone
	return r, usageInstantRange{start: start.UTC(), end: end.UTC()}, nil
}

func (a *App) localUsageFilter(tenant, model string, r usageInstantRange) (localusage.QueryFilter, error) {
	if model != "" && !validUsageText(model, usageMaxTextLen) {
		return localusage.QueryFilter{}, usageInvalidArgument("模型筛选条件无效")
	}
	scope, err := a.localUsageScope(tenant)
	if err != nil {
		return localusage.QueryFilter{}, err
	}
	return localusage.QueryFilter{ScopeID: scope.ScopeID, StartAt: r.start, EndAt: r.end, Model: model}, nil
}

func localUsageTotals(calls []localusage.CallRecord) protocol.UsageTotals {
	var input, output, total, cacheHit, cacheMiss, cacheEligible int64
	var inputN, outputN, totalN, tokenRequests, tokenCalls, cacheKnown, cacheEligibleRequests, durations, requestCount int64
	var durationSum int64
	for _, c := range calls {
		// 在途 start（尚无 finish）不进请求数：常见于回合末自动标题未完成时刷新用量页，
		// 否则会把一条用户消息显示成两次，并挡住消耗 Token。
		if c.Status == localusage.StatusPending {
			continue
		}
		requestCount++
		isTokenCall := c.CallKind == localusage.CallKindLLM || c.CallKind == localusage.CallKindVision
		if isTokenCall {
			tokenCalls++
		}
		if c.InputTokensKnown && c.InputTokens != nil {
			input += *c.InputTokens
			inputN++
		}
		if c.OutputTokensKnown && c.OutputTokens != nil {
			output += *c.OutputTokens
			outputN++
		}
		if isTokenCall && c.InputTokensKnown && c.OutputTokensKnown && c.InputTokens != nil && c.OutputTokens != nil {
			total += *c.InputTokens + *c.OutputTokens
			totalN++
			tokenRequests++
		}
		if isTokenCall && (c.InputTokensKnown || c.OutputTokensKnown) {
			// tokenRequestCount 表示至少有一项可信 Token 的调用；totalTokens
			// 仍要求同一调用的输入、输出都可信，避免把未知当成 0。
			if !c.InputTokensKnown || !c.OutputTokensKnown {
				tokenRequests++
			}
		}
		if isTokenCall && c.CacheHitTokensKnown && c.CacheMissTokensKnown && c.CacheHitTokens != nil && c.CacheMissTokens != nil {
			cacheHit += *c.CacheHitTokens
			cacheMiss += *c.CacheMissTokens
			cacheEligible += *c.CacheHitTokens + *c.CacheMissTokens
			cacheKnown++
			cacheEligibleRequests++
		}
		if c.DurationMs != nil {
			durations++
			durationSum += *c.DurationMs
		}
	}
	coverage := "none"
	if totalN == tokenCalls && tokenCalls > 0 {
		coverage = "complete"
	} else if totalN > 0 {
		coverage = "partial"
	}
	var inPtr, outPtr, totalPtr *string
	if inputN > 0 {
		v := strconv.FormatInt(input, 10)
		inPtr = &v
	}
	if outputN > 0 {
		v := strconv.FormatInt(output, 10)
		outPtr = &v
	}
	// 有完整 I+O 的调用就展示其合计；未计量的终态调用只把覆盖标成 partial，
	// 不再因为「还有一行未知」把整卡抹成 —。
	if totalN > 0 {
		v := strconv.FormatInt(total, 10)
		totalPtr = &v
	}
	var avg *float64
	if durations > 0 {
		v := float64(durationSum) / float64(durations)
		avg = &v
	}
	var cacheHitPtr, cacheMissPtr, cacheEligiblePtr *string
	var cacheRate *float64
	if cacheKnown > 0 {
		h, m, e := strconv.FormatInt(cacheHit, 10), strconv.FormatInt(cacheMiss, 10), strconv.FormatInt(cacheEligible, 10)
		cacheHitPtr, cacheMissPtr, cacheEligiblePtr = &h, &m, &e
		if cacheEligible > 0 {
			v := float64(cacheHit) / float64(cacheEligible)
			cacheRate = &v
		}
	}
	cacheCoverage := "none"
	if cacheKnown > 0 && cacheKnown == tokenCalls {
		cacheCoverage = "complete"
	} else if cacheKnown > 0 {
		cacheCoverage = "partial"
	}
	return protocol.UsageTotals{
		RequestCount: strconv.FormatInt(requestCount, 10), TokenRequestCount: strconv.FormatInt(tokenRequests, 10),
		InputTokens: inPtr, OutputTokens: outPtr, TotalTokens: totalPtr,
		CacheHitTokens: cacheHitPtr, CacheMissTokens: cacheMissPtr, CacheEligibleInputTokens: cacheEligiblePtr,
		CacheHitRate: cacheRate, CacheKnownRequestCount: strconv.FormatInt(cacheKnown, 10), CacheEligibleRequestCount: strconv.FormatInt(cacheEligibleRequests, 10),
		ReasoningTokens: nil, DurationSampleCount: strconv.FormatInt(durations, 10), AverageDurationMs: avg,
		Quality: protocol.UsageQuality{TokenCoverage: coverage, CacheCoverage: cacheCoverage, InvalidRecordCount: "0"},
	}
}

const usageMaxTrendBuckets = 600

// localUsageGranularity 与前端 bucketGranularity / 设计说明一致：
// ≤48 小时按小时，≤90 天按天，更长按月。
func localUsageGranularity(start, end time.Time) string {
	span := end.Sub(start)
	if span <= 48*time.Hour {
		return "hour"
	}
	if span <= 90*24*time.Hour {
		return "day"
	}
	return "month"
}

func localUsageAlignFloor(t time.Time, g string) time.Time {
	t = t.In(usageLoc)
	switch g {
	case "hour":
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, usageLoc)
	case "day":
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, usageLoc)
	default:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, usageLoc)
	}
}

func localUsageNextBoundary(aligned time.Time, g string) time.Time {
	aligned = aligned.In(usageLoc)
	switch g {
	case "hour":
		return aligned.Add(time.Hour)
	case "day":
		return aligned.AddDate(0, 0, 1)
	default:
		return aligned.AddDate(0, 1, 0)
	}
}

func localUsageBucketFromCalls(start, end time.Time, calls []localusage.CallRecord) protocol.UsageBucket {
	totals := localUsageTotals(calls)
	b := protocol.UsageBucket{
		StartAt: start.UTC().Format(time.RFC3339Nano), EndAt: end.UTC().Format(time.RFC3339Nano),
		RequestCount: totals.RequestCount, InputTokens: totals.InputTokens, OutputTokens: totals.OutputTokens,
		TotalTokens: totals.TotalTokens, CacheHitTokens: totals.CacheHitTokens,
		CacheEligibleInputTokens: totals.CacheEligibleInputTokens, Coverage: "known",
	}
	if len(calls) == 0 {
		// 本机覆盖已知：空桶是真 0 请求；Token 无样本保持 null，不当成 "0"。
		b.RequestCount = "0"
	}
	return b
}

// localUsageTrend 按 startedAt 归入半开区间 [bucketStart, bucketEnd)；首尾可被查询范围裁剪。
func localUsageTrend(start, end time.Time, calls []localusage.CallRecord) ([]protocol.UsageBucket, error) {
	if !start.Before(end) {
		return nil, usageError(protocol.ErrCodeUsageInvalidRange, "开始时间必须早于结束时间")
	}
	g := localUsageGranularity(start, end)
	cur := localUsageAlignFloor(start, g)
	out := make([]protocol.UsageBucket, 0, 32)
	for cur.Before(end) {
		next := localUsageNextBoundary(cur, g)
		bStart, bEnd := cur, next
		if bStart.Before(start) {
			bStart = start
		}
		if bEnd.After(end) {
			bEnd = end
		}
		if bStart.Before(bEnd) {
			var group []localusage.CallRecord
			for _, c := range calls {
				at := c.StartedAt
				if !at.Before(bStart) && at.Before(bEnd) {
					group = append(group, c)
				}
			}
			out = append(out, localUsageBucketFromCalls(bStart, bEnd, group))
			if len(out) > usageMaxTrendBuckets {
				return nil, usageError(protocol.ErrCodeUsageInvalidRange, "趋势分桶超过 600 个，请缩小时间范围")
			}
		}
		cur = next
	}
	return out, nil
}

func localUsageEarliestStart(calls []localusage.CallRecord) (time.Time, bool) {
	var earliest time.Time
	ok := false
	for _, c := range calls {
		if !ok || c.StartedAt.Before(earliest) {
			earliest = c.StartedAt
			ok = true
		}
	}
	return earliest, ok
}

func (a *App) localUsageOverview(q protocol.UsageQuery) (protocol.UsageOverview, error) {
	if q.ProviderID != "" {
		return protocol.UsageOverview{}, usageError(protocol.ErrCodeUsageUnsupported, "本机记录暂不提供供应商筛选")
	}
	r, ir, err := localUsageRange(q, usageNow())
	if err != nil {
		return protocol.UsageOverview{}, err
	}
	f, err := a.localUsageFilter(q.TenantID, q.Model, ir)
	if err != nil {
		return protocol.UsageOverview{}, err
	}
	snap, err := a.usageStore.QuerySnapshot(context.Background(), f)
	if err != nil {
		return protocol.UsageOverview{}, usageUnavailable("")
	}
	// 「全部」从最早调用起算，避免自 Unix 0 按月分桶超过 600。
	if q.Range == "all" {
		if earliest, ok := localUsageEarliestStart(snap.Calls); ok && earliest.After(ir.start) {
			ir.start = earliest.UTC()
			r.StartAt = ir.start.Format(time.RFC3339Nano)
		} else if len(snap.Calls) == 0 {
			return protocol.UsageOverview{
				ScopeID: f.ScopeID, QueryKey: fmt.Sprintf("local:%d", snap.Seq), EffectiveRange: r,
				DataAsOf: usageNow().UTC().Format(time.RFC3339Nano), Summary: localUsageTotals(nil),
				Trend: []protocol.UsageBucket{}, Capabilities: protocol.UsageCapabilities{},
				Empty: true, CoverageStatus: "known", SourceKind: localusage.SourceKindLocalPlatform,
				Consistency: "local_snapshot", MappingVersion: localUsageMappingVersion,
			}, nil
		}
	}
	totals := localUsageTotals(snap.Calls)
	trend, err := localUsageTrend(ir.start, ir.end, snap.Calls)
	if err != nil {
		return protocol.UsageOverview{}, err
	}
	return protocol.UsageOverview{
		ScopeID: f.ScopeID, QueryKey: fmt.Sprintf("local:%d", snap.Seq), EffectiveRange: r,
		DataAsOf: usageNow().UTC().Format(time.RFC3339Nano), Summary: totals,
		Trend:        trend,
		Capabilities: protocol.UsageCapabilities{ProviderBreakdown: false, CacheMetrics: totals.CacheKnownRequestCount != "0", RequestStatus: false, TTFT: false},
		Empty:        len(snap.Calls) == 0, CoverageStatus: "known", SourceKind: localusage.SourceKindLocalPlatform,
		Consistency: "local_snapshot", MappingVersion: localUsageMappingVersion,
	}, nil
}

func localUsageRow(c localusage.CallRecord) protocol.UsageRequestRow {
	started := c.StartedAt.UTC().Format(time.RFC3339Nano)
	recorded := started
	if c.FinishedAt != nil {
		recorded = c.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	row := protocol.UsageRequestRow{ID: c.CallID, RecordedAt: recorded, StartedAt: &started, Model: c.Model,
		MeterKind: "calls", DurationMs: c.DurationMs,
		Quality: protocol.UsageRowQuality{TokenSource: "unavailable", CacheSource: "unavailable", ReasoningSource: "unavailable"}}
	switch c.CallKind {
	case localusage.CallKindLLM, localusage.CallKindVision:
		row.MeterKind = "tokens"
		row.Quality.TokenSource = "reported"
		if c.CacheHitTokensKnown && c.CacheHitTokens != nil && c.CacheMissTokensKnown && c.CacheMissTokens != nil {
			h, m := strconv.FormatInt(*c.CacheHitTokens, 10), strconv.FormatInt(*c.CacheMissTokens, 10)
			row.CacheHitTokens, row.CacheMissTokens = &h, &m
			row.Quality.CacheSource = "reported"
		}
		if c.InputTokensKnown && c.InputTokens != nil {
			v := strconv.FormatInt(*c.InputTokens, 10)
			row.InputTokens = &v
		}
		if c.OutputTokensKnown && c.OutputTokens != nil {
			v := strconv.FormatInt(*c.OutputTokens, 10)
			row.OutputTokens = &v
		}
	case localusage.CallKindImage:
		row.MeterKind = "images"
		if c.GeneratedImages != nil {
			v := strconv.FormatInt(*c.GeneratedImages, 10)
			row.UsageQuantity = &v
			unit := "images"
			row.UsageUnit = &unit
		}
	}
	return row
}

func (a *App) localUsageRequests(q protocol.UsageRequestsQuery) (protocol.UsageRequestsPage, error) {
	if q.Query.ProviderID != "" {
		return protocol.UsageRequestsPage{}, usageError(protocol.ErrCodeUsageUnsupported, "本机记录暂不提供供应商筛选")
	}
	r, ir, err := localResolvedRange(q.Query)
	if err != nil {
		return protocol.UsageRequestsPage{}, err
	}
	f, err := a.localUsageFilter(q.Query.TenantID, q.Query.Model, ir)
	if err != nil {
		return protocol.UsageRequestsPage{}, err
	}
	if q.PageSize != 20 && q.PageSize != 50 && q.PageSize != 100 {
		return protocol.UsageRequestsPage{}, usageInvalidArgument("每页条数只能是 20、50 或 100")
	}
	page, err := a.usageStore.QueryPage(context.Background(), f, q.Cursor, q.PageSize)
	if err != nil {
		if q.Cursor != "" {
			return protocol.UsageRequestsPage{}, usageError(protocol.ErrCodeUsageInvalidCursor, "分页已失效，请回到第一页重新查看")
		}
		return protocol.UsageRequestsPage{}, usageUnavailable("")
	}
	items := make([]protocol.UsageRequestRow, 0, len(page.Calls))
	for _, c := range page.Calls {
		if c.Status == localusage.StatusPending {
			continue
		}
		items = append(items, localUsageRow(c))
	}
	return protocol.UsageRequestsPage{Items: items, TotalCount: strconv.Itoa(page.TotalCount), PageSize: q.PageSize,
		NextCursor: page.NextCursor, HasMore: page.HasMore, EffectiveRange: r, QueryKey: fmt.Sprintf("local:%d", page.Seq)}, nil
}

func (a *App) localUsageDimensions(q protocol.UsageDimensionsQuery) (protocol.UsageDimensionsPage, error) {
	if q.GroupBy != "model" {
		return protocol.UsageDimensionsPage{}, usageError(protocol.ErrCodeUsageUnsupported, "本机记录只支持按模型汇总")
	}
	if q.Query.ProviderID != "" {
		return protocol.UsageDimensionsPage{}, usageError(protocol.ErrCodeUsageUnsupported, "本机记录暂不提供供应商筛选")
	}
	r, ir, err := localResolvedRange(q.Query)
	if err != nil {
		return protocol.UsageDimensionsPage{}, err
	}
	f, err := a.localUsageFilter(q.Query.TenantID, q.Query.Model, ir)
	if err != nil {
		return protocol.UsageDimensionsPage{}, err
	}
	if q.Page < 1 || q.Page > usageMaxDimPage || (q.PageSize != 50 && q.PageSize != 100) || !validUsageText(q.Search, usageMaxSearchLen) {
		return protocol.UsageDimensionsPage{}, usageInvalidArgument("分组分页参数无效")
	}
	snap, err := a.usageStore.QuerySnapshot(context.Background(), f)
	if err != nil {
		return protocol.UsageDimensionsPage{}, usageUnavailable("")
	}
	groups := map[string][]localusage.CallRecord{}
	for _, c := range snap.Calls {
		if q.Search == "" || strings.Contains(strings.ToLower(c.Model), strings.ToLower(q.Search)) {
			groups[c.Model] = append(groups[c.Model], c)
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if len(groups[keys[j]]) > len(groups[keys[i]]) || (len(groups[keys[j]]) == len(groups[keys[i]]) && keys[j] < keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	start := (q.Page - 1) * q.PageSize
	if start > len(keys) {
		start = len(keys)
	}
	end := start + q.PageSize
	if end > len(keys) {
		end = len(keys)
	}
	items := make([]protocol.UsageDimensionRow, 0, end-start)
	for _, k := range keys[start:end] {
		items = append(items, protocol.UsageDimensionRow{ID: k, Name: k, Totals: localUsageTotals(groups[k])})
	}
	return protocol.UsageDimensionsPage{Items: items, GroupBy: q.GroupBy, Page: q.Page, PageSize: q.PageSize,
		TotalCount: strconv.Itoa(len(keys)), HasMore: end < len(keys), EffectiveRange: r, QueryKey: fmt.Sprintf("local:%d", snap.Seq)}, nil
}

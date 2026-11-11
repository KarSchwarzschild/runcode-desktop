package desktop

import (
	"context"
	"math"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/wt68/runcode/internal/protocol"
)

// 用量统计（USAGE-001）的三条只读命令。统计全部在 Bridge 内完成；这里只做三件事：
// 校验并编码查询、按登录代际与时限读取、对返回值做契约校验——不合契约的数据整份
// 丢弃，绝不在客户端补零、重算或截断。

const (
	usageTimezone = "Asia/Shanghai"
	// usageCommandTimeout 是一条命令的总预算，含令牌续期与至多一次 401 重试；
	// Bridge 侧单次统计上限 15 秒，这里多留网络与续期的余量。
	usageCommandTimeout = 20 * time.Second
	usageMaxBuckets     = 600
	usageMaxCursorLen   = 4096
	usageMaxTextLen     = 256
	usageMaxSearchLen   = 100
	usageMaxDimPage     = 100000
	// usageClockSkew 容许本机与 Bridge 的时钟差：自定义结束时间可以略晚于本机「现在」。
	usageClockSkew = 2 * time.Minute
)

// usageNow 在测试里替换成固定时刻。
var usageNow = time.Now

var (
	usageRanges = map[string]bool{"today": true, "last24h": true, "last7d": true, "last14d": true, "last30d": true, "all": true, "custom": true}
	// 覆盖与状态枚举决定界面上「完整 / 部分 / 未知」的说法，不认识的值宁可整份拒绝，
	// 也不能被当成 complete 显示。
	usageCoverages       = map[string]bool{"complete": true, "partial": true, "none": true}
	usageBucketCoverages = map[string]bool{"known": true, "gap": true, "unknown": true}
	usageCoverageStatus  = map[string]bool{"known": true, "unknown": true}
	usageSourceKinds     = map[string]bool{"": true, "primary": true, "replica": true, "unknown": true}
	usageGroupBys        = map[string]bool{"model": true, "provider": true}
	usageTenantPattern   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	usageCursorPattern   = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]+$`)
	usageDecimalPattern  = regexp.MustCompile(`^(0|[1-9][0-9]{0,18})$`)
	usageEnumPattern     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)
	maxInt64             = big.NewInt(math.MaxInt64)
)

// GetUsageOverview 读取当前租户、所选范围的用量汇总、趋势与口径。
func (a *App) GetUsageOverview(req protocol.UsageQuery) (protocol.UsageOverview, error) {
	ctx, cancel := context.WithTimeout(context.Background(), usageCommandTimeout)
	defer cancel()
	out, err := a.usageOverview(ctx, req)
	return out, wireError(err)
}

// GetUsageRequests 读取一页请求日志；范围必须是总览返回的 effectiveRange。
func (a *App) GetUsageRequests(req protocol.UsageRequestsQuery) (protocol.UsageRequestsPage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), usageCommandTimeout)
	defer cancel()
	out, err := a.usageRequests(ctx, req)
	return out, wireError(err)
}

// GetUsageDimensions 读取按模型或供应商分组的汇总，也是历史模型筛选项的来源。
func (a *App) GetUsageDimensions(req protocol.UsageDimensionsQuery) (protocol.UsageDimensionsPage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), usageCommandTimeout)
	defer cancel()
	out, err := a.usageDimensions(ctx, req)
	return out, wireError(err)
}

func (a *App) usageOverview(ctx context.Context, req protocol.UsageQuery) (protocol.UsageOverview, error) {
	values, start, end, err := usageOverviewValues(req, usageNow())
	if err != nil {
		return protocol.UsageOverview{}, err
	}
	var out protocol.UsageOverview
	if err := a.usageGet(ctx, "/api/usage/overview", values, &out); err != nil {
		return protocol.UsageOverview{}, err
	}
	if err := validateUsageOverview(&out, start, end); err != nil {
		return protocol.UsageOverview{}, err
	}
	return out, nil
}

func (a *App) usageRequests(ctx context.Context, req protocol.UsageRequestsQuery) (protocol.UsageRequestsPage, error) {
	values, rng, err := usageResolvedValues(req.Query)
	if err != nil {
		return protocol.UsageRequestsPage{}, err
	}
	if req.PageSize != 20 && req.PageSize != 50 && req.PageSize != 100 {
		return protocol.UsageRequestsPage{}, usageInvalidArgument("每页条数只能是 20、50 或 100")
	}
	if req.Cursor != "" {
		if !validUsageCursor(req.Cursor) {
			return protocol.UsageRequestsPage{}, usageInvalidArgument("分页游标无效，请回到第一页")
		}
		values.Set("cursor", req.Cursor)
	}
	values.Set("pageSize", strconv.Itoa(req.PageSize))
	var out protocol.UsageRequestsPage
	if err := a.usageGet(ctx, "/api/usage/requests", values, &out); err != nil {
		return protocol.UsageRequestsPage{}, err
	}
	if err := validateUsageRequestsPage(&out, req.PageSize, rng); err != nil {
		return protocol.UsageRequestsPage{}, err
	}
	return out, nil
}

func (a *App) usageDimensions(ctx context.Context, req protocol.UsageDimensionsQuery) (protocol.UsageDimensionsPage, error) {
	values, rng, err := usageResolvedValues(req.Query)
	if err != nil {
		return protocol.UsageDimensionsPage{}, err
	}
	if !usageGroupBys[req.GroupBy] {
		return protocol.UsageDimensionsPage{}, usageInvalidArgument("分组只能按模型或供应商")
	}
	if req.PageSize != 50 && req.PageSize != 100 {
		return protocol.UsageDimensionsPage{}, usageInvalidArgument("每页条数只能是 50 或 100")
	}
	if req.Page < 1 || req.Page > usageMaxDimPage {
		return protocol.UsageDimensionsPage{}, usageInvalidArgument("页码无效")
	}
	if !validUsageText(req.Search, usageMaxSearchLen) {
		return protocol.UsageDimensionsPage{}, usageInvalidArgument("搜索内容过长或含控制字符")
	}
	values.Set("groupBy", req.GroupBy)
	if req.Search != "" {
		values.Set("search", req.Search)
	}
	values.Set("page", strconv.Itoa(req.Page))
	values.Set("pageSize", strconv.Itoa(req.PageSize))
	var out protocol.UsageDimensionsPage
	if err := a.usageGet(ctx, "/api/usage/dimensions", values, &out); err != nil {
		return protocol.UsageDimensionsPage{}, err
	}
	if err := validateUsageDimensionsPage(&out, req, rng); err != nil {
		return protocol.UsageDimensionsPage{}, err
	}
	return out, nil
}

// ---- 查询校验与编码 ----------------------------------------------------------

type usageInstantRange struct{ start, end time.Time }

func (r usageInstantRange) contains(t time.Time) bool { return !t.Before(r.start) && t.Before(r.end) }

func validUsageText(s string, maxRunes int) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxRunes {
		return false
	}
	return !strings.ContainsFunc(s, unicode.IsControl)
}

func validUsageCursor(s string) bool {
	return len(s) <= usageMaxCursorLen && usageCursorPattern.MatchString(s)
}

func parseUsageTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

func usageFilterValues(values url.Values, providerID, model string) error {
	if !validUsageText(providerID, usageMaxTextLen) || !validUsageText(model, usageMaxTextLen) {
		return usageInvalidArgument("筛选条件过长或含控制字符")
	}
	if providerID != "" {
		values.Set("providerId", providerID)
	}
	if model != "" {
		values.Set("model", model)
	}
	return nil
}

func usageTenantValue(tenantID string) (string, error) {
	if !usageTenantPattern.MatchString(tenantID) {
		return "", usageInvalidArgument("请先选择一个租户")
	}
	return tenantID, nil
}

// usageOverviewValues 校验总览查询并给出 Bridge 的 query。返回的 start/end 是请求里
// 已经确定的边界（零值表示交给 Bridge 解析），用来核对回来的 effectiveRange。
func usageOverviewValues(q protocol.UsageQuery, now time.Time) (url.Values, time.Time, time.Time, error) {
	var start, end time.Time
	tenant, err := usageTenantValue(q.TenantID)
	if err != nil {
		return nil, start, end, err
	}
	if !usageRanges[q.Range] {
		return nil, start, end, usageInvalidArgument("时间范围无效")
	}
	if q.Timezone != "" && q.Timezone != usageTimezone {
		return nil, start, end, usageInvalidArgument("统计时区固定为 Asia/Shanghai")
	}
	values := url.Values{}
	values.Set("tenantId", tenant)
	values.Set("range", q.Range)
	values.Set("timezone", usageTimezone)
	if q.Range != "custom" {
		if q.StartAt != "" || q.EndAt != "" {
			return nil, start, end, usageInvalidArgument("快捷范围不接受起止时间")
		}
	} else {
		var ok bool
		if start, ok = parseUsageTime(q.StartAt); !ok {
			return nil, start, end, usageInvalidArgument("请填写带时区的开始时间")
		}
		latest := now.Add(usageClockSkew)
		if !start.Before(latest) {
			return nil, start, end, usageInvalidArgument("开始时间不能晚于当前时刻")
		}
		values.Set("startAt", q.StartAt)
		if q.FollowNow {
			if q.EndAt != "" {
				return nil, start, end, usageInvalidArgument("跟随当前时刻时不能再指定结束时间")
			}
			values.Set("followNow", "true")
		} else {
			if end, ok = parseUsageTime(q.EndAt); !ok {
				return nil, start, end, usageInvalidArgument("请填写带时区的结束时间")
			}
			if !start.Before(end) {
				return nil, start, end, usageInvalidArgument("开始时间必须早于结束时间")
			}
			if end.After(latest) {
				return nil, start, end, usageInvalidArgument("结束时间不能晚于当前时刻")
			}
			values.Set("endAt", q.EndAt)
		}
	}
	if err := usageFilterValues(values, q.ProviderID, q.Model); err != nil {
		return nil, start, end, err
	}
	return values, start, end, nil
}

func parseUsageRange(r protocol.UsageEffectiveRange) (usageInstantRange, bool) {
	start, ok1 := parseUsageTime(r.StartAt)
	end, ok2 := parseUsageTime(r.EndAt)
	if !ok1 || !ok2 || !start.Before(end) || r.Timezone != usageTimezone {
		return usageInstantRange{}, false
	}
	return usageInstantRange{start: start, end: end}, true
}

// usageResolvedValues 编码日志 / 分组共用的已解析查询：只有总览固定下来的半开区间，
// 不再带相对范围名。
func usageResolvedValues(q protocol.ResolvedUsageQuery) (url.Values, usageInstantRange, error) {
	tenant, err := usageTenantValue(q.TenantID)
	if err != nil {
		return nil, usageInstantRange{}, err
	}
	rng, ok := parseUsageRange(q.EffectiveRange)
	if !ok {
		return nil, usageInstantRange{}, usageInvalidArgument("查询范围无效，请刷新总览后重试")
	}
	values := url.Values{}
	values.Set("tenantId", tenant)
	values.Set("startAt", q.EffectiveRange.StartAt)
	values.Set("endAt", q.EffectiveRange.EndAt)
	values.Set("timezone", usageTimezone)
	if err := usageFilterValues(values, q.ProviderID, q.Model); err != nil {
		return nil, usageInstantRange{}, err
	}
	return values, rng, nil
}

// ---- 响应校验 ------------------------------------------------------------------

func usageDecimal(s string) (*big.Int, bool) {
	if !usageDecimalPattern.MatchString(s) {
		return nil, false
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok || v.Cmp(maxInt64) > 0 {
		return nil, false
	}
	return v, true
}

// usageNullable 校验可空的十进制字段；第二个返回值为 false 表示格式非法。
func usageNullable(s *string) (*big.Int, bool) {
	if s == nil {
		return nil, true
	}
	return usageDecimal(*s)
}

func usageRatioOK(v *float64) bool {
	return v == nil || (!math.IsNaN(*v) && *v >= 0 && *v <= 1)
}

// usageTokenTriple 核对 total = input + output；缓存是输入子集，不参与总量。
func usageTokenTriple(in, out, total *big.Int) bool {
	if in != nil && out != nil {
		return total != nil && total.Cmp(new(big.Int).Add(in, out)) == 0
	}
	return true
}

// usageCacheTriple 核对 命中 ≤ 可信输入，且 未命中 = 可信输入 − 命中。
func usageCacheTriple(hit, miss, eligible *big.Int) bool {
	if hit != nil && eligible != nil && hit.Cmp(eligible) > 0 {
		return false
	}
	if hit != nil && miss != nil && eligible != nil {
		return miss.Cmp(new(big.Int).Sub(eligible, hit)) == 0
	}
	return true
}

func validateUsageQuality(q protocol.UsageQuality) bool {
	_, ok := usageDecimal(q.InvalidRecordCount)
	return ok && usageCoverages[q.TokenCoverage] && usageCoverages[q.CacheCoverage]
}

func validateUsageTotals(t protocol.UsageTotals) bool {
	requests, ok1 := usageDecimal(t.RequestCount)
	tokenRequests, ok2 := usageDecimal(t.TokenRequestCount)
	cacheKnown, ok3 := usageDecimal(t.CacheKnownRequestCount)
	cacheEligible, ok4 := usageDecimal(t.CacheEligibleRequestCount)
	samples, ok5 := usageDecimal(t.DurationSampleCount)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !validateUsageQuality(t.Quality) {
		return false
	}
	// K ⊆ T ⊆ 全部请求。
	if tokenRequests.Cmp(requests) > 0 || cacheEligible.Cmp(tokenRequests) > 0 || cacheKnown.Cmp(cacheEligible) > 0 {
		return false
	}
	in, okIn := usageNullable(t.InputTokens)
	out, okOut := usageNullable(t.OutputTokens)
	total, okTotal := usageNullable(t.TotalTokens)
	hit, okHit := usageNullable(t.CacheHitTokens)
	miss, okMiss := usageNullable(t.CacheMissTokens)
	eligible, okEligible := usageNullable(t.CacheEligibleInputTokens)
	_, okReasoning := usageNullable(t.ReasoningTokens)
	if !okIn || !okOut || !okTotal || !okHit || !okMiss || !okEligible || !okReasoning {
		return false
	}
	if !usageTokenTriple(in, out, total) || !usageCacheTriple(hit, miss, eligible) || !usageRatioOK(t.CacheHitRate) {
		return false
	}
	// 命中率必须是加权比例 SUM(命中)/SUM(可信输入)；分母未知或为 0 时只能是 null。
	if eligible == nil || eligible.Sign() == 0 || hit == nil {
		if t.CacheHitRate != nil {
			return false
		}
	} else {
		want, _ := new(big.Rat).SetFrac(hit, eligible).Float64()
		if t.CacheHitRate == nil || math.Abs(*t.CacheHitRate-want) > 1e-6 {
			return false
		}
	}
	if t.AverageDurationMs != nil && (math.IsNaN(*t.AverageDurationMs) || math.IsInf(*t.AverageDurationMs, 0) || *t.AverageDurationMs < 0 || samples.Sign() == 0) {
		return false
	}
	return true
}

func validateUsageBucket(b protocol.UsageBucket) (usageInstantRange, bool) {
	start, ok1 := parseUsageTime(b.StartAt)
	end, ok2 := parseUsageTime(b.EndAt)
	if !ok1 || !ok2 || !start.Before(end) || !usageBucketCoverages[b.Coverage] {
		return usageInstantRange{}, false
	}
	_, okCount := usageDecimal(b.RequestCount)
	in, okIn := usageNullable(b.InputTokens)
	out, okOut := usageNullable(b.OutputTokens)
	total, okTotal := usageNullable(b.TotalTokens)
	hit, okHit := usageNullable(b.CacheHitTokens)
	eligible, okEligible := usageNullable(b.CacheEligibleInputTokens)
	if !okCount || !okIn || !okOut || !okTotal || !okHit || !okEligible {
		return usageInstantRange{}, false
	}
	if !usageTokenTriple(in, out, total) || !usageCacheTriple(hit, nil, eligible) {
		return usageInstantRange{}, false
	}
	return usageInstantRange{start: start, end: end}, true
}

func validUsageKey(s string) bool { return s != "" && validUsageText(s, usageMaxTextLen) }

// validateUsageOverview 核对总览：范围与请求一致、趋势桶首尾相接铺满范围、合计自洽。
// start/end 非零时是请求里已经确定的边界，返回的范围必须与之相同。
func validateUsageOverview(o *protocol.UsageOverview, start, end time.Time) error {
	rng, ok := parseUsageRange(o.EffectiveRange)
	if !ok || (!start.IsZero() && !rng.start.Equal(start)) || (!end.IsZero() && !rng.end.Equal(end)) {
		return usageInvalidResponse()
	}
	if _, ok := parseUsageTime(o.DataAsOf); !ok || !validUsageKey(o.ScopeID) || !validUsageKey(o.QueryKey) {
		return usageInvalidResponse()
	}
	if !validateUsageTotals(o.Summary) || !usageCoverageStatus[o.CoverageStatus] || !usageSourceKinds[o.SourceKind] {
		return usageInvalidResponse()
	}
	if o.Empty && o.Summary.RequestCount != "0" {
		return usageInvalidResponse()
	}
	if o.ReplicaLagSeconds != nil && *o.ReplicaLagSeconds < 0 {
		return usageInvalidResponse()
	}
	if o.CoverageStart != nil {
		if _, ok := parseUsageTime(*o.CoverageStart); !ok {
			return usageInvalidResponse()
		}
	}
	for _, gap := range o.CoverageGaps {
		gs, ok1 := parseUsageTime(gap.StartAt)
		ge, ok2 := parseUsageTime(gap.EndAt)
		if !ok1 || !ok2 || !gs.Before(ge) {
			return usageInvalidResponse()
		}
	}
	if len(o.Trend) > usageMaxBuckets {
		return usageInvalidResponse()
	}
	cursor := rng.start
	for _, b := range o.Trend {
		br, ok := validateUsageBucket(b)
		if !ok || !br.start.Equal(cursor) || br.end.After(rng.end) {
			return usageInvalidResponse()
		}
		cursor = br.end
	}
	if len(o.Trend) > 0 && !cursor.Equal(rng.end) {
		return usageInvalidResponse()
	}
	if o.Trend == nil {
		o.Trend = []protocol.UsageBucket{}
	}
	if o.CoverageGaps == nil {
		o.CoverageGaps = []protocol.UsageCoverageGap{}
	}
	return nil
}

func validUsageEnum(s string) bool { return s == "" || usageEnumPattern.MatchString(s) }

func validateUsageRow(r protocol.UsageRequestRow, rng usageInstantRange) (time.Time, bool) {
	recorded, ok := parseUsageTime(r.RecordedAt)
	if !ok || !rng.contains(recorded) || !validUsageKey(r.ID) || !validUsageText(r.Model, usageMaxTextLen) {
		return recorded, false
	}
	if r.StartedAt != nil {
		if _, ok := parseUsageTime(*r.StartedAt); !ok {
			return recorded, false
		}
	}
	for _, s := range []*string{r.ProviderID, r.ProviderName, r.UsageUnit} {
		if s != nil && !validUsageText(*s, usageMaxTextLen) {
			return recorded, false
		}
	}
	if !usageEnumPattern.MatchString(r.MeterKind) || !validUsageEnum(r.Quality.TokenSource) || !validUsageEnum(r.Quality.CacheSource) || !validUsageEnum(r.Quality.ReasoningSource) {
		return recorded, false
	}
	_, okQty := usageNullable(r.UsageQuantity)
	in, okIn := usageNullable(r.InputTokens)
	_, okOut := usageNullable(r.OutputTokens)
	hit, okHit := usageNullable(r.CacheHitTokens)
	miss, okMiss := usageNullable(r.CacheMissTokens)
	_, okReasoning := usageNullable(r.ReasoningTokens)
	if !okQty || !okIn || !okOut || !okHit || !okMiss || !okReasoning || !usageCacheTriple(hit, miss, in) {
		return recorded, false
	}
	if r.DurationMs != nil && *r.DurationMs < 0 {
		return recorded, false
	}
	return recorded, true
}

// validateUsageRequestsPage 核对一页日志：范围与请求完全一致、行在范围内且按入账时间
// 降序、ID 不重复、有下一页就必须带游标。
func validateUsageRequestsPage(p *protocol.UsageRequestsPage, pageSize int, rng usageInstantRange) error {
	got, ok := parseUsageRange(p.EffectiveRange)
	if !ok || !got.start.Equal(rng.start) || !got.end.Equal(rng.end) || p.PageSize != pageSize || len(p.Items) > pageSize {
		return usageInvalidResponse()
	}
	if _, ok := usageDecimal(p.TotalCount); !ok || !validUsageKey(p.QueryKey) {
		return usageInvalidResponse()
	}
	if p.HasMore != (p.NextCursor != "") || (p.NextCursor != "" && !validUsageCursor(p.NextCursor)) {
		return usageInvalidResponse()
	}
	seen := make(map[string]bool, len(p.Items))
	var previous time.Time
	for i, row := range p.Items {
		recorded, ok := validateUsageRow(row, rng)
		if !ok || seen[row.ID] || (i > 0 && recorded.After(previous)) {
			return usageInvalidResponse()
		}
		seen[row.ID] = true
		previous = recorded
	}
	if p.Items == nil {
		p.Items = []protocol.UsageRequestRow{}
	}
	return nil
}

func validateUsageDimensionsPage(p *protocol.UsageDimensionsPage, req protocol.UsageDimensionsQuery, rng usageInstantRange) error {
	got, ok := parseUsageRange(p.EffectiveRange)
	if !ok || !got.start.Equal(rng.start) || !got.end.Equal(rng.end) {
		return usageInvalidResponse()
	}
	if p.GroupBy != req.GroupBy || p.Page != req.Page || p.PageSize != req.PageSize || len(p.Items) > req.PageSize {
		return usageInvalidResponse()
	}
	if _, ok := usageDecimal(p.TotalCount); !ok || !validUsageKey(p.QueryKey) {
		return usageInvalidResponse()
	}
	seen := make(map[string]bool, len(p.Items))
	for _, row := range p.Items {
		if !validUsageKey(row.ID) || seen[row.ID] || !validUsageText(row.Name, usageMaxTextLen) || !validateUsageTotals(row.Totals) {
			return usageInvalidResponse()
		}
		seen[row.ID] = true
	}
	if p.Items == nil {
		p.Items = []protocol.UsageDimensionRow{}
	}
	return nil
}

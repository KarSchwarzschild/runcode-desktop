package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wt68/runcode/internal/protocol"
	hostproto "gitlab.ouc-online.com.cn/aibase/agentloop/protocol"
)

// 合成数据：上海当天 00:00—12:00，与设计第 4 节的校验例同口径（11000 / 1700 / 8300 / 17%）。
const (
	usageFixtureStart = "2026-11-10T00:00:00+08:00"
	usageFixtureMid   = "2026-11-10T06:00:00+08:00"
	usageFixtureEnd   = "2026-11-10T12:00:00+08:00"
)

func usageFixtureNow(t *testing.T) {
	t.Helper()
	old := usageNow
	usageNow = func() time.Time { return time.Date(2026, 11, 10, 4, 0, 30, 0, time.UTC) }
	t.Cleanup(func() { usageNow = old })
}

func usageTotalsFixture() map[string]any {
	return map[string]any{
		"requestCount": "2", "tokenRequestCount": "2",
		"inputTokens": "10000", "outputTokens": "1000", "totalTokens": "11000",
		"cacheHitTokens": "1700", "cacheMissTokens": "8300", "cacheEligibleInputTokens": "10000",
		"cacheHitRate": 0.17, "cacheKnownRequestCount": "2", "cacheEligibleRequestCount": "2",
		"reasoningTokens": nil, "durationSampleCount": "2", "averageDurationMs": 1500.0,
		"quality": map[string]any{
			"tokenCoverage": "complete", "cacheCoverage": "complete", "containsEstimates": false,
			"containsLegacyUnknown": true, "containsLegacyInferredCache": true, "invalidRecordCount": "0",
		},
	}
}

func usageOverviewFixture() map[string]any {
	return map[string]any{
		"scopeId": "scope-a", "queryKey": "qk-1",
		"effectiveRange": map[string]any{"startAt": usageFixtureStart, "endAt": usageFixtureEnd, "timezone": "Asia/Shanghai"},
		"dataAsOf":       "2026-11-10T04:00:29Z",
		"summary":        usageTotalsFixture(),
		"trend": []any{
			map[string]any{"startAt": usageFixtureStart, "endAt": usageFixtureMid, "requestCount": "0", "inputTokens": "0", "outputTokens": "0", "totalTokens": "0", "cacheHitTokens": nil, "cacheEligibleInputTokens": nil, "coverage": "known"},
			map[string]any{"startAt": "2026-11-09T22:00:00Z", "endAt": usageFixtureEnd, "requestCount": "2", "inputTokens": "10000", "outputTokens": "1000", "totalTokens": "11000", "cacheHitTokens": "1700", "cacheEligibleInputTokens": "10000", "coverage": "known"},
		},
		"capabilities":      map[string]any{"providerBreakdown": false, "cacheMetrics": true, "requestStatus": false, "ttft": false},
		"empty":             false,
		"coverageStart":     nil,
		"coverageGaps":      []any{},
		"coverageStatus":    "unknown",
		"replicaLagSeconds": nil,
		"futureField":       "ignored by older clients",
	}
}

func usageResolvedFixture() protocol.ResolvedUsageQuery {
	return protocol.ResolvedUsageQuery{
		TenantID:       "tenant-a",
		EffectiveRange: protocol.UsageEffectiveRange{StartAt: usageFixtureStart, EndAt: usageFixtureEnd, Timezone: "Asia/Shanghai"},
	}
}

func usageRowFixture(id, recordedAt string) map[string]any {
	return map[string]any{
		"id": id, "recordedAt": recordedAt, "startedAt": nil, "model": "qwen-max",
		"providerId": nil, "providerName": nil, "meterKind": "tokens", "usageQuantity": nil, "usageUnit": nil,
		"inputTokens": "1000", "outputTokens": "200", "cacheHitTokens": "800", "cacheMissTokens": "200",
		"reasoningTokens": nil, "durationMs": 1200,
		"quality": map[string]any{"tokenSource": "legacyUnknown", "cacheSource": "legacyInferred", "reasoningSource": "unavailable"},
	}
}

func usageRequestsFixture() map[string]any {
	return map[string]any{
		"items": []any{
			usageRowFixture("b", "2026-11-10T03:00:00Z"),
			usageRowFixture("a", "2026-11-10T01:00:00Z"),
		},
		"totalCount": "21", "pageSize": 20, "nextCursor": "c1.sig", "hasMore": true,
		"effectiveRange": map[string]any{"startAt": "2026-11-09T16:00:00Z", "endAt": "2026-11-10T04:00:00Z", "timezone": "Asia/Shanghai"},
		"queryKey":       "qk-1",
	}
}

func usageDimensionsFixture() map[string]any {
	return map[string]any{
		"items":   []any{map[string]any{"id": "qwen-max", "name": "qwen-max", "totals": usageTotalsFixture()}},
		"groupBy": "model", "page": 1, "pageSize": 50, "totalCount": "1", "hasMore": false,
		"effectiveRange": map[string]any{"startAt": usageFixtureStart, "endAt": usageFixtureEnd, "timezone": "Asia/Shanghai"},
		"queryKey":       "qk-1",
	}
}

type usageBridge struct {
	t        *testing.T
	server   *httptest.Server
	gets     atomic.Int32
	refresh  atomic.Int32
	handler  func(w http.ResponseWriter, r *http.Request)
	lastPath atomic.Value
	lastAuth atomic.Value
	lastKeys atomic.Value
}

func newUsageApp(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*App, *usageBridge) {
	t.Helper()
	isolateConfigDir(t)
	usageFixtureNow(t)
	b := &usageBridge{t: t, handler: handler}
	b.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			b.refresh.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"fresh-token","refresh_token":"refresh-2","expires_in":3600}`))
			return
		}
		b.gets.Add(1)
		b.lastPath.Store(r.URL.Path)
		b.lastAuth.Store(r.Header.Get("Authorization"))
		keys := make([]string, 0, len(r.URL.Query()))
		for k := range r.URL.Query() {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.lastKeys.Store(strings.Join(keys, ","))
		b.handler(w, r)
	}))
	t.Cleanup(b.server.Close)
	t.Setenv("RUNCODE_BRIDGE_BASE_URL", b.server.URL)
	app := New(&recordingSink{})
	app.tokens = newTokenManager(b.server.URL+"/token", "fixture", b.server.Client(), nil)
	app.tokens.setInMemory(tokenSet{AccessToken: "old-token", RefreshToken: "refresh-1", Expiry: time.Now().Add(time.Hour)})
	return app, b
}

func writeUsageJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func usageErrCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got success")
	}
	var pe *hostproto.Error
	if !errors.As(err, &pe) {
		t.Fatalf("error is not a structured protocol error: %v", err)
	}
	return pe.Code
}

func todayQuery() protocol.UsageQuery {
	return protocol.UsageQuery{TenantID: "tenant-a", Range: "today", Timezone: "Asia/Shanghai"}
}

func TestUsageAuthenticatedGet(t *testing.T) {
	app, b := newUsageApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer old-token" {
			writeUsageJSON(w, http.StatusUnauthorized, map[string]any{"code": "unauthorized", "message": "expired"})
			return
		}
		writeUsageJSON(w, http.StatusOK, usageOverviewFixture())
	})
	got, err := app.GetUsageOverview(todayQuery())
	if err != nil {
		t.Fatal(err)
	}
	if b.gets.Load() != 2 || b.refresh.Load() != 1 {
		t.Fatalf("gets=%d refreshes=%d, want one safe retry after one refresh", b.gets.Load(), b.refresh.Load())
	}
	if b.lastPath.Load() != "/api/usage/overview" || b.lastAuth.Load() != "Bearer fresh-token" {
		t.Fatalf("path=%v auth=%v", b.lastPath.Load(), b.lastAuth.Load())
	}
	if keys := b.lastKeys.Load(); keys != "range,tenantId,timezone" {
		t.Fatalf("query keys = %v; identity/scope must never be client supplied", keys)
	}
	if got.Summary.TotalTokens == nil || *got.Summary.TotalTokens != "11000" || got.Summary.ReasoningTokens != nil {
		t.Fatalf("summary = %+v", got.Summary)
	}
	if got.Summary.CacheHitRate == nil || *got.Summary.CacheHitRate != 0.17 {
		t.Fatalf("rate = %v", got.Summary.CacheHitRate)
	}
	if got.CoverageGaps == nil || len(got.Trend) != 2 {
		t.Fatalf("slices must be normalized: gaps=%v trend=%d", got.CoverageGaps, len(got.Trend))
	}

	// A second 401 after the forced refresh is final: no third request, no loop.
	app2, b2 := newUsageApp(t, func(w http.ResponseWriter, _ *http.Request) {
		writeUsageJSON(w, http.StatusUnauthorized, map[string]any{"code": "unauthorized"})
	})
	_, err = app2.GetUsageOverview(todayQuery())
	if code := usageErrCode(t, err); code != hostproto.ErrCodeNotLoggedIn {
		t.Fatalf("code = %s", code)
	}
	if b2.gets.Load() != 2 || b2.refresh.Load() != 1 {
		t.Fatalf("gets=%d refreshes=%d", b2.gets.Load(), b2.refresh.Load())
	}

	// Logged out: fail before touching the network.
	app3, b3 := newUsageApp(t, func(w http.ResponseWriter, _ *http.Request) { writeUsageJSON(w, 200, usageOverviewFixture()) })
	app3.tokens.Clear()
	_, err = app3.GetUsageOverview(todayQuery())
	if code := usageErrCode(t, err); code != hostproto.ErrCodeNotLoggedIn || b3.gets.Load() != 0 {
		t.Fatalf("code=%s gets=%d", code, b3.gets.Load())
	}
}

func TestUsageIdentityEpoch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*App)
	}{
		{"switch account", func(a *App) {
			a.tokens.setInMemory(tokenSet{AccessToken: "other-account", Expiry: time.Now().Add(time.Hour)})
		}},
		{"logout", func(a *App) { a.tokens.Clear() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			arrived := make(chan struct{})
			release := make(chan struct{})
			app, _ := newUsageApp(t, func(w http.ResponseWriter, _ *http.Request) {
				close(arrived)
				<-release
				writeUsageJSON(w, http.StatusOK, usageOverviewFixture())
			})
			done := make(chan error, 1)
			go func() {
				_, err := app.GetUsageOverview(todayQuery())
				done <- err
			}()
			<-arrived
			tc.change(app)
			close(release)
			if code := usageErrCode(t, <-done); code != protocol.ErrCodeUsageIdentityChanged {
				t.Fatalf("stale response was accepted: code=%s", code)
			}
		})
	}
}

func TestUsageResponseValidation(t *testing.T) {
	mutate := func(f func(map[string]any)) func() any {
		return func() any { v := usageOverviewFixture(); f(v); return v }
	}
	summary := func(v map[string]any) map[string]any { return v["summary"].(map[string]any) }
	cases := []struct {
		name string
		body func() any
	}{
		{"negative count", mutate(func(v map[string]any) { summary(v)["requestCount"] = "-1" })},
		{"fractional count", mutate(func(v map[string]any) { summary(v)["requestCount"] = "1.5" })},
		{"numeric count", mutate(func(v map[string]any) { summary(v)["requestCount"] = 2 })},
		{"leading zero", mutate(func(v map[string]any) { summary(v)["inputTokens"] = "010000" })},
		{"exceeds int64", mutate(func(v map[string]any) { summary(v)["outputTokens"] = "9223372036854775808" })},
		{"total is not input plus output", mutate(func(v map[string]any) { summary(v)["totalTokens"] = "12700" })},
		{"hit exceeds eligible input", mutate(func(v map[string]any) { summary(v)["cacheHitTokens"] = "10001" })},
		{"miss is not eligible minus hit", mutate(func(v map[string]any) { summary(v)["cacheMissTokens"] = "8299" })},
		{"rate out of range", mutate(func(v map[string]any) { summary(v)["cacheHitRate"] = 1.5 })},
		{"averaged rate instead of weighted", mutate(func(v map[string]any) { summary(v)["cacheHitRate"] = 0.45 })},
		{"rate without eligible input", mutate(func(v map[string]any) {
			s := summary(v)
			s["cacheHitTokens"], s["cacheMissTokens"], s["cacheEligibleInputTokens"] = nil, nil, nil
		})},
		{"unknown coverage", mutate(func(v map[string]any) { summary(v)["quality"].(map[string]any)["tokenCoverage"] = "mostly" })},
		{"wrong timezone", mutate(func(v map[string]any) { v["effectiveRange"].(map[string]any)["timezone"] = "UTC" })},
		{"inverted range", mutate(func(v map[string]any) {
			v["effectiveRange"] = map[string]any{"startAt": usageFixtureEnd, "endAt": usageFixtureStart, "timezone": "Asia/Shanghai"}
		})},
		{"trend does not tile range", mutate(func(v map[string]any) { v["trend"] = v["trend"].([]any)[1:] })},
		{"too many buckets", mutate(func(v map[string]any) {
			buckets := make([]any, 601)
			start := time.Date(2026, 11, 9, 16, 0, 0, 0, time.UTC)
			for i := range buckets {
				buckets[i] = map[string]any{"startAt": start.Add(time.Duration(i) * time.Minute).Format(time.RFC3339), "endAt": start.Add(time.Duration(i+1) * time.Minute).Format(time.RFC3339), "requestCount": "0", "inputTokens": nil, "outputTokens": nil, "totalTokens": nil, "cacheHitTokens": nil, "cacheEligibleInputTokens": nil, "coverage": "unknown"}
			}
			v["trend"] = buckets
		})},
		{"empty with records", mutate(func(v map[string]any) { v["empty"] = true })},
		{"missing query key", mutate(func(v map[string]any) { v["queryKey"] = "" })},
		{"not json", func() any { return json.RawMessage(`<html>`) }},
		{"over 4 MiB", func() any { return map[string]any{"pad": strings.Repeat("x", 4<<20)} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := newUsageApp(t, func(w http.ResponseWriter, _ *http.Request) { writeUsageJSON(w, http.StatusOK, tc.body()) })
			_, err := app.GetUsageOverview(todayQuery())
			if code := usageErrCode(t, err); code != protocol.ErrCodeUsageInvalidResponse {
				t.Fatalf("code = %s", code)
			}
		})
	}

	t.Run("null stays null and zero stays zero", func(t *testing.T) {
		body := usageOverviewFixture()
		s := summary(body)
		s["requestCount"], s["tokenRequestCount"], s["cacheKnownRequestCount"], s["cacheEligibleRequestCount"] = "0", "0", "0", "0"
		s["inputTokens"], s["outputTokens"], s["totalTokens"] = "0", "0", "0"
		s["cacheHitTokens"], s["cacheMissTokens"], s["cacheEligibleInputTokens"], s["cacheHitRate"] = nil, nil, nil, nil
		s["durationSampleCount"], s["averageDurationMs"] = "0", nil
		body["empty"] = true
		body["trend"] = []any{}
		app, _ := newUsageApp(t, func(w http.ResponseWriter, _ *http.Request) { writeUsageJSON(w, http.StatusOK, body) })
		got, err := app.GetUsageOverview(todayQuery())
		if err != nil {
			t.Fatal(err)
		}
		if got.Summary.TotalTokens == nil || *got.Summary.TotalTokens != "0" {
			t.Fatalf("real zero must stay \"0\": %v", got.Summary.TotalTokens)
		}
		if got.Summary.CacheHitRate != nil || got.Summary.CacheHitTokens != nil {
			t.Fatal("unknown cache must stay null, never 0%")
		}
	})

	t.Run("requests page bound to the requested range", func(t *testing.T) {
		body := usageRequestsFixture()
		body["effectiveRange"] = map[string]any{"startAt": usageFixtureStart, "endAt": "2026-11-10T13:00:00+08:00", "timezone": "Asia/Shanghai"}
		app, _ := newUsageApp(t, func(w http.ResponseWriter, _ *http.Request) { writeUsageJSON(w, http.StatusOK, body) })
		_, err := app.GetUsageRequests(protocol.UsageRequestsQuery{Query: usageResolvedFixture(), PageSize: 20})
		if code := usageErrCode(t, err); code != protocol.ErrCodeUsageInvalidResponse {
			t.Fatalf("code = %s", code)
		}
	})

	t.Run("requests page rows are checked", func(t *testing.T) {
		for name, edit := range map[string]func(map[string]any){
			"page size changed":   func(v map[string]any) { v["pageSize"] = 50 },
			"more without cursor": func(v map[string]any) { v["nextCursor"] = "" },
			"duplicate id":        func(v map[string]any) { v["items"].([]any)[1].(map[string]any)["id"] = "b" },
			"not descending":      func(v map[string]any) { v["items"].([]any)[1].(map[string]any)["recordedAt"] = "2026-11-10T03:30:00Z" },
			"row outside range":   func(v map[string]any) { v["items"].([]any)[0].(map[string]any)["recordedAt"] = "2026-11-10T04:00:00Z" },
			"hit above input":     func(v map[string]any) { v["items"].([]any)[0].(map[string]any)["cacheHitTokens"] = "1001" },
			"negative duration":   func(v map[string]any) { v["items"].([]any)[0].(map[string]any)["durationMs"] = -1 },
		} {
			t.Run(name, func(t *testing.T) {
				body := usageRequestsFixture()
				edit(body)
				app, _ := newUsageApp(t, func(w http.ResponseWriter, _ *http.Request) { writeUsageJSON(w, http.StatusOK, body) })
				_, err := app.GetUsageRequests(protocol.UsageRequestsQuery{Query: usageResolvedFixture(), PageSize: 20})
				if code := usageErrCode(t, err); code != protocol.ErrCodeUsageInvalidResponse {
					t.Fatalf("code = %s", code)
				}
			})
		}
	})
}

func TestUsageErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		want   string
	}{
		{http.StatusForbidden, "forbidden", protocol.ErrCodeUsageForbidden},
		{http.StatusBadRequest, "invalid_cursor", protocol.ErrCodeUsageInvalidCursor},
		{http.StatusBadRequest, "invalid_range", protocol.ErrCodeUsageInvalidRange},
		{http.StatusUnprocessableEntity, "unsupported", protocol.ErrCodeUsageUnsupported},
		{http.StatusGatewayTimeout, "timeout", protocol.ErrCodeUsageTimeout},
		{http.StatusServiceUnavailable, "temporarily_unavailable", protocol.ErrCodeUsageUnavailable},
		{http.StatusInternalServerError, "", protocol.ErrCodeUsageUnavailable},
		{http.StatusNotFound, "", protocol.ErrCodeUsageUnavailable},
	} {
		t.Run(http.StatusText(tc.status)+"/"+tc.code, func(t *testing.T) {
			app, b := newUsageApp(t, func(w http.ResponseWriter, _ *http.Request) {
				writeUsageJSON(w, tc.status, map[string]any{"code": tc.code, "message": "jdbc:mysql://secret-host/core password=x", "requestId": "req-42"})
			})
			_, err := app.GetUsageOverview(todayQuery())
			if got := usageErrCode(t, err); got != tc.want {
				t.Fatalf("code = %s, want %s", got, tc.want)
			}
			var pe *hostproto.Error
			errors.As(err, &pe)
			if strings.Contains(pe.Message, "jdbc") || strings.Contains(pe.Message, "password") {
				t.Fatalf("raw upstream message leaked: %q", pe.Message)
			}
			if pe.Details["requestId"] != "req-42" {
				t.Fatalf("requestId not kept for support: %v", pe.Details)
			}
			if b.refresh.Load() != 0 || b.gets.Load() != 1 {
				t.Fatalf("non-401 failures must not refresh or retry: gets=%d refreshes=%d", b.gets.Load(), b.refresh.Load())
			}
		})
	}
	t.Run("unsafe request id is dropped", func(t *testing.T) {
		app, _ := newUsageApp(t, func(w http.ResponseWriter, _ *http.Request) {
			writeUsageJSON(w, http.StatusServiceUnavailable, map[string]any{"requestId": "<script>"})
		})
		_, err := app.GetUsageOverview(todayQuery())
		var pe *hostproto.Error
		if !errors.As(err, &pe) || pe.Details["requestId"] != "" {
			t.Fatalf("details = %v", pe)
		}
	})
}

func TestUsageRedirectRejected(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Add(1)
		writeUsageJSON(w, http.StatusOK, usageOverviewFixture())
	}))
	defer other.Close()
	app, _ := newUsageApp(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/api/usage/overview", http.StatusFound)
	})
	_, err := app.GetUsageOverview(todayQuery())
	if code := usageErrCode(t, err); code != protocol.ErrCodeUsageUnavailable {
		t.Fatalf("code = %s", code)
	}
	if leaked.Load() != 0 {
		t.Fatal("redirect was followed; bearer token could leave the configured Bridge")
	}
}

func TestUsageDeadline(t *testing.T) {
	if usageCommandTimeout != 20*time.Second {
		t.Fatalf("command budget = %s", usageCommandTimeout)
	}
	app, _ := newUsageApp(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := app.usageOverview(ctx, todayQuery())
	if code := usageErrCode(t, err); code != protocol.ErrCodeUsageTimeout {
		t.Fatalf("code = %s", code)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("deadline not honored: %s", elapsed)
	}

	// The deadline covers the retry: a 401 followed by a hang still ends on time.
	var calls atomic.Int32
	app2, _ := newUsageApp(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeUsageJSON(w, http.StatusUnauthorized, map[string]any{})
			return
		}
		<-r.Context().Done()
	})
	ctx2, cancel2 := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel2()
	_, err = app2.usageOverview(ctx2, todayQuery())
	if code := usageErrCode(t, err); code != protocol.ErrCodeUsageTimeout {
		t.Fatalf("retry code = %s", code)
	}

	// Caller cancellation (page left) ends the read promptly too.
	app3, _ := newUsageApp(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx3, cancel3 := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel3() }()
	if _, err := app3.usageOverview(ctx3, todayQuery()); err == nil {
		t.Fatal("cancelled read succeeded")
	}
}

func TestUsageRequestValidation(t *testing.T) {
	app, b := newUsageApp(t, func(w http.ResponseWriter, _ *http.Request) { writeUsageJSON(w, http.StatusOK, usageOverviewFixture()) })
	overviews := map[string]protocol.UsageQuery{
		"empty tenant":         {Range: "today"},
		"blank tenant":         {TenantID: "  ", Range: "today"},
		"tenant with slash":    {TenantID: "a/b", Range: "today"},
		"unknown range":        {TenantID: "t", Range: "forever"},
		"foreign timezone":     {TenantID: "t", Range: "today", Timezone: "UTC"},
		"relative with bounds": {TenantID: "t", Range: "today", StartAt: usageFixtureStart},
		"custom without start": {TenantID: "t", Range: "custom", EndAt: usageFixtureEnd},
		"custom inverted":      {TenantID: "t", Range: "custom", StartAt: usageFixtureEnd, EndAt: usageFixtureStart},
		"custom empty":         {TenantID: "t", Range: "custom", StartAt: usageFixtureStart, EndAt: usageFixtureStart},
		"custom future end":    {TenantID: "t", Range: "custom", StartAt: usageFixtureStart, EndAt: "2026-11-11T00:00:00+08:00"},
		"custom no offset":     {TenantID: "t", Range: "custom", StartAt: "2026-11-10T00:00:00", EndAt: usageFixtureEnd},
		"follow with end":      {TenantID: "t", Range: "custom", StartAt: usageFixtureStart, EndAt: usageFixtureEnd, FollowNow: true},
		"control in model":     {TenantID: "t", Range: "today", Model: "a\nb"},
	}
	for name, q := range overviews {
		if _, err := app.GetUsageOverview(q); usageErrCode(t, err) != hostproto.ErrCodeInvalidArgument {
			t.Fatalf("%s: code = %s", name, usageErrCode(t, err))
		}
	}
	resolved := usageResolvedFixture()
	for name, q := range map[string]protocol.UsageRequestsQuery{
		"page size 30":    {Query: resolved, PageSize: 30},
		"cursor spaces":   {Query: resolved, PageSize: 20, Cursor: "a b"},
		"range inverted":  {Query: protocol.ResolvedUsageQuery{TenantID: "t", EffectiveRange: protocol.UsageEffectiveRange{StartAt: usageFixtureEnd, EndAt: usageFixtureStart, Timezone: "Asia/Shanghai"}}, PageSize: 20},
		"range no tz":     {Query: protocol.ResolvedUsageQuery{TenantID: "t", EffectiveRange: protocol.UsageEffectiveRange{StartAt: usageFixtureStart, EndAt: usageFixtureEnd}}, PageSize: 20},
		"tenant missing":  {Query: protocol.ResolvedUsageQuery{EffectiveRange: resolved.EffectiveRange}, PageSize: 20},
		"cursor too long": {Query: resolved, PageSize: 20, Cursor: strings.Repeat("a", 4097)},
	} {
		if _, err := app.GetUsageRequests(q); usageErrCode(t, err) != hostproto.ErrCodeInvalidArgument {
			t.Fatalf("%s: code = %s", name, usageErrCode(t, err))
		}
	}
	for name, q := range map[string]protocol.UsageDimensionsQuery{
		"group by user": {Query: resolved, GroupBy: "user", Page: 1, PageSize: 50},
		"page size 20":  {Query: resolved, GroupBy: "model", Page: 1, PageSize: 20},
		"page zero":     {Query: resolved, GroupBy: "model", Page: 0, PageSize: 50},
		"long search":   {Query: resolved, GroupBy: "model", Page: 1, PageSize: 50, Search: strings.Repeat("模", 101)},
	} {
		if _, err := app.GetUsageDimensions(q); usageErrCode(t, err) != hostproto.ErrCodeInvalidArgument {
			t.Fatalf("%s: code = %s", name, usageErrCode(t, err))
		}
	}
	if b.gets.Load() != 0 {
		t.Fatalf("invalid input reached the network %d times", b.gets.Load())
	}
}

func TestUsageQueryEncoding(t *testing.T) {
	var seen []url.Values
	app, _ := newUsageApp(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		switch r.URL.Path {
		case "/api/usage/overview":
			body := usageOverviewFixture()
			writeUsageJSON(w, http.StatusOK, body)
		case "/api/usage/requests":
			writeUsageJSON(w, http.StatusOK, usageRequestsFixture())
		case "/api/usage/dimensions":
			writeUsageJSON(w, http.StatusOK, usageDimensionsFixture())
		default:
			http.NotFound(w, r)
		}
	})
	if _, err := app.GetUsageOverview(protocol.UsageQuery{TenantID: "tenant-a", Range: "custom", StartAt: usageFixtureStart, FollowNow: true, Model: "qwen-max"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetUsageRequests(protocol.UsageRequestsQuery{Query: usageResolvedFixture(), PageSize: 20, Cursor: "c1.sig"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetUsageDimensions(protocol.UsageDimensionsQuery{Query: usageResolvedFixture(), GroupBy: "model", Search: "qwen", Page: 1, PageSize: 50}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 {
		t.Fatalf("requests = %d", len(seen))
	}
	want := []map[string]string{
		{"tenantId": "tenant-a", "range": "custom", "startAt": usageFixtureStart, "followNow": "true", "timezone": "Asia/Shanghai", "model": "qwen-max"},
		{"tenantId": "tenant-a", "startAt": usageFixtureStart, "endAt": usageFixtureEnd, "timezone": "Asia/Shanghai", "cursor": "c1.sig", "pageSize": "20"},
		{"tenantId": "tenant-a", "startAt": usageFixtureStart, "endAt": usageFixtureEnd, "timezone": "Asia/Shanghai", "groupBy": "model", "search": "qwen", "page": "1", "pageSize": "50"},
	}
	for i, w := range want {
		if len(seen[i]) != len(w) {
			t.Fatalf("request %d keys = %v, want %v", i, seen[i], w)
		}
		for k, v := range w {
			if seen[i].Get(k) != v {
				t.Fatalf("request %d %s = %q, want %q", i, k, seen[i].Get(k), v)
			}
		}
	}
}

package localusage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// QueryFilter 是本机查询的最小条件。ScopeID 已由宿主根据当前账号、Bridge 和租户冻结，
// 查询层不接受自由账号字段，也不会访问在线服务补齐身份。
type QueryFilter struct {
	ScopeID     string
	StartAt     time.Time
	EndAt       time.Time
	Model       string
	SnapshotSeq int64
}

// CallRecord 是从 start 与 finish 事件还原的一次逻辑调用；同一 callId 永远只返回一行。
type CallRecord struct {
	CallID               string
	ScopeID              string
	Model                string
	CallKind             string
	Status               string
	StartedAt            time.Time
	FinishedAt           *time.Time
	DurationMs           *int64
	InputTokens          *int64
	InputTokensKnown     bool
	OutputTokens         *int64
	OutputTokensKnown    bool
	CacheHitTokens       *int64
	CacheHitTokensKnown  bool
	CacheMissTokens      *int64
	CacheMissTokensKnown bool
	GeneratedImages      *int64
}

// QuerySnapshot 是一次性全量查询结果（含水位 seq）。
type QuerySnapshot struct {
	Seq   int64
	Calls []CallRecord
}

// QueryPage 是分页查询结果。
type QueryPage struct {
	Seq        int64
	Calls      []CallRecord
	TotalCount int
	NextCursor string
	HasMore    bool
}

type queryCursor struct {
	Seq        int64  `json:"seq"`
	Started    int64  `json:"started"`
	CallID     string `json:"callId"`
	ScopeID    string `json:"scopeId"`
	Model      string `json:"model"`
	StartRange int64  `json:"startRange"`
	EndRange   int64  `json:"endRange"`
}

// QuerySnapshot 按过滤条件返回完整调用列表与水位。
func (s *Store) QuerySnapshot(ctx context.Context, filter QueryFilter) (QuerySnapshot, error) {
	if err := validateQueryFilter(filter); err != nil {
		return QuerySnapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return QuerySnapshot{}, ErrStoreClosed
	}
	seq, err := s.snapshotSeq(ctx, filter.SnapshotSeq)
	if err != nil {
		return QuerySnapshot{}, err
	}
	filter.SnapshotSeq = seq
	calls, err := s.queryCallsLocked(ctx, filter)
	if err != nil {
		return QuerySnapshot{}, err
	}
	return QuerySnapshot{Seq: seq, Calls: calls}, nil
}

// QueryPage 使用固定 snapshotSeq 还原页；迟到 finish 不会改写已经发出的旧页。
// 页码由客户端维护，游标绑定快照、scope、模型、时间范围和上一行排序键。
func (s *Store) QueryPage(ctx context.Context, filter QueryFilter, cursor string, limit int) (QueryPage, error) {
	if limit <= 0 || limit > 1000 {
		return QueryPage{}, fmt.Errorf("查询页大小无效")
	}
	if err := validateQueryFilter(filter); err != nil {
		return QueryPage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return QueryPage{}, ErrStoreClosed
	}
	if cursor != "" {
		decoded, err := decodeCursor(cursor)
		if err != nil || decoded.ScopeID != filter.ScopeID || decoded.Model != filter.Model || decoded.StartRange != filter.StartAt.UTC().UnixMilli() || decoded.EndRange != filter.EndAt.UTC().UnixMilli() {
			return QueryPage{}, fmt.Errorf("分页游标无效")
		}
		filter.SnapshotSeq = decoded.Seq
	}
	seq, err := s.snapshotSeq(ctx, filter.SnapshotSeq)
	if err != nil {
		return QueryPage{}, err
	}
	filter.SnapshotSeq = seq
	calls, err := s.queryCallsLocked(ctx, filter)
	if err != nil {
		return QueryPage{}, err
	}
	if cursor != "" {
		decoded, _ := decodeCursor(cursor)
		start := -1
		for i, call := range calls {
			if call.StartedAt.UnixMilli() == decoded.Started && call.CallID == decoded.CallID {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return QueryPage{}, fmt.Errorf("分页游标无效")
		}
		calls = calls[start:]
	}
	total := len(calls)
	if cursor != "" {
		// totalCount 需要反映整份快照，而不是当前游标之后的剩余行。
		all, err := s.queryCallsLocked(ctx, QueryFilter{ScopeID: filter.ScopeID, StartAt: filter.StartAt, EndAt: filter.EndAt, Model: filter.Model, SnapshotSeq: seq})
		if err != nil {
			return QueryPage{}, err
		}
		total = len(all)
	}
	page := calls
	if len(page) > limit {
		page = page[:limit]
	}
	out := QueryPage{Seq: seq, Calls: page, TotalCount: total, HasMore: len(calls) > len(page)}
	if out.HasMore && len(page) > 0 {
		last := page[len(page)-1]
		out.NextCursor = encodeCursor(queryCursor{Seq: seq, Started: last.StartedAt.UnixMilli(), CallID: last.CallID, ScopeID: filter.ScopeID, Model: filter.Model, StartRange: filter.StartAt.UTC().UnixMilli(), EndRange: filter.EndAt.UTC().UnixMilli()})
	}
	return out, nil
}

// ListTenantIDs 列出某账号+Bridge 下已出现过的租户 ID。
func (s *Store) ListTenantIDs(ctx context.Context, accountDigest, bridgeDigest string) ([]string, error) {
	if accountDigest == "" || bridgeDigest == "" {
		return nil, fmt.Errorf("账号或 Bridge 摘要不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrStoreClosed
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT tenant_id FROM scopes WHERE account_digest=? AND bridge_digest=? AND tenant_id<>'' ORDER BY tenant_id`, accountDigest, bridgeDigest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var tenant string
		if err := rows.Scan(&tenant); err != nil {
			return nil, err
		}
		out = append(out, tenant)
	}
	return out, rows.Err()
}

func validateQueryFilter(f QueryFilter) error {
	if f.ScopeID == "" || f.StartAt.IsZero() || f.EndAt.IsZero() || !f.StartAt.Before(f.EndAt) || f.StartAt.Location() == nil || f.EndAt.Location() == nil {
		return errors.New("查询范围无效")
	}
	return nil
}

func (s *Store) snapshotSeq(ctx context.Context, requested int64) (int64, error) {
	var current sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(seq) FROM events`).Scan(&current); err != nil {
		return 0, err
	}
	if requested > 0 {
		if !current.Valid || requested > current.Int64 {
			return 0, fmt.Errorf("查询快照已失效")
		}
		return requested, nil
	}
	if !current.Valid {
		return 0, nil
	}
	return current.Int64, nil
}

func (s *Store) queryCallsLocked(ctx context.Context, f QueryFilter) ([]CallRecord, error) {
	query := `
SELECT s.call_id, s.scope_id, s.model, s.call_kind, s.started_at_ms,
       f.finished_at_ms, f.duration_ms, COALESCE(f.status, 'pending'),
	   f.input_tokens, f.input_tokens_known, f.output_tokens, f.output_tokens_known,
	   f.cache_hit_tokens, f.cache_hit_tokens_known, f.cache_miss_tokens, f.cache_miss_tokens_known,
	   f.generated_images
FROM events s
LEFT JOIN events f ON f.call_id=s.call_id AND f.phase='finish' AND f.seq<=?
WHERE s.phase='start' AND s.seq<=? AND s.scope_id=? AND s.started_at_ms>=? AND s.started_at_ms<?`
	args := []any{f.SnapshotSeq, f.SnapshotSeq, f.ScopeID, f.StartAt.UTC().UnixMilli(), f.EndAt.UTC().UnixMilli()}
	if f.Model != "" {
		query += ` AND s.model=?`
		args = append(args, f.Model)
	}
	query += ` ORDER BY s.started_at_ms DESC, s.call_id DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CallRecord
	for rows.Next() {
		var call CallRecord
		var started, finished sql.NullInt64
		var duration, input, inputKnown, output, outputKnown, cacheHit, cacheHitKnown, cacheMiss, cacheMissKnown, images sql.NullInt64
		if err := rows.Scan(&call.CallID, &call.ScopeID, &call.Model, &call.CallKind, &started, &finished, &duration, &call.Status, &input, &inputKnown, &output, &outputKnown, &cacheHit, &cacheHitKnown, &cacheMiss, &cacheMissKnown, &images); err != nil {
			return nil, err
		}
		call.StartedAt = time.UnixMilli(started.Int64).UTC()
		if finished.Valid {
			t := time.UnixMilli(finished.Int64).UTC()
			call.FinishedAt = &t
		}
		if duration.Valid {
			v := duration.Int64
			call.DurationMs = &v
		}
		call.InputTokensKnown = inputKnown.Valid && inputKnown.Int64 != 0 && input.Valid
		if call.InputTokensKnown {
			v := input.Int64
			call.InputTokens = &v
		}
		call.OutputTokensKnown = outputKnown.Valid && outputKnown.Int64 != 0 && output.Valid
		if call.OutputTokensKnown {
			v := output.Int64
			call.OutputTokens = &v
		}
		call.CacheHitTokensKnown = cacheHitKnown.Valid && cacheHitKnown.Int64 != 0 && cacheHit.Valid
		if call.CacheHitTokensKnown {
			v := cacheHit.Int64
			call.CacheHitTokens = &v
		}
		call.CacheMissTokensKnown = cacheMissKnown.Valid && cacheMissKnown.Int64 != 0 && cacheMiss.Valid
		if call.CacheMissTokensKnown {
			v := cacheMiss.Int64
			call.CacheMissTokens = &v
		}
		if images.Valid {
			v := images.Int64
			call.GeneratedImages = &v
		}
		out = append(out, call)
	}
	return out, rows.Err()
}

func encodeCursor(c queryCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (queryCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return queryCursor{}, err
	}
	var c queryCursor
	if err := json.Unmarshal(b, &c); err != nil || c.Seq <= 0 || c.Started <= 0 || c.CallID == "" || c.ScopeID == "" || c.StartRange < 0 || c.EndRange <= c.StartRange {
		return queryCursor{}, errors.New("游标内容无效")
	}
	return c, nil
}

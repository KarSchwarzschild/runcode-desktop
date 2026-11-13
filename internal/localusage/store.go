package localusage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，打开 usage.sqlite3
)

// 写入预算：统计失败不得拖垮业务路径（设计：200ms）。
const writeBudget = 200 * time.Millisecond

var (
	// ErrSchemaTooNew 表示库 schema 比本二进制新，拒绝打开，不得删库。
	ErrSchemaTooNew = errors.New("本地用量库版本过新，当前客户端无法打开")
	// ErrStoreClosed 表示关闭后拒绝写入。
	ErrStoreClosed = errors.New("本地用量记录器已关闭")
	// ErrInvalidRecord 表示字段不合法。
	ErrInvalidRecord = errors.New("本地用量记录字段不合法")
	// ErrScopeConflict 表示同一 callId 试图更换冻结 scope。
	ErrScopeConflict = errors.New("本地用量 callId 的归属 scope 不可更换")
)

// Store 是本机 usage.sqlite3 的唯一写入／查询入口；实现 Recorder。
type Store struct {
	db   *sql.DB
	path string
	now  func() time.Time
	run  string

	mu     sync.Mutex
	closed bool
}

// Open 打开或创建独立用量库。path 必须是绝对路径；now 可注入测试时钟。
// 损坏／版本过新／权限失败原样返回，不清空重建。
func Open(path string, now func() time.Time) (*Store, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: 路径必须是绝对路径", ErrInvalidRecord)
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("创建用量目录失败: %w", err)
	}
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath, RawQuery: "_pragma=busy_timeout(200)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // 单写序列
	s := &Store{db: db, path: path, now: now, run: fmt.Sprintf("run-%d", now().UTC().UnixNano())}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.recoverDirtyRuns(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.markRunDirty(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) initSchema() error {
	var ver string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&ver)
	if errors.Is(err, sql.ErrNoRows) || isMissingTable(err) {
		if _, err := s.db.Exec(createSchemaSQL); err != nil {
			return fmt.Errorf("初始化用量 schema 失败: %w", err)
		}
		_, err = s.db.Exec(`INSERT INTO meta(key,value) VALUES('schema_version',?)`, fmt.Sprintf("%d", SchemaVersion))
		return err
	}
	if err != nil {
		return err
	}
	var n int
	if _, err := fmt.Sscanf(ver, "%d", &n); err != nil {
		return fmt.Errorf("%w: schema_version=%q", ErrSchemaTooNew, ver)
	}
	if n > SchemaVersion {
		return fmt.Errorf("%w: 库=%d 客户端=%d", ErrSchemaTooNew, n, SchemaVersion)
	}
	if n < SchemaVersion {
		if n == 1 && SchemaVersion == 2 {
			if err := migrateV1ToV2(s.db); err != nil {
				return fmt.Errorf("本地用量库迁移到 v2 失败: %w", err)
			}
			return nil
		}
		return fmt.Errorf("本地用量库 schema=%d 过旧，需要迁移（当前仅支持 %d）", n, SchemaVersion)
	}
	return nil
}

// migrateV1ToV2 为已存在的本机库补充缓存命中／未命中字段；只追加列，
// 不删除历史事件，也不把旧记录的未知缓存误写成 0。
func migrateV1ToV2(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`ALTER TABLE events ADD COLUMN cache_hit_tokens INTEGER`,
		`ALTER TABLE events ADD COLUMN cache_hit_tokens_known INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE events ADD COLUMN cache_miss_tokens INTEGER`,
		`ALTER TABLE events ADD COLUMN cache_miss_tokens_known INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE meta SET value='2' WHERE key='schema_version'`); err != nil {
		return err
	}
	return tx.Commit()
}

func isMissingTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

func (s *Store) markRunDirty() error {
	_, err := s.db.Exec(`INSERT INTO meta(key,value) VALUES('active_run_id',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, s.run)
	return err
}

// recoverDirtyRuns：上次未干净退出时，将该 runId 的未完成 start 追加 unknown finish。
// 不在查询页面时恢复；不重发任何上游请求。
func (s *Store) recoverDirtyRuns() error {
	var dirty string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'active_run_id'`).Scan(&dirty)
	if errors.Is(err, sql.ErrNoRows) || isMissingTable(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if dirty == "" || dirty == s.run {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT e.call_id, e.scope_id, e.account_digest, e.bridge_digest, e.tenant_id,
		       e.model, e.call_kind, e.session_id, e.turn_id, e.generation_id, e.started_at_ms, e.run_id
		FROM events e
		WHERE e.phase = 'start' AND e.run_id = ?
		  AND NOT EXISTS (SELECT 1 FROM events f WHERE f.call_id = e.call_id AND f.phase = 'finish')`, dirty)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	nowMs := s.now().UTC().UnixMilli()
	type pending struct {
		callID, scopeID, account, bridge, tenant, model, kind, session, turn, gen, runID string
		started                                                                          int64
	}
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.callID, &p.scopeID, &p.account, &p.bridge, &p.tenant,
			&p.model, &p.kind, &p.session, &p.turn, &p.gen, &p.started, &p.runID); err != nil {
			return err
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range list {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO events(
				call_id, phase, run_id, scope_id, account_digest, bridge_digest, tenant_id,
				model, call_kind, session_id, turn_id, generation_id, started_at_ms,
				finished_at_ms, status, token_source, recorded_at_ms
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.callID, "finish", p.runID, p.scopeID, p.account, p.bridge, p.tenant,
			p.model, p.kind, p.session, p.turn, p.gen, p.started,
			nowMs, StatusUnknown, "unavailable", nowMs)
		if err != nil {
			return err
		}
		_, _ = tx.ExecContext(ctx, `
			INSERT INTO coverage_gaps(scope_id, start_at_ms, end_at_ms, reason)
			VALUES (?,?,?,?)`, p.scopeID, p.started, nowMs, "unclean_exit")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM meta WHERE key = 'active_run_id'`); err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, `INSERT INTO diagnostics(at_ms, code, detail) VALUES (?,?,?)`,
		nowMs, "recovered_unclean_exit", dirty)
	return tx.Commit()
}

// Begin 写入 start 事件；重复 start（同 callId）返回错误但不破坏库。
func (s *Store) Begin(ctx context.Context, start StartRecord) error {
	if err := validateStart(start); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeBudget)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}
	return s.beginLocked(ctx, start)
}

func (s *Store) beginLocked(ctx context.Context, start StartRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := ensureScope(ctx, tx, start.Scope, start.StartedAt.UTC().UnixMilli()); err != nil {
		return err
	}
	// 已有同 callId 的 start：拒绝更换 scope；幂等重复 start 视为冲突（设计：UNIQUE）。
	var existingScope string
	err = tx.QueryRowContext(ctx, `SELECT scope_id FROM events WHERE call_id=? AND phase='start'`, start.CallID).Scan(&existingScope)
	if err == nil {
		if existingScope != start.Scope.ScopeID {
			return ErrScopeConflict
		}
		return fmt.Errorf("%w: callId 已有 start", ErrInvalidRecord)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	nowMs := s.now().UTC().UnixMilli()
	started := start.StartedAt.UTC().UnixMilli()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events(
			call_id, phase, run_id, scope_id, account_digest, bridge_digest, tenant_id,
			model, call_kind, session_id, turn_id, generation_id, started_at_ms,
			status, token_source, recorded_at_ms
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		start.CallID, "start", s.run, start.Scope.ScopeID, start.Scope.AccountDigest, start.Scope.BridgeDigest, start.Scope.TenantID,
		start.Model, start.CallKind, start.SessionID, start.TurnID, start.GenerationID, started,
		StatusPending, "unavailable", nowMs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE scopes SET last_healthy_write_at_ms=? WHERE scope_id=?`, nowMs, start.Scope.ScopeID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Finish 写入终态。重复 finish 不覆盖先前终态；矛盾终态记诊断。
func (s *Store) Finish(ctx context.Context, finish FinishRecord) error {
	if err := validateFinish(finish); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeBudget)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}
	return s.finishLocked(ctx, finish)
}

func (s *Store) finishLocked(ctx context.Context, finish FinishRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var existingStatus sql.NullString
	var existingScope string
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(status,''), scope_id FROM events WHERE call_id=? AND phase='finish'`, finish.CallID).
		Scan(&existingStatus, &existingScope)
	if err == nil {
		// 已有终态：不覆盖；矛盾则记诊断。
		if existingStatus.String != "" && existingStatus.String != finish.Status {
			_, _ = tx.ExecContext(ctx, `INSERT INTO diagnostics(at_ms, code, detail) VALUES (?,?,?)`,
				s.now().UTC().UnixMilli(), "contradictory_finish",
				finish.CallID+":"+existingStatus.String+"->"+finish.Status)
			_ = tx.Commit()
			return nil // 不报业务失败，避免统计拖垮调用方
		}
		_ = tx.Commit()
		return nil // 幂等重复 finish
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// 读取已有 start 的完整冻结元数据。finish 可能在调用方没有保留
	// FrozenStart 的情况下到达，不能因此把归属、模型或调用类型写空。
	var frozen StartRecord
	var startScope string
	var startedMs int64
	err = tx.QueryRowContext(ctx, `
		SELECT scope_id, account_digest, bridge_digest, tenant_id, model, call_kind,
		       session_id, turn_id, generation_id, started_at_ms
		FROM events WHERE call_id=? AND phase='start'`, finish.CallID).
		Scan(&startScope, &frozen.Scope.AccountDigest, &frozen.Scope.BridgeDigest,
			&frozen.Scope.TenantID, &frozen.Model, &frozen.CallKind,
			&frozen.SessionID, &frozen.TurnID, &frozen.GenerationID, &startedMs)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := validateStart(finish.FrozenStart); err != nil {
			return fmt.Errorf("补写 start 失败: %w", err)
		}
		if finish.FrozenStart.CallID != finish.CallID {
			return fmt.Errorf("%w: FrozenStart.callId 与 finish 不一致", ErrInvalidRecord)
		}
		if err := ensureScope(ctx, tx, finish.FrozenStart.Scope, finish.FrozenStart.StartedAt.UTC().UnixMilli()); err != nil {
			return err
		}
		frozen = finish.FrozenStart
		nowMs := s.now().UTC().UnixMilli()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO events(
				call_id, phase, run_id, scope_id, account_digest, bridge_digest, tenant_id,
				model, call_kind, session_id, turn_id, generation_id, started_at_ms,
				status, token_source, recorded_at_ms
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			finish.FrozenStart.CallID, "start", s.run,
			finish.FrozenStart.Scope.ScopeID, finish.FrozenStart.Scope.AccountDigest,
			finish.FrozenStart.Scope.BridgeDigest, finish.FrozenStart.Scope.TenantID,
			finish.FrozenStart.Model, finish.FrozenStart.CallKind,
			finish.FrozenStart.SessionID, finish.FrozenStart.TurnID, finish.FrozenStart.GenerationID,
			finish.FrozenStart.StartedAt.UTC().UnixMilli(), StatusPending, "unavailable", nowMs)
		if err != nil {
			return err
		}
		startScope = finish.FrozenStart.Scope.ScopeID
	case err != nil:
		return err
	default:
		frozen.Scope.ScopeID = startScope
		frozen.StartedAt = time.UnixMilli(startedMs).UTC()
		if finish.FrozenStart.Scope.ScopeID != "" && finish.FrozenStart.Scope.ScopeID != startScope {
			return ErrScopeConflict
		}
	}

	nowMs := s.now().UTC().UnixMilli()
	finished := finish.FinishedAt.UTC().UnixMilli()
	started := frozen.StartedAt.UTC().UnixMilli()
	tokenSource := finish.TokenSource
	if tokenSource == "" {
		tokenSource = "unavailable"
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events(
			call_id, phase, run_id, scope_id, account_digest, bridge_digest, tenant_id,
			model, call_kind, session_id, turn_id, generation_id, started_at_ms,
			finished_at_ms, duration_ms, status,
			input_tokens, input_tokens_known, output_tokens, output_tokens_known,
			cache_hit_tokens, cache_hit_tokens_known, cache_miss_tokens, cache_miss_tokens_known,
			generated_images, token_source, error_code, recorded_at_ms
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		finish.CallID, "finish", s.run, startScope,
		frozen.Scope.AccountDigest, frozen.Scope.BridgeDigest, frozen.Scope.TenantID,
		frozen.Model, frozen.CallKind,
		frozen.SessionID, frozen.TurnID, frozen.GenerationID,
		started, finished, finish.DurationMs, finish.Status,
		nullableInt(finish.InputTokensKnown, finish.InputTokens), boolInt(finish.InputTokensKnown),
		nullableInt(finish.OutputTokensKnown, finish.OutputTokens), boolInt(finish.OutputTokensKnown),
		nullableInt(finish.CacheHitTokensKnown, finish.CacheHitTokens), boolInt(finish.CacheHitTokensKnown),
		nullableInt(finish.CacheMissTokensKnown, finish.CacheMissTokens), boolInt(finish.CacheMissTokensKnown),
		finish.GeneratedImages, tokenSource, finish.ErrorCode, nowMs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE scopes SET last_healthy_write_at_ms=? WHERE scope_id=?`, nowMs, startScope)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func ensureScope(ctx context.Context, tx *sql.Tx, scope Scope, startedMs int64) error {
	if scope.ScopeID == "" || scope.AccountDigest == "" || scope.BridgeDigest == "" || scope.TenantID == "" {
		return fmt.Errorf("%w: scope 不完整", ErrInvalidRecord)
	}
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT scope_id FROM scopes WHERE scope_id=?`, scope.ScopeID).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO scopes(scope_id, account_digest, bridge_digest, tenant_id, collection_started_at_ms, coverage_status)
		VALUES (?,?,?,?,?, 'ok')`,
		scope.ScopeID, scope.AccountDigest, scope.BridgeDigest, scope.TenantID, startedMs)
	return err
}

func nullableInt(known bool, v *int64) any {
	if !known || v == nil {
		return nil
	}
	return *v
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func validateStart(s StartRecord) error {
	if s.CallID == "" || s.Model == "" || s.CallKind == "" {
		return fmt.Errorf("%w: start 缺 callId/model/callKind", ErrInvalidRecord)
	}
	switch s.CallKind {
	case CallKindLLM, CallKindVision, CallKindImage, CallKindSearch:
	default:
		return fmt.Errorf("%w: 未知 callKind", ErrInvalidRecord)
	}
	if s.Scope.ScopeID == "" || s.Scope.TenantID == "" {
		return fmt.Errorf("%w: start scope 不完整", ErrInvalidRecord)
	}
	if s.StartedAt.IsZero() {
		return fmt.Errorf("%w: startedAt 必填", ErrInvalidRecord)
	}
	return nil
}

func validateFinish(f FinishRecord) error {
	if f.CallID == "" {
		return fmt.Errorf("%w: finish 缺 callId", ErrInvalidRecord)
	}
	switch f.Status {
	case StatusSucceeded, StatusFailed, StatusCancelled, StatusUnknown, StatusSaveFailed:
	default:
		return fmt.Errorf("%w: 非法 status %q", ErrInvalidRecord, f.Status)
	}
	if f.FinishedAt.IsZero() {
		return fmt.Errorf("%w: finishedAt 必填", ErrInvalidRecord)
	}
	return nil
}

// Close 标记干净退出并释放连接。必须在回合收尾之后调用。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	_, _ = s.db.Exec(`DELETE FROM meta WHERE key = 'active_run_id'`)
	return s.db.Close()
}

// Path 返回库文件绝对路径（测试／诊断用）。
func (s *Store) Path() string { return s.path }

// CountCalls 返回某 scope 的唯一 start 数（测试用）。
func (s *Store) CountCalls(ctx context.Context, scopeID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE scope_id=? AND phase='start'`, scopeID).Scan(&n)
	return n, err
}

// CallStatus 返回 callId 在 finish 水位的状态；无 finish 则看 start→pending。
func (s *Store) CallStatus(ctx context.Context, callID string) (string, error) {
	var st sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT status FROM events WHERE call_id=? AND phase='finish'`, callID).Scan(&st)
	if err == nil {
		return st.String, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var exists int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM events WHERE call_id=? AND phase='start'`, callID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", err
	}
	return StatusPending, nil
}

// DiagnosticCount 返回诊断条数（测试用）。
func (s *Store) DiagnosticCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM diagnostics`).Scan(&n)
	return n, err
}

// DB 暴露只读测试钩子；生产查询走专用方法。
func (s *Store) DB() *sql.DB { return s.db }

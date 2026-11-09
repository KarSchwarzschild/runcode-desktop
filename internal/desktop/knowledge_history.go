package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitlab.ouc-online.com.cn/aibase/agentloop/sessions"
)

var (
	errLegacyKnowledgeHistory = errors.New("此历史包含旧版知识库扩展，当前版本不能继续执行；请新建会话，旧记录已保留")
	errKnowledgeHistoryFormat = errors.New("无法安全读取会话历史格式；本次操作未执行，旧记录已保留")
)

// Match the engine checkpoint bound. Ordinary journals have no aggregate
// record cap: several valid images can exceed this size in one user message.
const knowledgeHistoryRecordLimit = 32 << 20

// v0.16.0 discards unknown JSON fields when loading llm.Message. Check the
// original journal AND derived checkpoint before handing either to the engine.
// This is an execution barrier, not the legacy history viewer (Task 6).
func checkKnowledgeHistory(ctx context.Context, workspace, id, kind string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := knowledgePath(workspace, id); err != nil {
		return errKnowledgeHistoryFormat
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return errKnowledgeHistoryFormat
	}
	defer func() { _ = root.Close() }()
	cp := fmt.Sprintf("cp-%x.json", sha256.Sum256([]byte(id)))
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", sessions.BackendJSONL:
		if err := checkKnowledgeHistoryFile(ctx, root, filepath.Join(".runcode", "sessions", id+".jsonl"), false); err != nil {
			return err
		}
		return checkKnowledgeHistoryFile(ctx, root, filepath.Join(".runcode", "context-state", cp), true)
	case sessions.BackendSQLite:
		return checkKnowledgeSQLite(ctx, root, workspace, id, cp)
	default:
		return errKnowledgeHistoryFormat
	}
}

func checkKnowledgeHistoryFile(ctx context.Context, root *os.Root, path string, checkpoint bool) error {
	f, err := root.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errKnowledgeHistoryFormat
	}
	defer func() { _ = f.Close() }()
	if checkpoint {
		raw, err := io.ReadAll(io.LimitReader(f, knowledgeHistoryRecordLimit+1))
		if err != nil || len(raw) > knowledgeHistoryRecordLimit {
			return errKnowledgeHistoryFormat
		}
		return checkKnowledgeRaw(ctx, raw, true)
	}
	dec := json.NewDecoder(f)
	dec.UseNumber()
	for {
		err := checkKnowledgeObject(ctx, dec, false)
		if errors.Is(err, io.EOF) {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
	}
}

func checkKnowledgeRaw(ctx context.Context, raw []byte, checkpoint bool) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := checkKnowledgeObject(ctx, dec, checkpoint); err != nil {
		if errors.Is(err, io.EOF) {
			return errKnowledgeHistoryFormat
		}
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errKnowledgeHistoryFormat
	}
	return ctx.Err()
}

// Token traversal avoids retaining a complete JSONL message (including all
// image data) or decoding it into an old engine type that drops unknown fields.
// Inspect every occurrence, including duplicate/case-variant keys: Go's struct
// decoder matches JSON tags case-insensitively and uses the last occurrence.
func checkKnowledgeObject(ctx context.Context, dec *json.Decoder, checkpoint bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	token, err := dec.Token()
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	if err != nil || token != json.Delim('{') {
		return errKnowledgeHistoryFormat
	}
	found := false
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := dec.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return errKnowledgeHistoryFormat
		}
		switch {
		case strings.EqualFold(key, "question_context_version"), strings.EqualFold(key, "question_context"), strings.EqualFold(key, "context_update"):
			return errLegacyKnowledgeHistory
		case strings.EqualFold(key, "messages"):
			if !checkpoint {
				if err := skipKnowledgeJSONValue(ctx, dec); err != nil {
					return err
				}
				continue
			}
			found = true
			token, err := dec.Token()
			if err != nil {
				return errKnowledgeHistoryFormat
			}
			if token == nil {
				continue
			}
			if token != json.Delim('[') {
				return errKnowledgeHistoryFormat
			}
			for dec.More() {
				if err := checkKnowledgeObject(ctx, dec, false); err != nil {
					return err
				}
			}
			if token, err = dec.Token(); err != nil || token != json.Delim(']') {
				return errKnowledgeHistoryFormat
			}
		case strings.EqualFold(key, "role"):
			if checkpoint {
				if err := skipKnowledgeJSONValue(ctx, dec); err != nil {
					return err
				}
				continue
			}
			found = true
			token, err := dec.Token()
			role, ok := token.(string)
			if err != nil || !ok || role == "" {
				return errKnowledgeHistoryFormat
			}
			if strings.EqualFold(role, "context_update") {
				return errLegacyKnowledgeHistory
			}
		default:
			if err := skipKnowledgeJSONValue(ctx, dec); err != nil {
				return err
			}
		}
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') || !found {
		return errKnowledgeHistoryFormat
	}
	return ctx.Err()
}

func skipKnowledgeJSONValue(ctx context.Context, dec *json.Decoder) error {
	depth := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := dec.Token()
		if err != nil {
			return errKnowledgeHistoryFormat
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
		if depth == 0 {
			return nil
		}
	}
}

func checkKnowledgeSQLite(ctx context.Context, root *os.Root, workspace, id, checkpoint string) error {
	// SQLite opens by path, so resolve and constrain the database and its WAL
	// sidecars as well. mode=ro prevents creation or migration of the old store.
	base, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return errKnowledgeHistoryFormat
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return errKnowledgeHistoryFormat
	}
	path := filepath.Join(base, ".runcode", "sessions.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		f, err := root.Open(filepath.Join(".runcode", "sessions.db"+suffix))
		if errors.Is(err, os.ErrNotExist) {
			if suffix == "" {
				return nil
			}
			continue
		}
		if err != nil {
			return errKnowledgeHistoryFormat
		}
		_ = f.Close()
		resolved, err := filepath.EvalSymlinks(path + suffix)
		if err != nil {
			return errKnowledgeHistoryFormat
		}
		rel, err := filepath.Rel(base, resolved)
		if err != nil || !filepath.IsLocal(rel) {
			return errKnowledgeHistoryFormat
		}
	}
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(1000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return errKnowledgeHistoryFormat
	}
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return errKnowledgeHistoryFormat
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, "SELECT data FROM messages WHERE session_id = ? ORDER BY seq", id)
	if err != nil {
		return errKnowledgeHistoryFormat
	}
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			_ = rows.Close()
			return errKnowledgeHistoryFormat
		}
		if err := checkKnowledgeRaw(ctx, raw, false); err != nil {
			_ = rows.Close()
			return err
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return errKnowledgeHistoryFormat
	}
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='context_state'").Scan(&exists); err != nil {
		return errKnowledgeHistoryFormat
	}
	if exists == 0 {
		return nil
	}
	var size int64
	var raw []byte
	err = tx.QueryRowContext(ctx, "SELECT length(data), CASE WHEN length(data) <= ? THEN data ELSE NULL END FROM context_state WHERE key = ?", knowledgeHistoryRecordLimit, checkpoint).Scan(&size, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil || size > knowledgeHistoryRecordLimit {
		return errKnowledgeHistoryFormat
	}
	return checkKnowledgeRaw(ctx, raw, true)
}

func (a *App) checkEntryKnowledgeHistory(ctx context.Context, e *sessionEntry, id string) error {
	a.mu.Lock()
	workspace, kind := e.workspace, e.connectionConfig.SessionBackend
	a.mu.Unlock()
	return checkKnowledgeHistory(ctx, workspace, id, kind)
}

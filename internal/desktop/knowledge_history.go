package desktop

import (
	"bufio"
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
	"sync"
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

// Successful checks are cached by journal/checkpoint fingerprint so resume,
// model switch, and later submits do not re-decode an unchanged history.
type knowledgeHistoryFingerprint struct {
	journalSize, journalMod int64
	checkpointSize, checkpointMod int64
}

type knowledgeHistoryCacheEntry struct {
	fp  knowledgeHistoryFingerprint
	err error
}

var knowledgeHistoryCache sync.Map // string → knowledgeHistoryCacheEntry

func knowledgeHistoryCacheKey(workspace, id, kind string) string {
	return workspace + "\x00" + id + "\x00" + strings.ToLower(strings.TrimSpace(kind))
}

// knowledgeHistoryCheckNeeded is true only when this session ever used knowledge
// selection (sidecar present) or is a legacy independent knowledge-chat id.
// Ordinary chats must not be blocked by a crash-truncated JSONL tail.
func knowledgeHistoryCheckNeeded(workspace, id string) bool {
	if strings.HasPrefix(id, "kc-") {
		return true
	}
	path, err := knowledgePath(workspace, id)
	if err != nil {
		return false
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func knowledgeHistoryFingerprintOf(root *os.Root, journalPath, checkpointPath string) knowledgeHistoryFingerprint {
	var fp knowledgeHistoryFingerprint
	statPath := func(path string) (size, mod int64) {
		info, err := root.Stat(path)
		if err != nil {
			return 0, 0
		}
		return info.Size(), info.ModTime().UnixNano()
	}
	fp.journalSize, fp.journalMod = statPath(journalPath)
	fp.checkpointSize, fp.checkpointMod = statPath(checkpointPath)
	return fp
}

// v0.16.0 discards unknown JSON fields when loading llm.Message. Check the
// original journal AND derived checkpoint before handing either to the engine.
// This is an execution barrier for sessions that used knowledge features.
func checkKnowledgeHistory(ctx context.Context, workspace, id, kind string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !knowledgeHistoryCheckNeeded(workspace, id) {
		return nil
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	cacheKey := knowledgeHistoryCacheKey(workspace, id, kind)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return errKnowledgeHistoryFormat
	}
	defer func() { _ = root.Close() }()
	cpName := fmt.Sprintf("cp-%x.json", sha256.Sum256([]byte(id)))
	journalPath := filepath.Join(".runcode", "sessions", id+".jsonl")
	checkpointPath := filepath.Join(".runcode", "context-state", cpName)

	var fp knowledgeHistoryFingerprint
	switch kind {
	case "", sessions.BackendJSONL:
		fp = knowledgeHistoryFingerprintOf(root, journalPath, checkpointPath)
	case sessions.BackendSQLite:
		fp = knowledgeHistoryFingerprintOf(root, filepath.Join(".runcode", "sessions.db"), "")
	default:
		return errKnowledgeHistoryFormat
	}
	if cached, ok := knowledgeHistoryCache.Load(cacheKey); ok {
		entry := cached.(knowledgeHistoryCacheEntry)
		if entry.fp == fp {
			return entry.err
		}
	}

	var checkErr error
	switch kind {
	case "", sessions.BackendJSONL:
		if err := checkKnowledgeHistoryFile(ctx, root, journalPath, false); err != nil {
			checkErr = err
			break
		}
		checkErr = checkKnowledgeHistoryFile(ctx, root, checkpointPath, true)
	case sessions.BackendSQLite:
		checkErr = checkKnowledgeSQLite(ctx, root, workspace, id, cpName)
	}
	// Cache successes and stable hard failures so repeated open/submit paths
	// do not re-scan. Context cancellation is not cached.
	if checkErr == nil || errors.Is(checkErr, errLegacyKnowledgeHistory) || errors.Is(checkErr, errKnowledgeHistoryFormat) {
		knowledgeHistoryCache.Store(cacheKey, knowledgeHistoryCacheEntry{fp: fp, err: checkErr})
	}
	return checkErr
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
	return checkKnowledgeHistoryJSONL(ctx, f)
}

// checkKnowledgeHistoryJSONL mirrors engine sessions.scanHistory: a crash can
// only tear the trailing unterminated line, which is dropped so the prefix stays
// usable. A newline-terminated corrupt line is real corruption.
func checkKnowledgeHistoryJSONL(ctx context.Context, r io.Reader) error {
	reader := bufio.NewReader(r)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := reader.ReadBytes('\n')
		atEOF := errors.Is(readErr, io.EOF)
		terminated := len(line) > 0 && line[len(line)-1] == '\n'
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			if err := checkKnowledgeRaw(ctx, trimmed, false); err != nil {
				if atEOF && !terminated {
					return nil
				}
				return err
			}
		}
		if readErr != nil {
			if atEOF {
				return ctx.Err()
			}
			return errKnowledgeHistoryFormat
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

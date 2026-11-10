package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	appwire "github.com/wt68/runcode/internal/protocol"
)

// 文生图的工作区内存储：会话开关 sidecar 与每次生成的提交账本。全部经 os.OpenRoot
// 限定在会话工作区内，写入走临时文件 + fsync + 原子替换；读到损坏内容一律按存储
// 失败处理，不当成"没有记录"。

var errImageGenerationStorage = errors.New("图片生成记录无法读取或保存，请检查工作区存储后重试")

const (
	imageGenerationStoreVersion = 1
	imageGenerationSidecarLimit = 64 * 1024
)

// validImageGenerationID accepts the opaque ids used as file names here
// (session, generation and turn ids): letters, digits, '_' and '-'.
func validImageGenerationID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func newImageGenerationID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// readRootedFile reads rel under workspace. exists=false is a genuinely absent
// file; an ancestor that is not a directory is a broken store, not absence
// (Windows reports ENOENT in that case).
func readRootedFile(workspace, rel string, limit int64) (data []byte, exists bool, err error) {
	if workspace == "" {
		return nil, false, errImageGenerationStorage
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, false, errImageGenerationStorage
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(rel)
	if errors.Is(err, os.ErrNotExist) {
		for parent := filepath.Dir(rel); ; parent = filepath.Dir(parent) {
			info, statErr := root.Stat(parent)
			if statErr == nil {
				if !info.IsDir() {
					return nil, false, errImageGenerationStorage
				}
				return nil, false, nil
			}
			if !errors.Is(statErr, os.ErrNotExist) || filepath.Dir(parent) == parent {
				return nil, false, errImageGenerationStorage
			}
		}
	}
	if err != nil {
		return nil, false, errImageGenerationStorage
	}
	defer func() { _ = f.Close() }()
	data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, true, errImageGenerationStorage
	}
	return data, true, nil
}

// writeRootedFileAtomic replaces rel with data durably: a reader sees either
// the previous or the new content, never a torn file.
func writeRootedFileAtomic(workspace, rel string, data []byte) error {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return errImageGenerationStorage
	}
	defer func() { _ = root.Close() }()
	if err := root.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
		return errImageGenerationStorage
	}
	tmp := rel + "." + rand.Text() + ".tmp"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errImageGenerationStorage
	}
	defer func() { _ = root.Remove(tmp) }()
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if errors.Join(writeErr, syncErr, closeErr) != nil {
		return errImageGenerationStorage
	}
	if err := root.Rename(tmp, rel); err != nil {
		return errImageGenerationStorage
	}
	return nil
}

// imageGenerationSwitch is one identity partition's session switch.
type imageGenerationSwitch struct {
	Enabled  bool  `json:"enabled"`
	Revision int64 `json:"revision"`
}

type imageGenerationSessionDisk struct {
	Version   int                              `json:"version"`
	SessionID string                           `json:"sessionId"`
	Scopes    map[string]imageGenerationSwitch `json:"scopes"`
}

func imageGenerationSessionPath(id string) (string, error) {
	if !validImageGenerationID(id) {
		return "", errImageGenerationStorage
	}
	return filepath.Join(".runcode", "image-generation", "sessions", id+".json"), nil
}

func readImageGenerationSession(workspace, id string) (imageGenerationSessionDisk, error) {
	disk := imageGenerationSessionDisk{Version: imageGenerationStoreVersion, SessionID: id, Scopes: map[string]imageGenerationSwitch{}}
	path, err := imageGenerationSessionPath(id)
	if err != nil {
		return disk, err
	}
	data, exists, err := readRootedFile(workspace, path, imageGenerationSidecarLimit)
	if err != nil || !exists {
		return disk, err
	}
	var decoded imageGenerationSessionDisk
	if json.Unmarshal(data, &decoded) != nil || decoded.Version != imageGenerationStoreVersion || decoded.SessionID != id || decoded.Scopes == nil {
		return disk, errImageGenerationStorage
	}
	for scope, sw := range decoded.Scopes {
		if _, err := hex.DecodeString(scope); err != nil || len(scope) != 64 || sw.Revision < 0 || sw.Revision == math.MaxInt64 {
			return disk, errImageGenerationStorage
		}
	}
	return decoded, nil
}

// createRootedFileExclusive creates rel with data and syncs it; an existing
// file is reported as os.ErrExist and never replaced.
func createRootedFileExclusive(workspace, rel string, data []byte) error {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return errImageGenerationStorage
	}
	defer func() { _ = root.Close() }()
	if err := root.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
		return errImageGenerationStorage
	}
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return os.ErrExist
	}
	if err != nil {
		return errImageGenerationStorage
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if errors.Join(writeErr, syncErr, closeErr) != nil {
		_ = root.Remove(rel)
		return errImageGenerationStorage
	}
	return nil
}

// —— 提交账本 ——
//
// 每次生成一个记录：.runcode/image-generation/requests/<session>/<generation>.json。
// POST 之前先写成 submitted 并 fsync；写不进去就不提交。另有每回合一个排他创建的
// 标记 .runcode/image-generation/turns/<session>/<turn>.json，它是跨工具实例、跨进程的
// 「本回合已经提交过」的权威依据，任何结果下都不删除。这是客户端本地防重，不是 Core
// 账务的原子性或幂等承诺。记录里不存令牌、Base64 或签名 URL。

var errImageGenerationTurnSpent = errors.New("本回合已经提交过一次图片生成")

const imageGenerationMsgInterrupted = "上次生成过程中断，结果无法确认，可能已生成或计费；不会自动重新生成"

type imageGenerationLedger struct {
	Version          int                            `json:"version"`
	GenerationID     string                         `json:"generationId"`
	SessionID        string                         `json:"sessionId"`
	TurnID           string                         `json:"turnId"`
	QuestionID       string                         `json:"questionId"`
	ToolUseID        string                         `json:"toolUseId"`
	ScopeID          string                         `json:"scopeId"`
	Model            string                         `json:"model"`
	Size             string                         `json:"size"`
	Status           string                         `json:"status"`
	Result           *appwire.ImageGenerationResult `json:"result,omitempty"`
	RequestID        string                         `json:"requestId,omitempty"`
	AccountingStatus string                         `json:"accountingStatus,omitempty"`
	SubmittedAt      time.Time                      `json:"submittedAt,omitzero"`
	UpdatedAt        time.Time                      `json:"updatedAt,omitzero"`
}

func normalizeAccountingStatus(s string) string {
	switch s {
	case "verified", "ambiguous", "observed_mismatch":
		return s
	default:
		return "unverified"
	}
}

func finalImageGenerationStatus(s string) bool {
	switch s {
	case "succeeded", "failed", "unknown", "cancelled", "save_failed":
		return true
	}
	return false
}

// finalized records result as this generation's outcome. The accounting
// observation stays whatever a person recorded: a file or HTTP outcome never
// upgrades or overwrites it.
func (rec imageGenerationLedger) finalized(result appwire.ImageGenerationResult) imageGenerationLedger {
	rec.Status = result.Status
	if result.RequestID != "" {
		rec.RequestID = result.RequestID
	}
	rec.AccountingStatus = normalizeAccountingStatus(rec.AccountingStatus)
	result.AccountingStatus = rec.AccountingStatus
	result.RequestID = rec.RequestID
	rec.Result = &result
	rec.UpdatedAt = time.Now().UTC()
	return rec
}

// effectiveResult is what the record means now. A submission without a final
// outcome (the process stopped, or it is still being written) is unknown: it
// may have been generated and charged, and it is never resubmitted.
func (rec imageGenerationLedger) effectiveResult() appwire.ImageGenerationResult {
	var out appwire.ImageGenerationResult
	if finalImageGenerationStatus(rec.Status) && rec.Result != nil {
		out = *rec.Result
		out.Status = rec.Status
	} else {
		out = appwire.ImageGenerationResult{Status: "unknown", Code: "unknown_outcome", Message: imageGenerationMsgInterrupted}
	}
	out.Version, out.GenerationID = 1, rec.GenerationID
	if out.Model == "" {
		out.Model = rec.Model
	}
	if out.Size == "" {
		out.Size = rec.Size
	}
	if out.Images == nil || out.Status != "succeeded" {
		out.Images = []appwire.ImageGenerationArtifact{}
	}
	out.RequestID = rec.RequestID
	out.AccountingStatus = normalizeAccountingStatus(rec.AccountingStatus)
	return out
}

func imageGenerationLedgerDir(sessionID string) (string, error) {
	if !validImageGenerationID(sessionID) {
		return "", errImageGenerationStorage
	}
	return filepath.Join(".runcode", "image-generation", "requests", sessionID), nil
}

func imageGenerationLedgerPath(sessionID, generationID string) (string, error) {
	dir, err := imageGenerationLedgerDir(sessionID)
	if err != nil || !validImageGenerationID(generationID) {
		return "", errImageGenerationStorage
	}
	return filepath.Join(dir, generationID+".json"), nil
}

// reserveImageGenerationTurn durably claims the turn's single attempt.
func reserveImageGenerationTurn(workspace, sessionID, turnID, generationID string) error {
	if !validImageGenerationID(sessionID) || !validImageGenerationID(turnID) || !validImageGenerationID(generationID) {
		return errImageGenerationStorage
	}
	rel := filepath.Join(".runcode", "image-generation", "turns", sessionID, turnID+".json")
	data, _ := json.Marshal(map[string]any{"version": imageGenerationStoreVersion, "turnId": turnID, "generationId": generationID, "reservedAt": time.Now().UTC()})
	err := createRootedFileExclusive(workspace, rel, data)
	if errors.Is(err, os.ErrExist) {
		return errImageGenerationTurnSpent
	}
	return err
}

func writeImageGenerationLedger(workspace string, rec imageGenerationLedger) error {
	path, err := imageGenerationLedgerPath(rec.SessionID, rec.GenerationID)
	if err != nil {
		return err
	}
	rec.Version = imageGenerationStoreVersion
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Now().UTC()
	}
	data, err := json.Marshal(rec)
	if err != nil || len(data) > imageGenerationSidecarLimit {
		return errImageGenerationStorage
	}
	return writeRootedFileAtomic(workspace, path, data)
}

func decodeImageGenerationLedger(data []byte, sessionID, generationID string) (imageGenerationLedger, error) {
	var rec imageGenerationLedger
	if json.Unmarshal(data, &rec) != nil || rec.Version != imageGenerationStoreVersion || rec.SessionID != sessionID || rec.GenerationID != generationID {
		return imageGenerationLedger{}, errImageGenerationStorage
	}
	return rec, nil
}

func readImageGenerationLedger(workspace, sessionID, generationID string) (imageGenerationLedger, error) {
	path, err := imageGenerationLedgerPath(sessionID, generationID)
	if err != nil {
		return imageGenerationLedger{}, err
	}
	data, exists, err := readRootedFile(workspace, path, imageGenerationSidecarLimit)
	if err != nil {
		return imageGenerationLedger{}, err
	}
	if !exists {
		return imageGenerationLedger{}, os.ErrNotExist
	}
	return decodeImageGenerationLedger(data, sessionID, generationID)
}

// listImageGenerationLedgers returns every record of a session. A damaged
// record is an error, not something to skip silently.
func listImageGenerationLedgers(workspace, sessionID string) ([]imageGenerationLedger, error) {
	dir, err := imageGenerationLedgerDir(sessionID)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, errImageGenerationStorage
	}
	defer func() { _ = root.Close() }()
	d, err := root.Open(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []imageGenerationLedger{}, nil
	}
	if err != nil {
		return nil, errImageGenerationStorage
	}
	entries, err := d.ReadDir(-1)
	_ = d.Close()
	if err != nil {
		return nil, errImageGenerationStorage
	}
	out := []imageGenerationLedger{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		generationID := strings.TrimSuffix(name, ".json")
		if !validImageGenerationID(generationID) {
			continue // atomic-write temporaries carry extra dots
		}
		rec, err := readImageGenerationLedger(workspace, sessionID, generationID)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func writeImageGenerationSession(workspace, id string, disk imageGenerationSessionDisk) error {
	path, err := imageGenerationSessionPath(id)
	if err != nil {
		return err
	}
	disk.Version, disk.SessionID = imageGenerationStoreVersion, id
	data, err := json.Marshal(disk)
	if err != nil || len(data) > imageGenerationSidecarLimit {
		return errImageGenerationStorage
	}
	return writeRootedFileAtomic(workspace, path, data)
}

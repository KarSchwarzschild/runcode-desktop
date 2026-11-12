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
	"unicode/utf8"
)

// Knowledge state lives outside engine metadata. No documents, credentials,
// login epochs or model inputs are persisted here.
type knowledgeSelection struct {
	Revision   int64              `json:"revision"`
	LibraryIDs []string           `json:"libraryIds"`
	Summaries  []knowledgeSummary `json:"summaries,omitempty"`
}

// knowledgeSummary 是重启显示用的最小标题摘要，不是授权证据。
type knowledgeSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type knowledgeDisk struct {
	Version    int                           `json:"version"`
	SessionID  string                        `json:"sessionId"`
	Selections map[string]knowledgeSelection `json:"selections"`
}

const (
	knowledgeStoreVersionV1   = 1
	knowledgeStoreVersionV2   = 2
	knowledgeSummaryTitleMax  = 512
	knowledgeSummaryDescMax   = 600
)

var errKnowledgeStorage = errors.New("知识库选择记录无法读取或保存，请修复存储后重试；本次消息未发送")

func knowledgePath(workspace, id string) (string, error) {
	if workspace == "" || id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\:") {
		return "", errKnowledgeStorage
	}
	return filepath.Join(".runcode", "knowledge", id+".json"), nil
}

func readKnowledge(workspace, id string) (knowledgeDisk, error) {
	disk := knowledgeDisk{Version: knowledgeStoreVersionV1, SessionID: id, Selections: make(map[string]knowledgeSelection)}
	path, err := knowledgePath(workspace, id)
	if err != nil {
		return disk, err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return disk, errKnowledgeStorage
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		// Windows can report ENOENT when an ancestor is a regular file.
		// That is a broken store, not a new session with no selection.
		for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
			info, statErr := root.Stat(parent)
			if statErr == nil {
				if !info.IsDir() {
					return disk, errKnowledgeStorage
				}
				break
			}
			if !errors.Is(statErr, os.ErrNotExist) || filepath.Dir(parent) == parent {
				return disk, errKnowledgeStorage
			}
		}
		return disk, nil
	}
	if err != nil {
		return disk, errKnowledgeStorage
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return disk, errKnowledgeStorage
	}
	var decoded knowledgeDisk
	if json.Unmarshal(data, &decoded) != nil || decoded.SessionID != id || decoded.Selections == nil {
		return disk, errKnowledgeStorage
	}
	if decoded.Version != knowledgeStoreVersionV1 && decoded.Version != knowledgeStoreVersionV2 {
		return disk, errKnowledgeStorage
	}
	disk = decoded
	for scope, selection := range disk.Selections {
		_, scopeErr := hex.DecodeString(scope)
		if len(scope) != 64 || scopeErr != nil || selection.Revision < 0 || selection.Revision == math.MaxInt64 || len(selection.LibraryIDs) > knowledgeMaxSelection {
			return disk, errKnowledgeStorage
		}
		seen := make(map[string]bool)
		for _, libraryID := range selection.LibraryIDs {
			if !validStoredKnowledgeID(libraryID) || seen[libraryID] {
				return disk, errKnowledgeStorage
			}
			seen[libraryID] = true
		}
		if err := validateKnowledgeSummaries(selection); err != nil {
			return disk, err
		}
	}
	return disk, nil
}

func validateKnowledgeSummaries(selection knowledgeSelection) error {
	if len(selection.Summaries) == 0 {
		return nil
	}
	if len(selection.Summaries) > knowledgeMaxSelection {
		return errKnowledgeStorage
	}
	byID := make(map[string]bool, len(selection.LibraryIDs))
	for _, id := range selection.LibraryIDs {
		byID[id] = true
	}
	seen := make(map[string]bool)
	for _, summary := range selection.Summaries {
		if !validStoredKnowledgeID(summary.ID) || !byID[summary.ID] || seen[summary.ID] {
			return errKnowledgeStorage
		}
		if strings.TrimSpace(summary.Title) == "" || utf8.RuneCountInString(summary.Title) > knowledgeSummaryTitleMax {
			return errKnowledgeStorage
		}
		if utf8.RuneCountInString(summary.Description) > knowledgeSummaryDescMax {
			return errKnowledgeStorage
		}
		seen[summary.ID] = true
	}
	return nil
}

func writeKnowledge(workspace, id string, disk knowledgeDisk) error {
	path, err := knowledgePath(workspace, id)
	if err != nil {
		return err
	}
	if disk.Version == 0 {
		disk.Version = knowledgeStoreVersionV1
	}
	data, err := json.Marshal(disk)
	if err != nil || len(data) > 64*1024 {
		return errKnowledgeStorage
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return errKnowledgeStorage
	}
	defer func() { _ = root.Close() }()
	if err := root.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errKnowledgeStorage
	}
	tmp := path + "." + rand.Text() + ".tmp"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errKnowledgeStorage
	}
	defer func() { _ = root.Remove(tmp) }()
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if errors.Join(writeErr, syncErr, closeErr) != nil {
		return errKnowledgeStorage
	}
	if err := root.Rename(tmp, path); err != nil {
		return errKnowledgeStorage
	}
	return nil
}

func deleteKnowledge(workspace, id string) error {
	path, err := knowledgePath(workspace, id)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return errKnowledgeStorage
	}
	defer func() { _ = root.Close() }()
	if err := root.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errKnowledgeStorage
	}
	return nil
}

func truncateRunes(value string, max int) string {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}

func knowledgeSummariesFromView(libraries []KnowledgeBaseSummary) []knowledgeSummary {
	out := make([]knowledgeSummary, 0, len(libraries))
	for _, library := range libraries {
		title := strings.TrimSpace(library.Title)
		if title == "" {
			title = "名称待恢复"
		}
		out = append(out, knowledgeSummary{
			ID:          library.ID,
			Title:       truncateRunes(title, knowledgeSummaryTitleMax),
			Description: truncateRunes(strings.TrimSpace(library.Description), knowledgeSummaryDescMax),
		})
	}
	return out
}

func knowledgeDetailsFromSummaries(summaries []knowledgeSummary, keep []string) map[string]KnowledgeBaseSummary {
	want := map[string]bool{}
	for _, id := range keep {
		want[id] = true
	}
	out := map[string]KnowledgeBaseSummary{}
	for _, summary := range summaries {
		if !want[summary.ID] {
			continue
		}
		title := strings.TrimSpace(summary.Title)
		if title == "" || title == knowledgeUnavailableTitle {
			continue
		}
		out[summary.ID] = KnowledgeBaseSummary{
			ID:          summary.ID,
			Title:       title,
			Description: summary.Description,
		}
	}
	return out
}

// knowledgeSummariesForPersist keeps prior titles when the view only has
// offline placeholders (remove-only saves without gateway revalidation).
func knowledgeSummariesForPersist(previous []knowledgeSummary, libraries []KnowledgeBaseSummary) []knowledgeSummary {
	prev := map[string]knowledgeSummary{}
	for _, summary := range previous {
		title := strings.TrimSpace(summary.Title)
		if title == "" || title == knowledgeUnavailableTitle {
			continue
		}
		prev[summary.ID] = summary
	}
	out := knowledgeSummariesFromView(libraries)
	for i := range out {
		title := strings.TrimSpace(out[i].Title)
		if title != "" && title != knowledgeUnavailableTitle && title != "名称待恢复" {
			continue
		}
		if prior, ok := prev[out[i].ID]; ok {
			out[i] = prior
		}
	}
	return out
}

func knowledgeViewFromSnapshot(sessionID string, selection knowledgeSelection) SessionKnowledgeView {
	byID := make(map[string]knowledgeSummary, len(selection.Summaries))
	for _, summary := range selection.Summaries {
		byID[summary.ID] = summary
	}
	libraries := make([]KnowledgeBaseSummary, 0, len(selection.LibraryIDs))
	for _, id := range selection.LibraryIDs {
		summary, ok := byID[id]
		title := "名称待恢复"
		description := ""
		if ok {
			title, description = summary.Title, summary.Description
		}
		libraries = append(libraries, KnowledgeBaseSummary{
			ID: id, Title: title, Description: description,
		})
	}
	return SessionKnowledgeView{SessionID: sessionID, Revision: selection.Revision, Libraries: libraries, Mode: "snapshot"}
}

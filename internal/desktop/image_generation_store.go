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

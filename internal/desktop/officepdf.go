package desktop

// Office 文档(pptx/docx/xlsx)的高保真预览:交给本机装着的 Office 套件转成 PDF,
// 再用 WebView 自带的 PDF 查看器显示。
//
// 内嵌的 JS 渲染器(pptx-preview / docx-preview / SheetJS)在文字度量、autofit 缩放
// 与中文字体回退这些基本面上就和真 Office 对不齐——实测一套只有矩形和纯色填充的
// 演示稿都差得明显,那不是调 CSS 能补的,是排版引擎的差别。把渲染交回给用户机器上
// 那个"标准答案",预览看到的就是他另开 WPS/PowerPoint 会看到的东西。
//
// 转换是尽力而为:没装 Office、调用失败、非 Windows 平台,一律返回错误,由前端退回
// JS 渲染器。**能力缺失从来不是错误路径**——降级方向是安全的,只是不够好看。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gitlab.ouc-online.com.cn/aibase/agentloop/host"
)

const (
	// previewCacheDir 放转换产物。在 .runcode 下,所以文件选择器、Glob/Grep 与
	// @ 引用清单都看不见它(那些都跳过 .runcode)。
	previewCacheDir = "preview-cache"
)

// errOfficeUnavailable 表示这台机器上没有能用的转换器(没装 Office,或平台不支持)。
// 前端据此静默退回 JS 渲染器,不当故障报。
var errOfficeUnavailable = errors.New("本机没有可用于转换的 Office 组件")

// officeConvertSlots 以可取消的等待串行化转换。Office 的 COM 自动化并不欢迎并发调用,而预览又常常
// 是"打开一个标签页"这种低频动作,排队比并发安全。
var officeConvertSlots = make(chan struct{}, 1)

// officeKind 按扩展名判断文档类型,空串表示不是可转换的 Office 文档。
func officeKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pptx", ".ppt":
		return "ppt"
	case ".docx", ".doc":
		return "doc"
	case ".xlsx", ".xls":
		return "xls"
	default:
		return ""
	}
}

// RenderOfficePDF 将来源引用的授权快照转成 PDF，返回绑定同一引用的受控 URL。
// 私有转换文件不对外服务；缓存命中与 HTTP 读取仍须检查源权限。
//
// 同一份文件重复打开命中缓存直接返回;源文件改了(大小或修改时间变了)键就变了,
// 于是自然重转,不需要额外的失效逻辑。
func (a *App) RenderOfficePDF(reference string) (string, error) {
	var result string
	err := a.withArtifact(context.Background(), reference, func(ctx context.Context, _ host.SessionAccess, e *artifactAccess) error {
		source := e.permit.Path()
		if officeKind(source) == "" {
			return errors.New("不是可转换的 Office 文档")
		}
		f, err := e.permit.Open(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if info.Size() > maxArtifactBinaryBytes {
			return errors.New("file too large to preview")
		}
		version := officeCacheName(source, info)
		if e.ref.URL == "" {
			return errOfficeUnavailable
		}
		result = strings.Split(e.ref.URL, "/file/")[0] + "/pdf"
		if len(e.pdf) > 0 && e.version == version {
			return nil
		}
		select {
		case officeConvertSlots <- struct{}{}:
			defer func() { <-officeConvertSlots }()
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Conversion receives an authorized snapshot, not a mutable source path.
		// Temporary files are private, unserved, and removed when conversion ends.
		dir, err := os.MkdirTemp("", "runcode-preview-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		input := filepath.Join(dir, "source"+strings.ToLower(filepath.Ext(source)))
		output := filepath.Join(dir, "preview.pdf")
		snapshot, err := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(snapshot, io.LimitReader(f, maxArtifactBinaryBytes+1))
		closeErr := snapshot.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n > maxArtifactBinaryBytes {
			return errors.New("file too large to preview")
		}
		if err := e.permit.Check(ctx); err != nil {
			return err
		}
		if err := convertOfficeToPDF(ctx, input, output); err != nil {
			return err
		}
		pdf, err := os.Open(output)
		if err != nil {
			return errOfficeUnavailable
		}
		defer func() { _ = pdf.Close() }()
		data, err := io.ReadAll(io.LimitReader(pdf, maxArtifactBinaryBytes+1))
		if err != nil {
			return err
		}
		if len(data) == 0 || len(data) > maxArtifactBinaryBytes {
			return errOfficeUnavailable
		}
		if err := e.permit.Check(ctx); err != nil {
			return err
		}
		current, err := os.Stat(source)
		if err != nil || officeCacheName(source, current) != version {
			return errors.New("文档已改变，请重新预览")
		}
		a.artifactMu.Lock()
		defer a.artifactMu.Unlock()
		live := a.artifacts[reference]
		if live == nil {
			return errors.New("预览已关闭")
		}
		// Bound all retained PDF bytes, separately from reference count.
		total := len(data)
		for id, old := range a.artifacts {
			if id != reference {
				total += len(old.pdf)
			}
		}
		if total > 64<<20 {
			for id, old := range a.artifacts {
				if id != reference {
					old.pdf = nil
					old.version = ""
				}
			}
		}
		live.pdf = data
		live.version = version
		return nil
	})
	if err != nil {
		return "", wireError(err)
	}
	return result, nil
}

// officeCacheName 由源文件的绝对路径 + 修改时间 + 大小算出缓存文件名。三者任一变化
// 都换一个名字,所以"源文件改了要重转"这件事由键本身保证,不必再写失效逻辑;旧的那份
// 由引用生命周期与总量上限淘汰。
func officeCacheName(absPath string, info os.FileInfo) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", strings.ToLower(absPath), info.ModTime().UnixNano(), info.Size())))
	return hex.EncodeToString(sum[:])[:16] + ".pdf"
}

// prunePreviewCache 只保留最近修改的 keep 个 PDF。删除失败无所谓——缓存本来就是
// 可再生的,清不掉的那个下次再试。
func prunePreviewCache(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type aged struct {
		path string
		mod  time.Time
	}
	files := make([]aged, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pdf") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, aged{path: filepath.Join(dir, e.Name()), mod: info.ModTime()})
	}
	if len(files) <= keep {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	for _, f := range files[keep:] {
		_ = os.Remove(f.path)
	}
}

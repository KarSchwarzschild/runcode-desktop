package imagegentool

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// canonicalizeSize 把上游常见的 1024*1024 写成内部统一的 1024x1024。
func canonicalizeSize(size string) string {
	size = strings.TrimSpace(strings.ToLower(size))
	size = strings.ReplaceAll(size, "*", "x")
	size = strings.ReplaceAll(size, "×", "x")
	size = strings.ReplaceAll(size, " ", "")
	return size
}

func parseWH(size string) (int, int, bool) {
	size = canonicalizeSize(size)
	w, h, ok := strings.Cut(size, "x")
	if !ok {
		return 0, 0, false
	}
	width, err1 := strconv.Atoi(w)
	height, err2 := strconv.Atoi(h)
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

// closestSupportedSize 在支持列表中选宽高比优先、像素量次之的最接近项。
func closestSupportedSize(want string, supported []string) string {
	if len(supported) == 0 {
		return ""
	}
	ww, wh, ok := parseWH(want)
	if !ok {
		return supported[0]
	}
	wantAspect := float64(ww) / float64(wh)
	wantPixels := float64(ww * wh)
	best := supported[0]
	bestScore := math.MaxFloat64
	for _, candidate := range supported {
		cw, ch, cok := parseWH(candidate)
		if !cok {
			continue
		}
		aspectDiff := math.Abs(float64(cw)/float64(ch) - wantAspect)
		pixelDiff := math.Abs(float64(cw*ch)-wantPixels) / math.Max(wantPixels, 1)
		score := aspectDiff*10 + pixelDiff
		if score < bestScore {
			bestScore = score
			best = canonicalizeSize(candidate)
		}
	}
	return best
}

func formatSupportedSizesNote(supported []string) string {
	if len(supported) == 0 {
		return "本模型暂未声明可用尺寸。"
	}
	parts := make([]string, 0, len(supported))
	for _, s := range supported {
		w, h, ok := parseWH(s)
		if !ok {
			parts = append(parts, s)
			continue
		}
		parts = append(parts, fmt.Sprintf("%d×%d（%d×%d 像素）", w, h, w, h))
	}
	return "本模型支持的尺寸：" + strings.Join(parts, "、") + "。"
}

func sizeRemapNote(requested, chosen string, supported []string) string {
	req := canonicalizeSize(requested)
	got := canonicalizeSize(chosen)
	if req == "" || req == got {
		return ""
	}
	return fmt.Sprintf("已将请求尺寸 %s 调整为最接近的支持尺寸 %s。%s请在最终回复末尾向用户说明上述支持尺寸。",
		req, got, formatSupportedSizesNote(supported))
}

package imagegentool

import (
	"strings"
	"testing"
)

func TestClosestSupportedSizePrefersAspectThenPixels(t *testing.T) {
	supported := []string{"1024x1024", "1280x720", "720x1280"}
	if got := closestSupportedSize("1920x1080", supported); got != "1280x720" {
		t.Fatalf("landscape closest = %s", got)
	}
	if got := closestSupportedSize("1080x1920", supported); got != "720x1280" {
		t.Fatalf("portrait closest = %s", got)
	}
	if got := closestSupportedSize("900x900", supported); got != "1024x1024" {
		t.Fatalf("square closest = %s", got)
	}
	if got := closestSupportedSize("1600*900", supported); got != "1280x720" {
		t.Fatalf("star syntax landscape closest = %s", got)
	}
}

func TestFormatSupportedSizesNote(t *testing.T) {
	note := formatSupportedSizesNote([]string{"1024x1024", "1280x720"})
	for _, part := range []string{"1024×1024", "1280×720", "像素"} {
		if !strings.Contains(note, part) {
			t.Fatalf("note missing %q: %q", part, note)
		}
	}
}

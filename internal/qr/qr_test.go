package qr

import (
	"strings"
	"testing"
)

func TestSVG(t *testing.T) {
	svg, err := SVG("https://example.test/l/abc", "#4f5b2a")
	if err != nil || !strings.Contains(string(svg), "<svg") {
		t.Fatalf("svg: %s %v", svg, err)
	}
	if _, err = SVG("https://example.test/", `bad" onload="x`); err == nil {
		t.Fatal("unsafe color accepted")
	}
}

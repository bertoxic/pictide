package qr

import (
	"fmt"
	"github.com/skip2/go-qrcode"
	"regexp"
	"strings"
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func SVG(url, color string) ([]byte, error) { return SVGWithBackground(url, color, "#ffffff") }
func SVGWithBackground(url, color, background string) ([]byte, error) {
	if !hexColor.MatchString(color) || !hexColor.MatchString(background) {
		return nil, fmt.Errorf("invalid QR color")
	}
	q, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	bits := q.Bitmap()
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="100%%" height="100%%" fill="%s"/><path fill="%s" d="`, len(bits), len(bits), background, color)
	for y, row := range bits {
		for x, on := range row {
			if on {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x, y)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return []byte(b.String()), nil
}

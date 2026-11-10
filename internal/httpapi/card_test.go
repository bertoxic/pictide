package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestCardValidation(t *testing.T) {
	text := map[string]any{"id": "title", "text": "Gallery", "x": .5, "y": .2, "size": 50, "color": "#252b23", "font": "Georgia", "align": "center", "visible": true, "bold": false, "rotation": 45, "opacity": 70}
	c := map[string]any{"format": "card", "background": "#f4f0e6", "qrColor": "#4f5b2a", "backgroundImage": "", "elements": []any{text}}
	raw, _ := json.Marshal(c)
	if !validCard(raw) {
		t.Fatal("valid legacy card rejected")
	}
	text["id"] = "bad' onclick='alert(1)"
	raw, _ = json.Marshal(c)
	if validCard(raw) {
		t.Fatal("unsafe element id accepted")
	}
}
func TestCardShapeAndLogoValidation(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	im.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	base := map[string]any{"text": "", "x": .5, "y": .5, "size": 20, "color": "#252b23", "font": "sans-serif", "align": "center", "visible": true, "bold": false, "width": .3, "height": .2}
	logo := map[string]any{}
	shape := map[string]any{}
	for k, v := range base {
		logo[k] = v
		shape[k] = v
	}
	logo["id"] = "custom123"
	logo["kind"] = "image"
	logo["imageData"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	shape["id"] = "custom456"
	shape["kind"] = "shape"
	shape["shape"] = "ellipse"
	shape["strokeColor"] = "#4f5b2a"
	shape["strokeWidth"] = 4
	shape["radius"] = 10
	c := map[string]any{"format": "landscape", "background": "#ffffff", "qrColor": "#4f5b2a", "qrBackground": "#ffffff", "backgroundImage": "", "elements": []any{logo, shape}}
	raw, _ := json.Marshal(c)
	if !validCard(raw) {
		t.Fatal("valid image and shape rejected")
	}
	logo["imageData"] = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>"))
	raw, _ = json.Marshal(c)
	if validCard(raw) {
		t.Fatal("SVG logo accepted")
	}
	logo["imageData"] = "data:image/png;base64," + strings.Repeat("a", 400001)
	raw, _ = json.Marshal(c)
	if validCard(raw) {
		t.Fatal("oversized image accepted")
	}
	logo["imageData"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	shape["shape"] = "unknown"
	raw, _ = json.Marshal(c)
	if validCard(raw) {
		t.Fatal("invalid shape accepted")
	}
}

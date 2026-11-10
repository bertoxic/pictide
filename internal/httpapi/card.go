package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
	"strings"
	"unicode/utf8"
)

var cardID = regexp.MustCompile(`^(eyebrow|title|subtitle|qr|url|custom[0-9]+)$`)
var cardColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var cardFonts = map[string]bool{"Georgia": true, "sans-serif": true, "monospace": true, "Arial": true, "Trebuchet MS": true, "Palatino": true, "Times New Roman": true}

type cardElement struct {
	ID, Text, Color, Font, Align, Kind, Shape, ImageData, StrokeColor             string
	X, Y, Size, Opacity, Rotation, Width, Height, StrokeWidth, Radius, LineHeight float64
	Bold, Italic, Visible, Shadow                                                 bool
}
type cardDocument struct {
	Format, Background, QRColor, BackgroundImage, BorderColor, QRBackground string
	Overlay, BorderWidth                                                    float64
	Elements                                                                []cardElement
}

func validCard(raw []byte) bool {
	var c cardDocument
	if json.Unmarshal(raw, &c) != nil || !map[string]bool{"card": true, "landscape": true, "a4": true, "a5": true, "sticker": true, "story": true}[c.Format] || !cardColor.MatchString(c.Background) || !cardColor.MatchString(c.QRColor) || len(c.Elements) < 1 || len(c.Elements) > 20 || c.Overlay < 0 || c.Overlay > 80 || c.BorderWidth < 0 || c.BorderWidth > 24 || (c.BorderColor != "" && !cardColor.MatchString(c.BorderColor)) || (c.QRBackground != "" && !cardColor.MatchString(c.QRBackground)) {
		return false
	}
	if c.BackgroundImage != "" && !validCardImage(c.BackgroundImage, 800000) {
		return false
	}
	seen := map[string]bool{}
	for _, e := range c.Elements {
		if !cardID.MatchString(e.ID) || seen[e.ID] || utf8.RuneCountInString(e.Text) > 150 || e.X < 0 || e.X > 1 || e.Y < 0 || e.Y > 1 || e.Size < 10 || e.Size > 360 || !cardColor.MatchString(e.Color) || !cardFonts[e.Font] || !map[string]bool{"left": true, "center": true, "right": true}[e.Align] || e.Opacity < 0 || e.Opacity > 100 || e.Rotation < -180 || e.Rotation > 180 || e.Width < 0 || e.Width > 0.95 || e.Height < 0 || e.Height > 0.95 || e.StrokeWidth < 0 || e.StrokeWidth > 30 || e.Radius < 0 || e.Radius > 100 || e.LineHeight < 0 || e.LineHeight > 2 || (e.StrokeColor != "" && !cardColor.MatchString(e.StrokeColor)) {
			return false
		}
		switch e.Kind {
		case "", "text":
			if e.ImageData != "" {
				return false
			}
		case "shape":
			if !map[string]bool{"rectangle": true, "ellipse": true, "line": true}[e.Shape] || e.ImageData != "" {
				return false
			}
		case "image":
			if !validCardImage(e.ImageData, 400000) {
				return false
			}
		default:
			return false
		}
		if e.ID == "qr" && e.Kind != "" {
			return false
		}
		seen[e.ID] = true
	}
	return true
}
func validCardImage(data string, max int) bool {
	var payload string
	switch {
	case strings.HasPrefix(data, "data:image/jpeg;base64,"):
		payload = strings.TrimPrefix(data, "data:image/jpeg;base64,")
	case strings.HasPrefix(data, "data:image/png;base64,"):
		payload = strings.TrimPrefix(data, "data:image/png;base64,")
	default:
		return false
	}
	if len(payload) > max {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	return err == nil && (format == "jpeg" || format == "png") && int64(cfg.Width)*int64(cfg.Height) <= 4000000
}

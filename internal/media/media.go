package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"

	"github.com/disintegration/imaging"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"
)

type Options struct {
	Watermark, Style, Color, BrandPosition                   string
	WatermarkOpacity, WatermarkSize, BrandOpacity, BrandSize int
	Brand                                                    image.Image
}
type Assets struct{ Preview, Thumb, CleanPreview, CleanThumb, Master []byte }

func encodeJPEG(img image.Image) ([]byte, error) {
	var b bytes.Buffer
	err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 88})
	return b.Bytes(), err
}
func encodePNG(img image.Image) ([]byte, error) {
	var b bytes.Buffer
	e := png.Encoder{CompressionLevel: png.BestCompression}
	err := e.Encode(&b, img)
	return b.Bytes(), err
}
func colorFor(s string, alpha int) color.NRGBA {
	var r, g, b uint8
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		r, g, b = 255, 255, 255
	}
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 100 {
		alpha = 100
	}
	return color.NRGBA{R: r, G: g, B: b, A: uint8(alpha * 255 / 100)}
}
func brand(base image.Image, opt Options) image.Image {
	out := imaging.Clone(base)
	if opt.Brand == nil {
		return out
	}
	w, h := out.Bounds().Dx(), out.Bounds().Dy()
	size := opt.BrandSize
	if size < 5 || size > 30 {
		size = 18
	}
	logo := imaging.Fit(opt.Brand, max(1, w*size/100), max(1, h*30/100), imaging.Linear)
	x, y := w-logo.Bounds().Dx(), h-logo.Bounds().Dy()
	switch opt.BrandPosition {
	case "top-left":
		x, y = 0, 0
	case "top-right":
		y = 0
	case "bottom-left":
		x = 0
	}
	opacity := math.Max(0, math.Min(1, float64(opt.BrandOpacity)/100))
	return imaging.Overlay(out, logo, image.Pt(x, y), opacity)
}
func watermark(base image.Image, opt Options) image.Image {
	if opt.Watermark == "" || opt.WatermarkOpacity == 0 {
		return base
	}
	w, h := base.Bounds().Dx(), base.Bounds().Dy()
	layer := image.NewNRGBA(image.Rect(0, 0, w, h))
	face := basicfont.Face7x13
	d := font.Drawer{Dst: layer, Src: image.NewUniform(colorFor(opt.Color, opt.WatermarkOpacity)), Face: face}
	width := d.MeasureString(opt.Watermark).Ceil()
	scale := max(1, w*max(1, opt.WatermarkSize)/100/13)
	x, y := w/2-width*scale/2, h/2
	drawText := func(px, py int) {
		small := image.NewNRGBA(image.Rect(0, 0, max(width, 1), 16))
		t := font.Drawer{Dst: small, Src: d.Src, Face: face, Dot: fixed.P(0, 13)}
		t.DrawString(opt.Watermark)
		larger := imaging.Resize(small, max(1, width*scale), 16*scale, imaging.Linear)
		draw.Draw(layer, image.Rect(px, py, px+larger.Bounds().Dx(), py+larger.Bounds().Dy()), larger, image.Point{}, draw.Over)
	}
	switch opt.Style {
	case "corner":
		drawText(w-width*scale-8, h-16*scale-8)
	case "tile":
		for i := -2; i <= 2; i++ {
			drawText(x, y+i*scale*45)
		}
	default:
		drawText(x, y)
	}
	if opt.Style == "diagonal" || opt.Style == "tile" {
		rot := imaging.Rotate(layer, 25, color.Transparent)
		layer = imaging.CropCenter(rot, w, h)
	}
	return imaging.Overlay(base, layer, image.Point{}, 1)
}
func Process(data []byte, opt Options) (Assets, error) {
	if len(data) == 0 || len(data) > 30*1024*1024 {
		return Assets{}, errors.New("invalid image size")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Assets{}, err
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return Assets{}, errors.New("unsupported image type")
	}
	if cfg.Width > 12000 || cfg.Height > 12000 || int64(cfg.Width)*int64(cfg.Height) > 80000000 {
		return Assets{}, errors.New("image dimensions exceed limit")
	}
	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return Assets{}, err
	}
	master := brand(img, opt)
	preview := imaging.Fit(master, 1600, 1600, imaging.Linear)
	proof := watermark(preview, opt)
	var a Assets
	if a.Master, err = encodePNG(master); err != nil {
		return a, err
	}
	if a.CleanPreview, err = encodeJPEG(preview); err != nil {
		return a, err
	}
	if a.Preview, err = encodeJPEG(proof); err != nil {
		return a, err
	}
	if a.CleanThumb, err = encodeJPEG(imaging.Fit(preview, 480, 480, imaging.Linear)); err != nil {
		return a, err
	}
	if a.Thumb, err = encodeJPEG(imaging.Fit(proof, 480, 480, imaging.Linear)); err != nil {
		return a, err
	}
	return a, nil
}

package media

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestProcessOutputsAndRejectsInvalid(t *testing.T) {
	var src bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 500, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 500; x++ {
			img.Set(x, y, color.NRGBA{R: 100, G: 120, B: 80, A: 255})
		}
	}
	if err := png.Encode(&src, img); err != nil {
		t.Fatal(err)
	}
	a, err := Process(src.Bytes(), Options{Watermark: "PROOF", Style: "tile", Color: "#ffffff", WatermarkOpacity: 50, WatermarkSize: 5})
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"master": a.Master, "proof": a.Preview, "thumb": a.Thumb, "clean": a.CleanPreview, "cleanthumb": a.CleanThumb} {
		if len(b) == 0 {
			t.Fatalf("%s missing", name)
		}
	}
	if bytes.Equal(a.Preview, a.CleanPreview) {
		t.Fatal("watermark missing")
	}
	if _, err = Process([]byte("not an image"), Options{}); err == nil {
		t.Fatal("invalid input accepted")
	}
}

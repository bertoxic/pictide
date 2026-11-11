//go:build face_recognition && cgo

package facesearch

import (
	"image/jpeg"
	"os"
	"testing"
)

// Run with explicitly provisioned runtime, models, and a face image.
func TestNativeEngineWithModels(t *testing.T) {
	runtimePath, detectorPath, recognizerPath, imagePath := os.Getenv("PICTIDE_FACE_RUNTIME"), os.Getenv("PICTIDE_FACE_DETECTOR"), os.Getenv("PICTIDE_FACE_RECOGNIZER"), os.Getenv("PICTIDE_FACE_TEST_IMAGE")
	if runtimePath == "" || detectorPath == "" || recognizerPath == "" || imagePath == "" {
		t.Skip("native runtime, models, and test image not configured")
	}
	e, err := NewEngine(runtimePath, detectorPath, recognizerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f, err := os.Open(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	faces, err := e.Detect(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(faces) == 0 || len(faces[0].Embedding) == 0 {
		t.Fatal("expected a face and embedding")
	}
	if score, ok := Similarity(faces[0].Embedding, faces[0].Embedding); !ok || score < 0.999 {
		t.Fatalf("invalid embedding: %v %v", score, ok)
	}
}

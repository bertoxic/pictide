//go:build !face_recognition || !cgo

package facesearch

import (
	"errors"
	"image"
)

// Engine is unavailable unless Pictide is built with -tags face_recognition
// and CGO enabled. ONNX Runtime is a native dependency.
type Engine struct{}

func NewEngine(runtimePath, detectorPath, recognizerPath string) (*Engine, error) {
	return nil, errors.New("face recognition requires a CGO build with the face_recognition tag")
}

func (e *Engine) Detect(_ image.Image) ([]Detection, error) {
	return nil, errors.New("face recognition is unavailable")
}
func (e *Engine) Close() {}

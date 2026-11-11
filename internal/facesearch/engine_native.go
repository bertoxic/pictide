//go:build face_recognition && cgo

package facesearch

import (
	"fmt"
	"sync"

	"github.com/leandroveronezi/go-onnxface/face"
	"github.com/leandroveronezi/go-onnxface/sface"
	"github.com/leandroveronezi/go-onnxface/yunet"
)

// Engine owns an ONNX detector and recognizer. Calls are serialized because
// the underlying inference sessions share reusable input and output buffers.
type Engine struct {
	mu         sync.Mutex
	detector   *yunet.Detector
	recognizer *sface.Recognizer
}

// NewEngine loads explicitly provisioned model and runtime files. The ONNX
// environment is process-global and must only be initialized once.
func NewEngine(runtimePath, detectorPath, recognizerPath string) (*Engine, error) {
	if !face.IsInitialized() {
		if err := face.InitEnvironment(runtimePath); err != nil {
			return nil, fmt.Errorf("initialize ONNX runtime: %w", err)
		}
	}
	detector, err := yunet.NewDetector(detectorPath)
	if err != nil {
		return nil, fmt.Errorf("load face detector: %w", err)
	}
	recognizer, err := sface.NewRecognizer(recognizerPath)
	if err != nil {
		detector.Close()
		return nil, fmt.Errorf("load face recognizer: %w", err)
	}
	return &Engine{detector: detector, recognizer: recognizer}, nil
}

// Detect extracts faces and embeddings from an image.
func (e *Engine) Detect(img image.Image) ([]Detection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	faces, err := e.detector.Detect(img)
	if err != nil {
		return nil, err
	}
	result := make([]Detection, 0, len(faces))
	for _, f := range faces {
		feature, err := e.recognizer.Feature(e.recognizer.Align(img, f.Landmarks))
		if err != nil {
			return nil, err
		}
		result = append(result, Detection{Rectangle: f.Rectangle, Embedding: feature})
	}
	return result, nil
}

// Close releases the sessions owned by the engine.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.detector.Close()
	e.recognizer.Close()
}

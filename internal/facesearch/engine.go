package facesearch

import "image"

// Detection is a face's bounding rectangle and recognition embedding.
type Detection struct {
	Rectangle image.Rectangle
	Embedding []float32
}

// Detector is the portion of a recognition engine used by the HTTP service.
// It keeps the optional native dependency explicit and testable.
type Detector interface {
	Detect(image.Image) ([]Detection, error)
	Close()
}

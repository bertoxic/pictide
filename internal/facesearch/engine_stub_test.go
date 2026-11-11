//go:build !face_recognition || !cgo

package facesearch

import "testing"

func TestNativeEngineRequiresExplicitBuild(t *testing.T) {
	if _, err := NewEngine("runtime", "detector", "recognizer"); err == nil {
		t.Fatal("default build must not silently enable native face recognition")
	}
}

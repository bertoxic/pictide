package facesearch

import (
	"math"
	"reflect"
	"testing"
)

func TestSearchDeduplicatesAndRanksPhotos(t *testing.T) {
	faces := []IndexedFace{
		{PhotoID: "b", Embedding: []float32{0.8, 0.6}},
		{PhotoID: "a", Embedding: []float32{1, 0}},
		{PhotoID: "a", Embedding: []float32{0.9, 0.1}},
		{PhotoID: "c", Embedding: []float32{0, 1}},
	}
	if got := Search([]float32{1, 0}, faces, 0.7); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("unexpected matches: %v", got)
	}
}

func TestSimilarity(t *testing.T) {
	tests := []struct {
		a, b  []float32
		want  float64
		valid bool
	}{
		{[]float32{1, 2}, []float32{2, 4}, 1, true},
		{[]float32{1, 0}, []float32{0, 1}, 0, true},
		{[]float32{1}, []float32{-1}, -1, true},
		{nil, nil, 0, false},
		{[]float32{1}, []float32{1, 2}, 0, false},
		{[]float32{0}, []float32{1}, 0, false},
		{[]float32{float32(math.NaN())}, []float32{1}, 0, false},
	}
	for _, tt := range tests {
		got, valid := Similarity(tt.a, tt.b)
		if valid != tt.valid || valid && math.Abs(got-tt.want) > 1e-6 {
			t.Errorf("Similarity(%v, %v) = %v, %v; want %v, %v", tt.a, tt.b, got, valid, tt.want, tt.valid)
		}
	}
}

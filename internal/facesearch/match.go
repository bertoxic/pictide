// Package facesearch contains gallery-scoped face matching primitives.
package facesearch

import (
	"math"
	"sort"
)

// IndexedFace represents one face detected in a photo. Callers must supply
// faces from a single authorized gallery and a single model version.
type IndexedFace struct {
	PhotoID   string
	Embedding []float32
}

// Search returns each matching photo once, ordered by its best matching face.
func Search(query []float32, faces []IndexedFace, threshold float64) []string {
	best := make(map[string]float64)
	for _, f := range faces {
		if f.PhotoID == "" {
			continue
		}
		score, valid := Similarity(query, f.Embedding)
		if !valid || score < threshold {
			continue
		}
		if previous, exists := best[f.PhotoID]; !exists || score > previous {
			best[f.PhotoID] = score
		}
	}
	ids := make([]string, 0, len(best))
	for id := range best {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if best[ids[i]] == best[ids[j]] {
			return ids[i] < ids[j]
		}
		return best[ids[i]] > best[ids[j]]
	})
	return ids
}

// Similarity returns cosine similarity for two valid face embeddings.
// Embeddings from different models must never be compared.
func Similarity(a, b []float32) (float64, bool) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, false
	}
	var dot, aa, bb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
			return 0, false
		}
		dot += x * y
		aa += x * x
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return 0, false
	}
	return dot / (math.Sqrt(aa) * math.Sqrt(bb)), true
}

package pipeline

// simhash computes a locality-sensitive hash for short Chinese text
// using character bigram minhash (fast, zero-dependency).
func simhash(text string) uint64 {
	runes := []rune(text)
	if len(runes) < 2 {
		return uint64(len(runes))
	}

	var h uint64
	for i := 0; i < len(runes)-1; i++ {
		// Combine two adjacent runes into a 32-bit feature then hash
		feature := uint64(runes[i])<<16 | uint64(runes[i+1])
		h ^= feature * 0x9e3779b97f4a7c15
		h = (h << 13) | (h >> 51)
	}
	return h
}

// HammingDistance between two uint64 hashes.
func HammingDistance(a, b uint64) int {
	diff := a ^ b
	dist := 0
	for diff != 0 {
		dist++
		diff &= diff - 1
	}
	return dist
}

// SimilarityRatio estimates similarity from Hamming distance (0.0 ~ 1.0).
func SimilarityRatio(a, b uint64) float64 {
	dist := HammingDistance(a, b)
	return 1.0 - float64(dist)/64.0
}

// RecentPhrases tracks recent outputs for deduplication.
type RecentPhrases struct {
	hashes []uint64
	max    int
}

func NewRecentPhrases(max int) *RecentPhrases {
	return &RecentPhrases{hashes: make([]uint64, 0, max), max: max}
}

// IsSimilar checks if text is too similar to any recent output.
// Threshold: similarity > 0.8 (Hamming distance < 13 for 64-bit hash).
func (r *RecentPhrases) IsSimilar(text string) bool {
	h := simhash(text)
	for _, prev := range r.hashes {
		if SimilarityRatio(h, prev) > 0.8 {
			return true
		}
	}
	r.hashes = append(r.hashes, h)
	if len(r.hashes) > r.max {
		r.hashes = r.hashes[1:]
	}
	return false
}

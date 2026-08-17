package cdx

import (
	"bytes"
	"sort"
	"testing"
)

// TestNumericKeyRoundTrip covers positive, negative, zero, and
// fractional values.
func TestNumericKeyRoundTrip(t *testing.T) {
	values := []float64{0, 1, -1, 100, -100, 1.5, -1.5, 3.14159, -3.14159, 922337203685477.5807}
	for _, v := range values {
		enc := EncodeNumericKey(v)
		if len(enc) != 8 {
			t.Fatalf("EncodeNumericKey(%v) = %d bytes, want 8", v, len(enc))
		}
		got, err := DecodeNumericKey(enc)
		if err != nil {
			t.Fatalf("DecodeNumericKey: %v", err)
		}
		if got != v {
			t.Errorf("round trip %v -> %x -> %v", v, enc, got)
		}
	}
}

// TestNumericKeyByteOrderMatchesNumericOrder is the entire point
// of the transform: sorting the encoded byte strings must produce
// the same order as sorting the original numbers, including
// across the negative/positive boundary. This is the case
// ndx.EncodeNumericKey (plain IEEE) gets wrong for negatives.
func TestNumericKeyByteOrderMatchesNumericOrder(t *testing.T) {
	values := []float64{-1000, -100.5, -1, -0.001, 0, 0.001, 1, 100.5, 1000}
	type pair struct {
		v   float64
		enc []byte
	}
	pairs := make([]pair, len(values))
	for i, v := range values {
		pairs[i] = pair{v, EncodeNumericKey(v)}
	}
	// Shuffle by reversing, then sort by encoded bytes only.
	for i, j := 0, len(pairs)-1; i < j; i, j = i+1, j-1 {
		pairs[i], pairs[j] = pairs[j], pairs[i]
	}
	sort.Slice(pairs, func(i, j int) bool {
		return bytes.Compare(pairs[i].enc, pairs[j].enc) < 0
	})
	for i, p := range pairs {
		if p.v != values[i] {
			t.Errorf("position %d: byte-sorted gives %v, want %v (numeric order)", i, p.v, values[i])
		}
	}
}

// TestNumericKeyDiffersFromPlainEncoding guards against reusing
// ndx's plain encoding by mistake — the two must not agree, or
// this codec is not doing anything.
func TestNumericKeyDiffersFromPlainEncoding(t *testing.T) {
	enc := EncodeNumericKey(-100)
	// Plain little-endian IEEE bits for -100, as ndx would produce.
	plainNeg100 := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x59, 0xC0}
	if bytes.Equal(enc, plainNeg100) {
		t.Error("CDX/IDX numeric encoding matches the plain NDX encoding; the transform is not being applied")
	}
}

// TestDecodeNumericKeyRejectsShortInput guards the width check.
func TestDecodeNumericKeyRejectsShortInput(t *testing.T) {
	if _, err := DecodeNumericKey([]byte{1, 2, 3}); err == nil {
		t.Error("DecodeNumericKey accepted a short key")
	}
}

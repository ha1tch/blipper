package cdx

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Transformed numeric key encoding for CDX and compact/uncompressed
// IDX.
//
// This is deliberately not the same as ntx/ndx's plain
// little-endian IEEE double. CDX and IDX store numeric keys
// transformed so that an unsigned byte comparison of the stored
// form yields the correct numeric order:
//
//  1. Convert the value to an IEEE-754 double.
//  2. Reverse the byte order (the in-memory Intel/little-endian
//     form becomes big-endian, left-to-right).
//  3. If the value is negative, invert all 64 bits. Otherwise
//     invert only the leftmost (sign) bit.
//
// Confirmed 2026-07-24 against a primary Microsoft source (VFP 7.0
// archived documentation, "Index File Structure (.idx)": see
// docs/INDEX_FORMATS.md), which states this exact algorithm and
// notes it is shared between plain/uncompressed IDX and CDX.
//
// The hazard this exists to prevent: ndx.EncodeNumericKey produces
// a different, plain encoding for the same Go float64 input.
// Using the wrong one is the class of bug that corrupts only
// negative values and survives casual testing, because unsigned
// byte comparison of the untransformed form still agrees with
// numeric order for same-sign values.
const numericKeyWidth = 8

// EncodeNumericKey produces the transformed stored form of a
// numeric key for CDX or compact/uncompressed IDX.
func EncodeNumericKey(v float64) []byte {
	bits := math.Float64bits(v)
	var raw [8]byte
	binary.LittleEndian.PutUint64(raw[:], bits)

	// Reverse byte order: Intel (little-endian in-memory) to
	// left-to-right (big-endian) storage order.
	var out [8]byte
	for i := 0; i < 8; i++ {
		out[i] = raw[7-i]
	}

	if v < 0 {
		for i := range out {
			out[i] = ^out[i]
		}
	} else {
		out[0] ^= 0x80 // invert only the leftmost (sign) bit
	}
	return out[:]
}

// DecodeNumericKey reverses EncodeNumericKey.
func DecodeNumericKey(key []byte) (float64, error) {
	if len(key) < numericKeyWidth {
		return 0, fmt.Errorf("cdx: numeric key is %d bytes, want %d", len(key), numericKeyWidth)
	}
	var work [8]byte
	copy(work[:], key[:8])

	// The sign bit of the *stored* form tells us which inversion
	// was applied: a stored leftmost bit of 0 means the original
	// value was negative (all bits inverted, so a positive sign
	// bit 1 became 0); a stored leftmost bit of 1 means the
	// original was non-negative (only the sign bit was flipped,
	// 0 -> 1).
	negative := work[0]&0x80 == 0

	if negative {
		for i := range work {
			work[i] = ^work[i]
		}
	} else {
		work[0] ^= 0x80
	}

	// Reverse byte order back to little-endian.
	var le [8]byte
	for i := 0; i < 8; i++ {
		le[i] = work[7-i]
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(le[:])), nil
}

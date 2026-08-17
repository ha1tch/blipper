package mdx

import (
	"fmt"
	"math"
)

// Numeric key encoding.
//
// Derived empirically 2026-07-24 by cross-referencing every
// numeric-tagged .MDX in dbf/testdata/dbase5/full/ against the
// actual field values in the paired .DBF (dBASE stores Numeric
// fields as plain ASCII text, so ground truth was directly
// readable). Verified against all 44 available keys across two
// tags of different width/decimals (CODES.MDX/AREACODE, width 3
// dec 0; ACCT_REC.MDX/OLDBALANCE, width 9 dec 2) — 44/44 exact
// matches. This is a third, distinct encoding from both ndx's
// plain IEEE double and cdx/idx's byte-reversed transformed
// double; no documentation describing it was found anywhere this
// session, including Microsoft's own FoxPro-family archives, which
// do not cover dBASE-lineage MDX at all.
//
// Layout, 12 bytes as observed (tag header ItemSize was 16 in
// both specimens; the format below fills the first 4 bytes and
// pads the rest with zero):
//
//	byte 0    biased decimal exponent: floor(log10(|v|)) + 53
//	byte 1    0x29 constant marker; bit 0x80 set if negative
//	byte 2-3  four significant decimal digits, BCD nibble-packed,
//	          from the normalized mantissa (1.xxx * 10^exp)
//	byte 4-11 zero
//
// Zero is a sentinel, not the general formula evaluated at v=0:
// byte 0 = 0x34, byte 1 = 0x01, rest zero. Confirmed against
// exactly one specimen (ACCT_REC.DBF record 4).
//
// Scope, deliberately bounded to what was verified:
//
//   - Only values representable in 4 significant decimal digits
//     are supported. Encode returns an error rather than a
//     rounded, silently-wrong key for anything else — this
//     project's repeated lesson is that a plausible wrong encoding
//     passes casual testing and corrupts comparisons quietly, and
//     this is exactly that hazard shape.
//   - The zero sentinel and the marker byte's meaning are
//     confirmed by pattern, not by any specification; both hold
//     across all 44 keys with no exception, which is strong
//     evidence but not certainty.
//   - Untested: values needing 5+ significant digits, tags with
//     ItemSize other than 16 (which might indicate different
//     mantissa precision), and magnitudes at the edges of a
//     single-byte biased exponent's range.
const (
	numericBias         = 53
	numericMarker       = 0x29
	numericSignBit      = 0x80
	numericZeroExponent = 0x34
	numericZeroMarker   = 0x01
)

// EncodeNumericKey produces the 12-byte MDX numeric key for v.
//
// Returns an error if v needs more than 4 significant decimal
// digits to represent exactly — the boundary of what the oracle
// specimens verify. Silently rounding would produce a key that
// looks plausible and sorts wrong.
func EncodeNumericKey(v float64) ([]byte, error) {
	out := make([]byte, 12)
	if v == 0 {
		out[0] = numericZeroExponent
		out[1] = numericZeroMarker
		return out, nil
	}

	neg := v < 0
	av := math.Abs(v)
	exp := int(math.Floor(math.Log10(av)))
	mantissa := av / math.Pow(10, float64(exp))

	digits := math.Round(mantissa * 1000) // 4 significant digits, e.g. 1.25 -> 1250
	if digits >= 10000 {
		// Rounding pushed the mantissa to the next power of ten
		// (e.g. 9.9996 -> 10.00): renormalize.
		digits /= 10
		exp++
	}

	// Reject anything the 4-digit mantissa cannot represent
	// exactly, rather than silently rounding.
	reconstructed := digits / 1000 * math.Pow(10, float64(exp))
	if neg {
		reconstructed = -reconstructed
	}
	if reconstructed != v {
		return nil, fmt.Errorf("%w (%v)", ErrNumericUnsupported, v)
	}

	biased := exp + numericBias
	if biased < 0 || biased > 255 {
		return nil, fmt.Errorf("mdx: %v is out of the verified exponent range", v)
	}

	d := uint32(digits)
	d0, d1, d2, d3 := (d/1000)%10, (d/100)%10, (d/10)%10, d%10

	out[0] = byte(biased)
	out[1] = numericMarker
	if neg {
		out[1] |= numericSignBit
	}
	out[2] = byte(d0<<4 | d1)
	out[3] = byte(d2<<4 | d3)
	return out, nil
}

// DecodeNumericKey reverses EncodeNumericKey.
func DecodeNumericKey(key []byte) (float64, error) {
	if len(key) < 4 {
		return 0, fmt.Errorf("%w: numeric key is %d bytes, want at least 4", ErrCorrupt, len(key))
	}
	if key[0] == numericZeroExponent && key[1] == numericZeroMarker {
		return 0, nil
	}
	neg := key[1]&numericSignBit != 0
	marker := key[1] &^ numericSignBit
	if marker != numericMarker {
		return 0, fmt.Errorf("%w: numeric key marker byte is 0x%02x, want 0x%02x",
			ErrCorrupt, marker, numericMarker)
	}
	exp := int(key[0]) - numericBias
	d0 := key[2] >> 4
	d1 := key[2] & 0x0F
	d2 := key[3] >> 4
	d3 := key[3] & 0x0F
	digits := float64(d0)*1000 + float64(d1)*100 + float64(d2)*10 + float64(d3)
	mantissa := digits / 1000
	v := mantissa * math.Pow(10, float64(exp))
	if neg {
		v = -v
	}
	return v, nil
}

// compareKeys orders two keys according to the tag's key type.
// Character and Date keys compare as bytes, matching the format
// directly. Numeric keys do NOT compare correctly as bytes — the
// exponent byte grows with magnitude regardless of sign, so a
// naive byte comparison sorts -1000 after -1 — and must be decoded
// first.
func compareKeys(keyType byte, a, b []byte) (int, error) {
	if keyType != KeyNumeric {
		return bytesCompare(a, b), nil
	}
	va, err := DecodeNumericKey(a)
	if err != nil {
		return 0, err
	}
	vb, err := DecodeNumericKey(b)
	if err != nil {
		return 0, err
	}
	switch {
	case va < vb:
		return -1, nil
	case va > vb:
		return 1, nil
	default:
		return 0, nil
	}
}

func bytesCompare(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	default:
		return 0
	}
}

package dbf

import (
	"fmt"
	"math"
	"strconv"
)

// Clipper renders numbers to fixed-width ASCII in two closely related
// ways, and this file is the single implementation of both so they
// cannot drift apart.
//
//   - In a DBF record a numeric field is right aligned and SPACE
//     padded: a value of 7.25 in an 8-byte, 2-decimal field is
//     "    7.25".
//   - In an NTX key the same number is ZERO padded, because the key
//     is compared bytewise and a leading space (0x20) would sort
//     below a digit: the same value becomes "00007.25".
//
// Both forms are verified against Clipper 5.2e output; see
// docs/CLIPPER_ORACLE.md.

// NumericPad selects the padding a rendered number receives.
type NumericPad byte

const (
	// PadSpace is the DBF record convention.
	PadSpace NumericPad = ' '

	// PadZero is the NTX key convention.
	PadZero NumericPad = '0'
)

// FormatNumeric renders value into exactly width bytes with the given
// decimal count, right aligned and padded per pad.
//
// The sign, if any, is written immediately before the first digit, so
// a negative value consumes one byte of the width. Callers that need
// Clipper's negative-key collation apply ApplyNegativeKeyTransform to
// the rendering of the absolute value instead; see NegativeKeyDigit.
func FormatNumeric(value float64, width, decimals int, pad NumericPad) ([]byte, error) {
	if width <= 0 {
		return nil, fmt.Errorf("numeric width %d must be positive", width)
	}

	if decimals < 0 {
		return nil, fmt.Errorf("numeric decimals %d must not be negative", decimals)
	}

	if decimals > 0 && decimals+2 > width {
		return nil, fmt.Errorf(
			"%d decimals need at least %d bytes, width is %d",
			decimals, decimals+2, width,
		)
	}

	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, fmt.Errorf("cannot encode %v as a numeric field", value)
	}

	text := strconv.FormatFloat(value, 'f', decimals, 64)

	if len(text) > width {
		return nil, fmt.Errorf(
			"%w: value %s does not fit %d bytes",
			ErrNumericOverflow, text, width,
		)
	}

	out := make([]byte, width)
	fill := width - len(text)

	for i := 0; i < fill; i++ {
		out[i] = byte(pad)
	}

	copy(out[fill:], text)

	// Zero padding must not be written to the left of a minus sign:
	// "-0007.25" is wrong, and Clipper never produces it, because
	// negative keys use the digit transform instead of a sign. Reject
	// rather than emit something that would collate incorrectly.
	if pad == PadZero && fill > 0 && out[fill] == '-' {
		return nil, fmt.Errorf(
			"%w: negative values cannot be zero padded; "+
				"use the negative key transform instead",
			ErrUnsupported,
		)
	}

	return out, nil
}

// ParseNumeric reads a fixed-width ASCII numeric field. Blank fields
// decode as zero, matching Clipper, which treats an uninitialized
// numeric as 0.
func ParseNumeric(raw []byte) (float64, error) {
	text := trimSpaces(raw)

	if text == "" {
		return 0, nil
	}

	// Clipper fills a numeric field with asterisks when a value is
	// too wide for it. Report that distinctly: the stored value is
	// genuinely unrecoverable, not merely malformed.
	if allAsterisks(text) {
		return 0, fmt.Errorf("%w: field holds an overflow marker", ErrNumericOverflow)
	}

	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidRecord, text)
	}

	return value, nil
}

// NegativeKeyDigit is Clipper's transform for the digits of a negative
// numeric index key: out = 0x5C - in, applied to every byte of the
// rendered absolute value except the decimal point.
//
// The result is nine's complement expressed in ASCII, shifted down by
// four, so '0' becomes ',' (0x2C) and '9' becomes '#' (0x23). Because
// ',' < '.' < '0', every negative key sorts below every positive one,
// and within negatives a larger magnitude sorts first. Verified
// against Clipper 5.2e; see docs/CLIPPER_ORACLE.md §9.
func NegativeKeyDigit(c byte) byte {
	if c == '.' {
		return c
	}

	return 0x5C - c
}

// ApplyNegativeKeyTransform maps NegativeKeyDigit over key in place
// and returns it.
func ApplyNegativeKeyTransform(key []byte) []byte {
	for i, c := range key {
		key[i] = NegativeKeyDigit(c)
	}

	return key
}

func trimSpaces(raw []byte) string {
	start := 0
	end := len(raw)

	for start < end && raw[start] == ' ' {
		start++
	}

	for end > start && raw[end-1] == ' ' {
		end--
	}

	return string(raw[start:end])
}

func allAsterisks(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] != '*' {
			return false
		}
	}

	return len(text) > 0
}

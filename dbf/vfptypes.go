package dbf

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// Visual FoxPro fixed-width binary field types.
//
// The dBASE III+ types blipper started with are all ASCII: a
// Numeric is a right-aligned decimal string, a Date is "YYYYMMDD",
// a Logical is one of T/F/Y/N. The VFP types added here are
// binary, fixed-width, and little-endian, which the format
// documentation states explicitly: "Integers in table files are
// stored with the least significant byte first."
//
// Five are implemented. Four — Currency, Integer, Double, General
// — because Microsoft's own VFP 3.0 distribution contains real
// examples of each: 13 Currency, 4 Integer, 3 Double, 3 General
// fields across 136 vendor-written files. See docs/VFP30_FORMAT.md
// for provenance and dbf/testdata/vfp/ for specimens.
//
// DateTime (T) was added 2026-07-24, resolving what had been the
// session's longest-standing gap. No VFP 3.0 distribution specimen
// carries the type, so the epoch went undetermined for most of the
// session. It was settled by two independent findings converging:
// dBASE 7's own documentation states its @ Timestamp epoch
// explicitly (Julian day since 1 January 4713 BC, plus
// milliseconds since midnight — see docs/DBASE_FORMAT.md), and a
// real VFP 9 specimen — photos.dbf in the ha1tch/VPFX-Samples
// mirror, column CREATED — decodes under that exact formula to
// 2004-10-12, 14:03/14:20/14:22: three photos taken minutes apart,
// which is what real sample-data timestamps look like, not what a
// coincidence looks like. VFP 9 shipped in 2004. See
// docs/RESEARCH_NOTES.md for the full derivation.
//
// _NullFlags remains unimplemented. Its position and per-field bit
// layout are documented (docs/VFP30_FORMAT.md), including the
// bitmap-width-tracks-nullable-count rule found from the same
// Northwind specimens — but ordering *across* different fields in
// the bitmap is not established, and guessing it risks exactly the
// silent-corruption failure this project has repeatedly avoided.

// VFP field type codes.
const (
	// Integer is a 4-byte signed little-endian integer.
	// Range -2147483647..2147483647 per the documentation, which
	// is one short of the two's-complement minimum.
	Integer FieldType = 'I'

	// Double is an 8-byte IEEE-754 binary64.
	Double FieldType = 'B'

	// Currency is a 64-bit signed integer scaled by 10000, giving
	// four implied decimal places.
	Currency FieldType = 'Y'

	// General is a 4-byte pointer into the memo file, referencing
	// an OLE object. Read as a block number; blipper does not
	// interpret the OLE payload.
	General FieldType = 'G'

	// Blob is a 4-byte pointer into the memo file, identical in
	// encoding to General — confirmed 2026-07-24 against a real
	// specimen: photos.dbf's MEDIA field (type W) pointed at FPT
	// blocks whose payload was raw BMP and JPEG bytes (block
	// signature 1, the same "generic" signature Memo uses), with
	// no distinct signature or wrapper for Blob content. So this
	// is not a new format — it shares General's exact codec.
	Blob FieldType = 'W'

	// Varchar and Varbinary are fixed-width on disk — the DBF
	// format has no true variable-length field — space-padded
	// exactly like Character (confirmed 2026-07-24 by a worked,
	// byte-exact example in a book chapter on VFP 9's new types;
	// an earlier note in this codebase, from misreading a
	// different source's comparison-semantics statement as a
	// storage-layout one, wrongly said the pad byte was NUL).
	// When content is shorter than the field, the actual length
	// is stored in the field's own last byte, and a bit pair in
	// _NullFlags records whether the field is "full" — see
	// dbf/nullflags.go, which implements the bit algorithm this
	// depends on.
	//
	// Decoded here as simple space-trimmed content — correct
	// whenever the field is full, and correct for the common
	// not-full case, but not exact if the stored value legitimately
	// ends in significant spaces (VFP treats deliberately-included
	// trailing spaces as real content for these types). Getting
	// that exactly right needs the not-full/length-byte mechanism
	// wired through, which needs _NullFlags decoded before the
	// field it governs — a decodeRecord restructuring not done
	// this session. Tracked as the residual part of T-35.
	Varchar   FieldType = 'V'
	Varbinary FieldType = 'Q'

	// DateTime is an 8-byte value: two little-endian uint32
	// fields, a Julian day number then milliseconds since
	// midnight. See the package doc for the epoch's provenance.
	DateTime FieldType = 'T'
)

// currencyScale is the divisor implied by the Currency type.
//
// No source states it directly. It follows from the documented
// range: ±922337203685477.5807 is exactly (2^63 - 1) / 10^4, so
// the stored value is a 64-bit integer with four implied decimals.
const currencyScale = 10000

// Currency width in bytes, from the documentation's size column.
const (
	integerWidth  = 4
	doubleWidth   = 8
	currencyWidth = 8
	generalWidth  = 4
	dateTimeWidth = 8
)

// CurrencyValue is a Currency field's value: a fixed-point decimal
// with exactly four fractional digits.
//
// It is kept as the raw scaled integer rather than converted to a
// float, because a float64 cannot represent every value in the
// documented range exactly — the range needs 63 bits and a
// float64 mantissa carries 53. Converting on read would silently
// lose precision on monetary data, which is the one kind of data
// where that is least acceptable.
type CurrencyValue int64

// Float64 returns the value as a float64, accepting the precision
// loss that implies for large amounts.
func (c CurrencyValue) Float64() float64 {
	return float64(c) / currencyScale
}

// String formats the value with its four decimal places.
func (c CurrencyValue) String() string {
	neg := c < 0
	v := int64(c)
	if neg {
		v = -v
	}
	whole, frac := v/currencyScale, v%currencyScale
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%04d", sign, whole, frac)
}

// Scaled returns the underlying scaled integer, for callers doing
// exact arithmetic.
func (c CurrencyValue) Scaled() int64 { return int64(c) }

// NewCurrency builds a CurrencyValue from a whole and fractional
// part, where frac is in ten-thousandths.
func NewCurrency(whole, frac int64) CurrencyValue {
	return CurrencyValue(whole*currencyScale + frac)
}

// decodeVFPValue decodes the binary VFP types. Returns ok=false
// for any type it does not handle, so the caller can fall through
// to the ASCII decoders.
func decodeVFPValue(raw []byte, field Field) (value any, ok bool, err error) {
	switch field.Type {
	case Integer:
		if len(raw) < integerWidth {
			return nil, true, fmt.Errorf("%w: Integer field is %d bytes, want %d",
				ErrInvalidRecord, len(raw), integerWidth)
		}
		return int32(binary.LittleEndian.Uint32(raw[:integerWidth])), true, nil

	case Double:
		if len(raw) < doubleWidth {
			return nil, true, fmt.Errorf("%w: Double field is %d bytes, want %d",
				ErrInvalidRecord, len(raw), doubleWidth)
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(raw[:doubleWidth])), true, nil

	case Currency:
		if len(raw) < currencyWidth {
			return nil, true, fmt.Errorf("%w: Currency field is %d bytes, want %d",
				ErrInvalidRecord, len(raw), currencyWidth)
		}
		return CurrencyValue(int64(binary.LittleEndian.Uint64(raw[:currencyWidth]))), true, nil

	case General, Blob:
		if len(raw) < generalWidth {
			return nil, true, fmt.Errorf("%w: General field is %d bytes, want %d",
				ErrInvalidRecord, len(raw), generalWidth)
		}
		// A General field holds a memo block number, the same as
		// a Memo field but binary rather than ASCII-padded. Zero
		// means no object.
		return binary.LittleEndian.Uint32(raw[:generalWidth]), true, nil

	case DateTime:
		if len(raw) < dateTimeWidth {
			return nil, true, fmt.Errorf("%w: DateTime field is %d bytes, want %d",
				ErrInvalidRecord, len(raw), dateTimeWidth)
		}
		day := binary.LittleEndian.Uint32(raw[0:4])
		ms := binary.LittleEndian.Uint32(raw[4:8])
		if day == 0 && ms == 0 {
			return time.Time{}, true, nil // zero value: no date stored
		}
		return decodeJulianDateTime(day, ms), true, nil
	}
	return nil, false, nil
}

// encodeVFPValue encodes the binary VFP types. Returns ok=false
// for types it does not handle.
func encodeVFPValue(dst []byte, field Field, value any) (ok bool, err error) {
	switch field.Type {
	case Integer:
		if len(dst) < integerWidth {
			return true, fmt.Errorf("Integer field is %d bytes, want %d", len(dst), integerWidth)
		}
		n, err := toInt64(value)
		if err != nil {
			return true, err
		}
		if n > math.MaxInt32 || n < math.MinInt32 {
			return true, fmt.Errorf("%d out of range for an Integer field", n)
		}
		binary.LittleEndian.PutUint32(dst[:integerWidth], uint32(int32(n)))
		return true, nil

	case Double:
		if len(dst) < doubleWidth {
			return true, fmt.Errorf("Double field is %d bytes, want %d", len(dst), doubleWidth)
		}
		f, err := toFloat64(value)
		if err != nil {
			return true, err
		}
		binary.LittleEndian.PutUint64(dst[:doubleWidth], math.Float64bits(f))
		return true, nil

	case Currency:
		if len(dst) < currencyWidth {
			return true, fmt.Errorf("Currency field is %d bytes, want %d", len(dst), currencyWidth)
		}
		var scaled int64
		switch v := value.(type) {
		case CurrencyValue:
			scaled = int64(v)
		case float64:
			// Rounding rather than truncating: a value that came
			// from a float is already approximate, and truncation
			// would bias every conversion downward.
			scaled = int64(math.Round(v * currencyScale))
		default:
			n, err := toInt64(value)
			if err != nil {
				return true, fmt.Errorf("cannot store %T in a Currency field", value)
			}
			scaled = n * currencyScale
		}
		binary.LittleEndian.PutUint64(dst[:currencyWidth], uint64(scaled))
		return true, nil

	case General, Blob:
		if len(dst) < generalWidth {
			return true, fmt.Errorf("General field is %d bytes, want %d", len(dst), generalWidth)
		}
		n, err := toInt64(value)
		if err != nil {
			return true, err
		}
		binary.LittleEndian.PutUint32(dst[:generalWidth], uint32(n))
		return true, nil

	case DateTime:
		if len(dst) < dateTimeWidth {
			return true, fmt.Errorf("DateTime field is %d bytes, want %d", len(dst), dateTimeWidth)
		}
		t, ok := value.(time.Time)
		if !ok {
			return true, fmt.Errorf("cannot store %T in a DateTime field, want time.Time", value)
		}
		if t.IsZero() {
			// Leave both fields 0x00000000: the documented "no
			// date stored" representation.
			return true, nil
		}
		day, ms := encodeJulianDateTime(t)
		binary.LittleEndian.PutUint32(dst[0:4], day)
		binary.LittleEndian.PutUint32(dst[4:8], ms)
		return true, nil
	}
	return false, nil
}

// decodeJulianDateTime converts a Julian day number (days since 1
// January 4713 BC) and milliseconds-since-midnight into a Go
// time.Time. Standard Fliegel & Van Flandern algorithm.
//
// This is the exact formula dBASE 7's documentation states for its
// @ Timestamp type (docs/DBASE_FORMAT.md), confirmed to be VFP
// DateTime's encoding too by decoding a real VFP 9 specimen
// (photos.dbf in the ha1tch/VPFX-Samples mirror) and finding it
// produces plausible, closely-spaced photo timestamps rather than
// nonsense — see docs/RESEARCH_NOTES.md for the full check.
func decodeJulianDateTime(day, ms uint32) time.Time {
	a := int64(day) + 32044
	b := (4*a + 3) / 146097
	c := a - (146097*b)/4
	d := (4*c + 3) / 1461
	e := c - (1461*d)/4
	m := (5*e + 2) / 153
	dayOfMonth := e - (153*m+2)/5 + 1
	month := m + 3 - 12*(m/10)
	year := 100*b + d - 4800 + m/10

	msRemainder := ms % 1000
	totalSec := ms / 1000
	hh := totalSec / 3600
	mm := (totalSec % 3600) / 60
	ss := totalSec % 60

	return time.Date(int(year), time.Month(month), int(dayOfMonth),
		int(hh), int(mm), int(ss), int(msRemainder)*int(time.Millisecond),
		time.UTC)
}

// encodeJulianDateTime is decodeJulianDateTime's inverse: the
// standard Gregorian-to-Julian-day-number conversion.
func encodeJulianDateTime(t time.Time) (day, ms uint32) {
	t = t.UTC()
	y, mo, d := int64(t.Year()), int64(t.Month()), int64(t.Day())
	a := (14 - mo) / 12
	y2 := y + 4800 - a
	m2 := mo + 12*a - 3
	jdn := d + (153*m2+2)/5 + 365*y2 + y2/4 - y2/100 + y2/400 - 32045

	msVal := uint32(t.Hour())*3600000 + uint32(t.Minute())*60000 +
		uint32(t.Second())*1000 + uint32(t.Nanosecond()/int(time.Millisecond))
	return uint32(jdn), msVal
}

// isVFPType reports whether a field type is one of the binary VFP
// types handled here.
func isVFPType(t FieldType) bool {
	switch t {
	case Integer, Double, Currency, General, DateTime:
		return true
	}
	return false
}

// vfpFieldWidth returns the fixed on-disk width of a VFP type.
// A file declaring a different width for one of these is
// malformed; the widths are not configurable.
func vfpFieldWidth(t FieldType) (int, bool) {
	switch t {
	case Integer:
		return integerWidth, true
	case Double:
		return doubleWidth, true
	case Currency:
		return currencyWidth, true
	case General, Blob:
		return generalWidth, true
	case DateTime:
		return dateTimeWidth, true
	}
	return 0, false
}

// toInt64 coerces a value to int64, reusing the package's existing
// intValue coercion rather than adding a second set of rules for
// the same question.
func toInt64(v any) (int64, error) {
	n, ok := intValue(v)
	if !ok {
		return 0, fmt.Errorf("cannot store %T in an integer field", v)
	}
	return n, nil
}

// toFloat64 coerces a numeric value to float64.
func toFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	default:
		i, err := toInt64(v)
		if err != nil {
			return 0, fmt.Errorf("cannot store %T in a numeric field", v)
		}
		return float64(i), nil
	}
}

// validVFPValue reports whether value is an acceptable Go
// representation for one of the VFP binary types, mirroring what
// encodeVFPValue actually accepts (via toInt64/toFloat64) so that
// Record.Set and Table.Append agree on what is valid rather than
// Set rejecting something Append would have encoded correctly, or
// the reverse.
//
// This exists because record.go's validValue switch was never
// extended when the VFP types were added — every prior test for
// Integer/Double/Currency/General exercised decodeVFPValue/
// encodeVFPValue directly, never the public Set/Append path, so
// the gap went uncaught until DateTime's test used Set for the
// first time and every VFP-typed Set turned out to fail silently
// (Set's default case returned false, so the field kept its
// pre-Set nil value with no visible error at the call site
// unless the caller checked Set's return, which the DateTime
// test initially did not either).
func validVFPValue(fieldType FieldType, value any) bool {
	switch fieldType {
	case Integer, Double, General:
		switch value.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			return true
		}
		return false

	case Currency:
		switch value.(type) {
		case CurrencyValue,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			return true
		}
		return false

	case DateTime:
		_, ok := value.(time.Time)
		return ok
	}
	return false
}

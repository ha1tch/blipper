package dbf

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestOpensVendorVFPSpecimens is the point of staging the fixtures:
// these are Microsoft's own files, extracted from the VFP 3.0
// distribution, and blipper must open them and read every record.
func TestOpensVendorVFPSpecimens(t *testing.T) {
	for _, name := range []string{"30DBC.DBF", "30PJX.DBF", "26PJX.DBF"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "vfp", name))
			if err != nil {
				t.Skipf("fixture unavailable: %v", err)
			}
			if data[0] != 0x30 {
				t.Fatalf("fixture version byte = 0x%02X, want 0x30", data[0])
			}
			tbl, err := Open(&memFile{data: data})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			for i := uint32(1); i <= tbl.RecordCount(); i++ {
				if _, err := tbl.Get(i); err != nil {
					t.Fatalf("Get(%d): %v", i, err)
				}
			}
			t.Logf("%d records, %d fields", tbl.RecordCount(), len(tbl.Schema().Fields))
		})
	}
}

// TestVFPTypesRoundTrip covers each binary type through encode and
// decode at the boundaries that matter.
func TestVFPTypesRoundTrip(t *testing.T) {
	t.Run("Integer", func(t *testing.T) {
		for _, want := range []int32{0, 1, -1, math.MaxInt32, -math.MaxInt32} {
			buf := make([]byte, 4)
			f := Field{Name: "N", Type: Integer, Length: 4}
			if _, err := encodeVFPValue(buf, f, int64(want)); err != nil {
				t.Fatalf("encode(%d): %v", want, err)
			}
			got, _, err := decodeVFPValue(buf, f)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.(int32) != want {
				t.Errorf("Integer round trip: got %d, want %d", got, want)
			}
		}
	})

	t.Run("Double", func(t *testing.T) {
		for _, want := range []float64{0, 1.5, -1.5, 3.141592653589793, 8.9884656743115e307} {
			buf := make([]byte, 8)
			f := Field{Name: "D", Type: Double, Length: 8}
			if _, err := encodeVFPValue(buf, f, want); err != nil {
				t.Fatalf("encode(%v): %v", want, err)
			}
			got, _, _ := decodeVFPValue(buf, f)
			if got.(float64) != want {
				t.Errorf("Double round trip: got %v, want %v", got, want)
			}
		}
	})

	t.Run("Currency", func(t *testing.T) {
		// The documented range endpoints, which are exactly
		// (2^63-1)/10^4 — the derivation the scale comes from.
		for _, want := range []CurrencyValue{
			0, 1, -1, 10000, NewCurrency(1234, 5678),
			CurrencyValue(math.MaxInt64), CurrencyValue(math.MinInt64 + 1),
		} {
			buf := make([]byte, 8)
			f := Field{Name: "C", Type: Currency, Length: 8}
			if _, err := encodeVFPValue(buf, f, want); err != nil {
				t.Fatalf("encode(%v): %v", want, err)
			}
			got, _, _ := decodeVFPValue(buf, f)
			if got.(CurrencyValue) != want {
				t.Errorf("Currency round trip: got %v, want %v", got, want)
			}
		}
	})

	t.Run("General", func(t *testing.T) {
		for _, want := range []uint32{0, 1, 4294967295} {
			buf := make([]byte, 4)
			f := Field{Name: "G", Type: General, Length: 4}
			if _, err := encodeVFPValue(buf, f, int64(want)); err != nil {
				t.Fatalf("encode(%d): %v", want, err)
			}
			got, _, _ := decodeVFPValue(buf, f)
			if got.(uint32) != want {
				t.Errorf("General round trip: got %d, want %d", got, want)
			}
		}
	})
}

// TestCurrencyPrecision guards the reason CurrencyValue is a scaled
// integer rather than a float64: the documented range needs 63 bits
// and a float64 mantissa carries 53, so converting on read would
// silently lose precision on monetary data.
func TestCurrencyPrecision(t *testing.T) {
	// The documented maximum, ±922337203685477.5807.
	max := CurrencyValue(math.MaxInt64)
	if got := max.String(); got != "922337203685477.5807" {
		t.Errorf("max Currency = %s, want 922337203685477.5807", got)
	}
	// A value a float64 cannot hold exactly.
	exact := NewCurrency(922337203685477, 5807)
	if exact.Scaled() != math.MaxInt64 {
		t.Errorf("NewCurrency lost precision: %d != %d", exact.Scaled(), int64(math.MaxInt64))
	}
	// Round-tripping through float64 must lose it, which is why
	// the type does not do that.
	viaFloat := CurrencyValue(int64(exact.Float64() * currencyScale))
	if viaFloat == exact {
		t.Error("float64 round trip preserved the value; the test no longer demonstrates the hazard")
	}
}

func TestCurrencyFormatting(t *testing.T) {
	cases := map[CurrencyValue]string{
		0: "0.0000", 1: "0.0001", 10000: "1.0000",
		-1: "-0.0001", -10000: "-1.0000",
		NewCurrency(1234, 5678): "1234.5678",
	}
	for v, want := range cases {
		if got := v.String(); got != want {
			t.Errorf("CurrencyValue(%d).String() = %q, want %q", int64(v), got, want)
		}
	}
}

// TestIntegerRangeIsEnforced verifies the encode side refuses
// values a 4-byte field cannot hold, rather than truncating.
func TestIntegerRangeIsEnforced(t *testing.T) {
	buf := make([]byte, 4)
	f := Field{Name: "N", Type: Integer, Length: 4}
	if _, err := encodeVFPValue(buf, f, int64(math.MaxInt32)+1); err == nil {
		t.Error("encoding an out-of-range Integer succeeded; want error")
	}
}

// TestVFPTypesAreSupportedOnOpen confirms the field-type gate lets
// these through, which is what makes a 0x30 table openable.
func TestVFPTypesAreSupportedOnOpen(t *testing.T) {
	for _, ft := range []FieldType{Integer, Double, Currency, General} {
		if !isSupportedType(ft) {
			t.Errorf("field type %c is not accepted by isSupportedType", ft)
		}
		if !isVFPType(ft) {
			t.Errorf("field type %c is not recognised as a VFP type", ft)
		}
		if w, ok := vfpFieldWidth(ft); !ok || w == 0 {
			t.Errorf("field type %c has no fixed width", ft)
		}
	}
}

// TestVFPValuesSettableThroughPublicAPI is a regression test.
// Every prior VFP-type test exercised decodeVFPValue/
// encodeVFPValue directly; none went through the public
// Record.Set / Table.Append path, so Set silently rejected valid
// Integer/Double/Currency/General values (its type-check switch
// had never been extended for them) while returning an error that
// went unchecked in exactly the tests that would have caught it.
func TestVFPValuesSettableThroughPublicAPI(t *testing.T) {
	f := &memFile{}
	tbl, err := Create(f, Schema{Fields: []Field{
		{Name: "N", Type: Integer, Length: 4},
		{Name: "D", Type: Double, Length: 8},
		{Name: "C", Type: Currency, Length: 8},
		{Name: "G", Type: General, Length: 4},
	}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rec := NewRecord(tbl.Schema())
	if err := rec.Set(tbl.Schema(), "N", int32(42)); err != nil {
		t.Fatalf("Set(Integer): %v", err)
	}
	if err := rec.Set(tbl.Schema(), "D", 3.14); err != nil {
		t.Fatalf("Set(Double): %v", err)
	}
	if err := rec.Set(tbl.Schema(), "C", NewCurrency(19, 9900)); err != nil {
		t.Fatalf("Set(Currency): %v", err)
	}
	if err := rec.Set(tbl.Schema(), "G", uint32(7)); err != nil {
		t.Fatalf("Set(General): %v", err)
	}
	if _, err := tbl.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := tbl.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v, _ := got.Get(tbl.Schema(), "N"); v != int32(42) {
		t.Errorf("N = %v, want 42", v)
	}
	if v, _ := got.Get(tbl.Schema(), "D"); v != 3.14 {
		t.Errorf("D = %v, want 3.14", v)
	}
}

// TestBlobDecodesAsGeneralPointer confirms Blob shares General's
// exact encoding, verified against photos.dbf's real MEDIA field
// (type W) — three records whose raw 4-byte values point at real
// BMP and JPEG payloads in the paired FPT, confirmed by fetching
// photos.fpt directly and checking the block signature and magic
// bytes (docs/VFP30_FORMAT.md / docs/RESEARCH_NOTES.md).
func TestBlobDecodesAsGeneralPointer(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "vfp", "PHOTOS.DBF"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	const (
		headerSize = 520
		recordSize = 65
		mediaDisp  = 35
	)
	want := []uint32{11, 951, 11631} // block numbers confirmed against photos.fpt
	for i, w := range want {
		off := headerSize + i*recordSize + mediaDisp
		field := Field{Type: Blob, Length: 4}
		v, err := decodeValue(raw[off:off+4], field)
		if err != nil {
			t.Fatalf("record %d: decodeValue: %v", i+1, err)
		}
		got, ok := v.(uint32)
		if !ok {
			t.Fatalf("record %d: decoded as %T, want uint32", i+1, v)
		}
		if got != w {
			t.Errorf("record %d: block = %d, want %d", i+1, got, w)
		}
	}
}

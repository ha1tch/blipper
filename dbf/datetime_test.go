package dbf

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDateTimeOracle decodes CREATED from the real VFP 9 specimen
// that settled the epoch question: photos.dbf,
// ha1tch/VPFX-Samples/Solution/Europa/photos.dbf. Three records
// must decode to 2004-10-12 at three closely-spaced times — the
// pattern that proved the formula rather than merely being
// consistent with it.
//
// Reads the raw field bytes directly rather than through dbf.Open:
// the file also carries a General/OLE-adjacent MEDIA field of
// type 'W' (Blob), a VFP 9 type this package does not implement
// and which is unrelated to DateTime. That is a real, separate gap
// — not something to route around by relaxing schema validation —
// so this test isolates the field it is actually checking.
func TestDateTimeOracle(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "vfp", "PHOTOS.DBF"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	const (
		headerSize   = 520
		recordSize   = 65
		createdDisp  = 53 // field displacement, includes the delete-flag byte
		dateTimeSize = 8
	)
	want := []struct {
		h, m, s int
	}{{14, 3, 30}, {14, 20, 6}, {14, 22, 5}}

	for i, w := range want {
		off := headerSize + i*recordSize + createdDisp
		field := raw[off : off+dateTimeSize]
		day := uint32(field[0]) | uint32(field[1])<<8 | uint32(field[2])<<16 | uint32(field[3])<<24
		ms := uint32(field[4]) | uint32(field[5])<<8 | uint32(field[6])<<16 | uint32(field[7])<<24
		dt := decodeJulianDateTime(day, ms)

		if dt.Year() != 2004 || dt.Month() != time.October || dt.Day() != 12 {
			t.Errorf("record %d date = %v, want 2004-10-12", i+1, dt)
		}
		if dt.Hour() != w.h || dt.Minute() != w.m || dt.Second() != w.s {
			t.Errorf("record %d time = %02d:%02d:%02d, want %02d:%02d:%02d",
				i+1, dt.Hour(), dt.Minute(), dt.Second(), w.h, w.m, w.s)
		}
	}
}

// TestDateTimeRoundTrip covers encode/decode symmetry across a
// spread of dates, including the zero value.
func TestDateTimeRoundTrip(t *testing.T) {
	cases := []time.Time{
		time.Date(2004, time.October, 12, 14, 3, 30, 0, time.UTC),
		time.Date(1999, time.December, 31, 23, 59, 59, 500*int(time.Millisecond), time.UTC),
		time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC),
	}
	for _, want := range cases {
		day, ms := encodeJulianDateTime(want)
		got := decodeJulianDateTime(day, ms)
		if !got.Equal(want) {
			t.Errorf("round trip %v -> day=%d ms=%d -> %v", want, day, ms, got)
		}
	}
}

// TestDateTimeZeroValue confirms the documented "no date" encoding
// — both fields zero — round-trips as Go's zero time.Time rather
// than decoding through the Julian formula (which would otherwise
// produce a nonsense proleptic date for day=0).
func TestDateTimeZeroValue(t *testing.T) {
	f := &memFile{}
	tbl, err := Create(f, Schema{Fields: []Field{
		{Name: "WHEN", Type: DateTime, Length: 8},
	}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rec := NewRecord(tbl.Schema())
	rec.Set(tbl.Schema(), "WHEN", time.Time{})
	if _, err := tbl.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := tbl.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	v, err := got.Get(tbl.Schema(), "WHEN")
	if err != nil {
		t.Fatalf("Get(WHEN): %v", err)
	}
	dt, ok := v.(time.Time)
	if !ok {
		t.Fatalf("WHEN is %T, want time.Time", v)
	}
	if !dt.IsZero() {
		t.Errorf("zero-stored DateTime decoded as %v, want zero time.Time", dt)
	}
}

// TestDateTimeRejectsWrongType guards the encode path.
func TestDateTimeRejectsWrongType(t *testing.T) {
	f := &memFile{}
	tbl, err := Create(f, Schema{Fields: []Field{
		{Name: "WHEN", Type: DateTime, Length: 8},
	}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rec := NewRecord(tbl.Schema())
	rec.Set(tbl.Schema(), "WHEN", "not a time")
	if _, err := tbl.Append(rec); err == nil {
		t.Error("Append with a non-time.Time DateTime value succeeded")
	}
}

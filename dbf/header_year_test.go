package dbf

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHeaderYearPivotDecode verifies the Y2K windowing pivot at
// byte 80. Bytes below the pivot decode as 2000+y (Clipper's
// mod-100 output); bytes at or above decode as 1900+y (legacy
// dBASE III+ convention).
func TestHeaderYearPivotDecode(t *testing.T) {
	cases := []struct {
		yearByte byte
		want     int
		name     string
	}{
		{0, 2000, "byte 0 = year 2000 (mod-100 post-Y2K)"},
		{26, 2026, "byte 26 = year 2026 (Clipper today, oracle-verified)"},
		{79, 2079, "byte 79 = year 2079 (last mod-100 before pivot)"},
		{80, 1980, "byte 80 = year 1980 (first legacy at pivot)"},
		{91, 1991, "byte 91 = year 1991 (corpus 1990s)"},
		{98, 1998, "byte 98 = year 1998 (corpus 1990s)"},
		{99, 1999, "byte 99 = year 1999 (legacy)"},
		{109, 2009, "byte 109 = year 2009 (corpus post-2000 with legacy encoder)"},
	}
	for _, c := range cases {
		got := decodeHeaderDate(c.yearByte, 1, 1)
		if got.Year() != c.want {
			t.Errorf("%s: decoded year = %d, want %d", c.name, got.Year(), c.want)
		}
	}
}

// TestHeaderYearMod100Encode verifies encode writes year mod 100,
// matching Clipper 5.2e's behavior.
func TestHeaderYearMod100Encode(t *testing.T) {
	cases := []struct {
		year     int
		wantByte byte
	}{
		{1980, 80},
		{1999, 99},
		{2000, 0},
		{2026, 26},
		{2099, 99},
		{2100, 0},
	}
	for _, c := range cases {
		var dst [3]byte
		encodeHeaderDate(time.Date(c.year, 6, 15, 0, 0, 0, 0, time.UTC), dst[:])
		if dst[0] != c.wantByte {
			t.Errorf("year %d encoded as byte %d, want %d (mod-100)",
				c.year, dst[0], c.wantByte)
		}
	}
}

// TestHeaderYearRoundTripToday round-trips today's date. Under the
// pivot rule, encode(today) → mod-100 byte, decode(same byte) →
// today's year (as long as today is 2000..2079).
func TestHeaderYearRoundTripToday(t *testing.T) {
	now := time.Now()
	if now.Year() < 2000 || now.Year() >= 2080 {
		t.Skip("round trip only holds inside the 2000..2079 window without a legacy encoder")
	}
	var dst [3]byte
	encodeHeaderDate(now, dst[:])
	back := decodeHeaderDate(dst[0], dst[1], dst[2])
	if back.Year() != now.Year() {
		t.Errorf("round trip: encoded %d as byte %d, decoded as %d",
			now.Year(), dst[0], back.Year())
	}
}

// TestHeaderYearMatchesCorpusUMDBF decodes the UM.DBF fixture
// (a 1990s Clipper POS/MTS file) and verifies the last-updated
// stamp comes out in the 1990s under the pivot rule.
//
// UM.DBF's header bytes 1-3 are 62 0A 0C = year byte 0x62 = 98,
// month 0x0A = 10, day 0x0C = 12 → 1998-10-12 (via 1900+98).
func TestHeaderYearMatchesCorpusUMDBF(t *testing.T) {
	path := filepath.Join("testdata", "UM.DBF")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tbl, err := Open(&memFile{data: data})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got := tbl.Header().LastUpdate
	want := time.Date(1998, 10, 12, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("UM.DBF LastUpdate = %v, want %v", got, want)
	}
}

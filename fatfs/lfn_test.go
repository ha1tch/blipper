package fatfs

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestLFNChecksumMatchesSpecification checks the checksum against
// a value computed by hand from the algorithm in the FAT
// specification, so a transcription error in the rotate-and-add
// cannot pass unnoticed.
func TestLFNChecksumMatchesSpecification(t *testing.T) {
	var name [11]byte
	copy(name[:], "CUSTOM~1DBF")
	// Reference implementation, written out longhand.
	var want byte
	for _, c := range []byte("CUSTOM~1DBF") {
		want = ((want & 1) << 7) + (want >> 1) + c
	}
	if got := lfnChecksum(name); got != want {
		t.Errorf("lfnChecksum = %d, want %d", got, want)
	}
}

// TestLFNRoundTripThroughEntries builds a long name into entries
// and reassembles it, across lengths that straddle the 13
// characters one entry holds.
func TestLFNRoundTripThroughEntries(t *testing.T) {
	var alias [11]byte
	copy(alias[:], "CUSTOM~1DBF")

	names := []string{
		"a",
		strings.Repeat("x", 12),
		strings.Repeat("x", 13), // exactly one entry
		strings.Repeat("x", 14), // spills into a second
		"CUSTOMERS_ARCHIVE_2024.DBF",
		"Ünïcödé Namé.DBF",
		strings.Repeat("long name ", 20)[:200],
	}
	for _, name := range names {
		entries, err := buildLFNEntries(name, alias)
		if err != nil {
			t.Errorf("buildLFNEntries(%q): %v", name, err)
			continue
		}
		got, ok := assembleLongName(entries, alias)
		if !ok {
			t.Errorf("assembleLongName(%q) rejected its own output", name)
			continue
		}
		if got != name {
			t.Errorf("round trip: got %q, want %q", got, name)
		}
	}
}

// TestLFNChecksumMismatchIsRejected is the desync guard: a run
// whose checksum does not match the short entry describes some
// other file, and reporting its name would be worse than
// reporting the alias.
func TestLFNChecksumMismatchIsRejected(t *testing.T) {
	var alias, other [11]byte
	copy(alias[:], "CUSTOM~1DBF")
	copy(other[:], "DIFFRNT1DBF")

	entries, err := buildLFNEntries("Customers Archive.dbf", alias)
	if err != nil {
		t.Fatalf("buildLFNEntries: %v", err)
	}
	if _, ok := assembleLongName(entries, other); ok {
		t.Error("a run was accepted against a short entry it does not belong to")
	}
}

// TestLFNRejectsMalformedRuns covers the ordering invariants.
func TestLFNRejectsMalformedRuns(t *testing.T) {
	var alias [11]byte
	copy(alias[:], "CUSTOM~1DBF")
	entries, err := buildLFNEntries(strings.Repeat("y", 30), alias)
	if err != nil {
		t.Fatalf("buildLFNEntries: %v", err)
	}
	if len(entries) < 3 {
		t.Fatalf("expected at least 3 entries, got %d", len(entries))
	}

	// Out of order.
	shuffled := [][]byte{entries[1], entries[0], entries[2]}
	if _, ok := assembleLongName(shuffled, alias); ok {
		t.Error("an out-of-order run was accepted")
	}

	// Missing the last-entry marker.
	clipped := make([][]byte, len(entries))
	for i, e := range entries {
		c := make([]byte, len(e))
		copy(c, e)
		clipped[i] = c
	}
	clipped[0][0] &^= lfnLastEntry
	if _, ok := assembleLongName(clipped, alias); ok {
		t.Error("a run with no last-entry marker was accepted")
	}
}

// TestAliasGenerationAvoidsCollisions verifies the ~N numbering.
func TestAliasGenerationAvoidsCollisions(t *testing.T) {
	taken := map[[11]byte]bool{}
	mark := func(s string) {
		var k [11]byte
		copy(k[:], s)
		taken[k] = true
	}
	mark("CUSTOM~1DBF")
	mark("CUSTOM~2DBF")

	got, err := generateAlias("Customers Archive 2024.dbf", func(s [11]byte) bool {
		return taken[s]
	})
	if err != nil {
		t.Fatalf("generateAlias: %v", err)
	}
	if formatName(got) != "CUSTOM~3.DBF" {
		t.Errorf("alias = %q, want CUSTOM~3.DBF", formatName(got))
	}
}

// TestReadsLongNamesFromOracleImage reads an image whose long
// names were written by mcopy.
func TestReadsLongNamesFromOracleImage(t *testing.T) {
	vol, err := OpenImage(loadImage(t, "lfn32.img.gz"), WithLongNames(true))
	if err != nil {
		t.Fatalf("OpenImage: %v", err)
	}
	names := vol.List()

	want := "CUSTOMERS_ARCHIVE_2024.DBF"
	found := false
	for _, n := range names {
		if n == want {
			found = true
		}
	}
	if !found {
		t.Errorf("List = %v, want it to contain %q", names, want)
	}

	// Opening by long name must work.
	f, err := vol.Open(want)
	if err != nil {
		t.Fatalf("Open(%q): %v", want, err)
	}
	got, _ := io.ReadAll(f)
	if string(got) != "long name content" {
		t.Errorf("content = %q, want %q", got, "long name content")
	}

	// And by its 8.3 alias, which every file has regardless.
	if _, err := vol.Open("CUSTOM~1.DBF"); err != nil {
		t.Errorf("Open by alias: %v", err)
	}
}

// TestLongNamesDisabledShowsAliases confirms the default: with
// the option off, enumeration reports 8.3 aliases and nothing
// breaks.
func TestLongNamesDisabledShowsAliases(t *testing.T) {
	vol, err := OpenImage(loadImage(t, "lfn32.img.gz"))
	if err != nil {
		t.Fatalf("OpenImage: %v", err)
	}
	for _, n := range vol.List() {
		if strings.Contains(n, "_ARCHIVE_") {
			t.Errorf("long name %q surfaced with long names disabled", n)
		}
	}
	// The alias still opens.
	if _, err := vol.Open("CUSTOM~1.DBF"); err != nil {
		t.Errorf("Open by alias with long names disabled: %v", err)
	}
}

// TestWriteLongNameRoundTrip writes a long-named file and reads it
// back after a reopen, which proves the LFN run reached the image
// rather than only the cache.
func TestWriteLongNameRoundTrip(t *testing.T) {
	img := loadImage(t, "fat32.img.gz")
	vol, err := OpenImageRW(img, WithLongNames(true))
	if err != nil {
		t.Fatalf("OpenImageRW: %v", err)
	}

	const name = "Quarterly Report 2026.DBF"
	payload := []byte("a long-named file")
	f, err := vol.Create(name)
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	if _, err := f.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := vol.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	reopened, err := OpenImage(&memImage{data: img.data}, WithLongNames(true))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	g, err := reopened.Open(name)
	if err != nil {
		t.Fatalf("reopen Open(%q): %v", name, err)
	}
	got, _ := io.ReadAll(g)
	if !bytes.Equal(got, payload) {
		t.Errorf("content = %q, want %q", got, payload)
	}
}

// TestLongNamedFileIsVisibleToShortNameReader is the
// compatibility check that matters: a reader without long-name
// support must still see a valid volume with a working 8.3 alias.
func TestLongNamedFileIsVisibleToShortNameReader(t *testing.T) {
	img := loadImage(t, "fat32.img.gz")
	vol, err := OpenImageRW(img, WithLongNames(true))
	if err != nil {
		t.Fatalf("OpenImageRW: %v", err)
	}
	f, _ := vol.Create("Quarterly Report 2026.DBF")
	f.Write([]byte("payload"))
	vol.Flush()

	// Reopen with long names OFF, as an older tool would.
	plain, err := OpenImage(&memImage{data: img.data})
	if err != nil {
		t.Fatalf("reopen without long names: %v", err)
	}
	var alias string
	for _, n := range plain.List() {
		if strings.HasPrefix(n, "QUARTE~") {
			alias = n
		}
	}
	if alias == "" {
		t.Fatalf("no 8.3 alias visible to a short-name reader; List = %v", plain.List())
	}
	g, err := plain.Open(alias)
	if err != nil {
		t.Fatalf("Open(%q): %v", alias, err)
	}
	got, _ := io.ReadAll(g)
	if string(got) != "payload" {
		t.Errorf("alias content = %q, want %q", got, "payload")
	}
}

// TestRemoveClearsLongNameRun guards the orphan case: deleting a
// long-named file must clear its LFN entries, or an LFN-aware
// reader would report a stale name.
func TestRemoveClearsLongNameRun(t *testing.T) {
	img := loadImage(t, "fat32.img.gz")
	vol, err := OpenImageRW(img, WithLongNames(true))
	if err != nil {
		t.Fatalf("OpenImageRW: %v", err)
	}
	const name = "Doomed Long Name.DBF"
	f, _ := vol.Create(name)
	f.Write([]byte("x"))
	vol.Flush()

	if err := vol.Remove(name); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	vol.Flush()

	reopened, err := OpenImage(&memImage{data: img.data}, WithLongNames(true))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	for _, n := range reopened.List() {
		if n == name {
			t.Errorf("long name %q survived Remove", name)
		}
	}
}

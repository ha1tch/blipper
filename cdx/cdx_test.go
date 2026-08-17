package cdx

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// openFixture returns a reader for testdata/CDATA.CDX, generated
// by Clipper 5.2e via DBFCDX.LIB (see testdata/README.md).
func openFixture(t *testing.T) *File {
	t.Helper()
	path := filepath.Join("testdata", "CDATA.CDX")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	f, err := Open(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return f
}

func TestOpenAcceptsClipperCDX(t *testing.T) {
	f := openFixture(t)
	if f == nil {
		t.Fatal("Open returned nil")
	}
	if got := f.header.options; got&(optCompact|optCompoundHdr) != (optCompact | optCompoundHdr) {
		t.Errorf("header options = 0x%02X, want compact+compound-header bits", got)
	}
	if f.header.descending {
		t.Errorf("expected ascending index")
	}
}

func TestOpenRejectsNonCDXHeader(t *testing.T) {
	// A 512-byte block with no compound-header flag should be
	// rejected. Zero the block and try.
	blank := make([]byte, BlockSize)
	_, err := Open(bytes.NewReader(blank))
	if !errors.Is(err, ErrNotCDX) {
		t.Errorf("Open on blank block: err=%v, want ErrNotCDX", err)
	}
}

func TestOpenRejectsNonMachineCollation(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "CDATA.CDX"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Signature byte at offset 15 is 0 in DBFCDX output;
	// non-zero is treated as a collation identifier we cannot
	// safely honour.
	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	corrupted[15] = 0x03 // arbitrary non-zero value
	_, err = Open(bytes.NewReader(corrupted))
	if !errors.Is(err, ErrUnsupportedCollation) {
		t.Errorf("Open with non-zero signature: err=%v, want ErrUnsupportedCollation", err)
	}
}

func TestTagsEnumeratesBothClipperTags(t *testing.T) {
	f := openFixture(t)
	got := f.TagNames()
	want := []string{"BYCODE", "BYNVAL"}
	if len(got) != len(want) {
		t.Fatalf("TagNames: got %v, want %v", got, want)
	}
	// Order should be BYCODE then BYNVAL (MACHINE-collated
	// alphabetical, and that's what DBFCDX.LIB writes).
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tag %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTagResolvesKeyExpression(t *testing.T) {
	f := openFixture(t)
	byCode, err := f.Tag("BYCODE")
	if err != nil {
		t.Fatalf("Tag(BYCODE): %v", err)
	}
	// The generator wrote `INDEX ON CODE TAG BYCODE`, so the
	// key expression should be exactly "CODE" (uppercase, as
	// Clipper normalises identifiers).
	if got := byCode.KeyExpr(); got != "CODE" {
		t.Errorf("BYCODE KeyExpr = %q, want %q", got, "CODE")
	}
	if byCode.Descending() {
		t.Errorf("BYCODE should be ascending")
	}

	byNval, err := f.Tag("BYNVAL")
	if err != nil {
		t.Fatalf("Tag(BYNVAL): %v", err)
	}
	if got := byNval.KeyExpr(); got != "NVAL" {
		t.Errorf("BYNVAL KeyExpr = %q, want %q", got, "NVAL")
	}
}

func TestTagLookupUnknownReturnsError(t *testing.T) {
	f := openFixture(t)
	_, err := f.Tag("MISSING")
	if !errors.Is(err, ErrTagNotFound) {
		t.Errorf("Tag(MISSING): err=%v, want ErrTagNotFound", err)
	}
}

func TestTraverseByCodeInAlphabeticalOrder(t *testing.T) {
	f := openFixture(t)
	tag, err := f.Tag("BYCODE")
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}
	var got []string
	var recs []uint32
	err = f.Traverse(tag, func(e Entry) error {
		got = append(got, string(bytes.TrimRight(e.Key, " ")))
		recs = append(recs, e.RecNo)
		return nil
	})
	if err != nil {
		t.Fatalf("Traverse: %v", err)
	}
	// The generator wrote:
	//   rec 1: CODE="ALPHA"
	//   rec 2: CODE="BRAVO"
	//   rec 3: CODE="CHARLIE"
	// so BYCODE should yield them in that order with recNo
	// 1, 2, 3.
	wantKeys := []string{"ALPHA", "BRAVO", "CHARLIE"}
	wantRecs := []uint32{1, 2, 3}
	if len(got) != len(wantKeys) {
		t.Fatalf("Traverse yielded %d entries, want %d: %v", len(got), len(wantKeys), got)
	}
	for i := range wantKeys {
		if got[i] != wantKeys[i] {
			t.Errorf("entry %d key: got %q, want %q", i, got[i], wantKeys[i])
		}
		if recs[i] != wantRecs[i] {
			t.Errorf("entry %d recNo: got %d, want %d", i, recs[i], wantRecs[i])
		}
	}
}

func TestSeekExactMatch(t *testing.T) {
	f := openFixture(t)
	tag, _ := f.Tag("BYCODE")
	e, exact, err := f.Seek(tag, []byte("BRAVO"))
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if !exact {
		t.Errorf("expected exact match for BRAVO")
	}
	if got := string(bytes.TrimRight(e.Key, " ")); got != "BRAVO" {
		t.Errorf("landed on key %q, want BRAVO", got)
	}
	if e.RecNo != 2 {
		t.Errorf("landed on recNo %d, want 2", e.RecNo)
	}
}

func TestSeekBeforeFirstYieldsFirst(t *testing.T) {
	f := openFixture(t)
	tag, _ := f.Tag("BYCODE")
	e, exact, err := f.Seek(tag, []byte("A"))
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if exact {
		t.Errorf("did not expect exact match for A")
	}
	if got := string(bytes.TrimRight(e.Key, " ")); got != "ALPHA" {
		t.Errorf("landed on %q, want ALPHA", got)
	}
}

func TestSeekPastLastYieldsNoEntry(t *testing.T) {
	f := openFixture(t)
	tag, _ := f.Tag("BYCODE")
	_, exact, err := f.Seek(tag, []byte("ZZZZZZZZZZ"))
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if exact {
		t.Errorf("did not expect exact match past end")
	}
	// Returning zero Entry with exact=false is our contract.
}

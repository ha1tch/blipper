package blipperdb

import (
	"bytes"
	"testing"

	"github.com/ha1tch/blipper/cdx"
	"github.com/ha1tch/blipper/dbf"
)

// TestAreaPackRebuildsCDX is the point of the Compactable
// interface: after a pack renumbers records, an attached CDX must
// agree with the table again rather than pointing at the numbers
// records used to have.
func TestAreaPackRebuildsCDX(t *testing.T) {
	db := New()
	area, err := db.Create("DATA", &memFile{}, codeSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	codes := []string{"ALPHA", "BRAVO", "CHARLIE", "DELTA", "ECHO"}
	for _, c := range codes {
		rec := dbf.NewRecord(area.Table().Schema())
		rec.Set(area.Table().Schema(), "CODE", c)
		if _, err := area.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// A CDX over CODE, matching the physical order.
	var buf bytes.Buffer
	entries := make([]cdx.Entry, len(codes))
	for i, c := range codes {
		entries[i] = cdx.Entry{Key: []byte(c), RecNo: uint32(i + 1)}
	}
	if err := cdx.Build(&buf, []cdx.TagSpec{{
		Name: "BYCODE", KeyExpr: "CODE", KeyLen: 10, Entries: entries,
	}}); err != nil {
		t.Fatalf("cdx.Build: %v", err)
	}
	cdxFile := &memFile{data: buf.Bytes()}
	if _, err := area.AttachCDX(cdxFile); err != nil {
		t.Fatalf("AttachCDX: %v", err)
	}
	if err := area.SetOrderCDX("BYCODE"); err != nil {
		t.Fatalf("SetOrderCDX: %v", err)
	}

	// Delete BRAVO (2) and DELTA (4), then pack.
	if err := area.Table().Delete(2); err != nil {
		t.Fatalf("Delete(2): %v", err)
	}
	if err := area.Table().Delete(4); err != nil {
		t.Fatalf("Delete(4): %v", err)
	}

	mapping, err := area.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if mapping.Kept != 3 || mapping.Removed != 2 {
		t.Fatalf("mapping: Kept=%d Removed=%d, want 3 and 2", mapping.Kept, mapping.Removed)
	}

	// The index must now yield the surviving records, in key
	// order, at their new numbers.
	var visited []uint32
	if err := area.TraverseCDX(func(recNo uint32) error {
		visited = append(visited, recNo)
		return nil
	}); err != nil {
		t.Fatalf("TraverseCDX: %v", err)
	}
	// ALPHA(1), CHARLIE(2), ECHO(3) in key order.
	want := []uint32{1, 2, 3}
	if len(visited) != len(want) {
		t.Fatalf("index yielded %v, want %v", visited, want)
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Errorf("index entry %d = %d, want %d", i, visited[i], want[i])
		}
	}

	// And every number the index yields must resolve to the right
	// record in the packed table — the check that would fail if
	// the rebuild were skipped.
	wantCodes := []string{"ALPHA", "CHARLIE", "ECHO"}
	for i, recNo := range visited {
		rec, err := area.Table().Get(recNo)
		if err != nil {
			t.Fatalf("Get(%d): %v", recNo, err)
		}
		got, _ := rec.Get(area.Table().Schema(), "CODE")
		if got.(string) != wantCodes[i] {
			t.Errorf("index entry %d points at %q, want %q", i, got, wantCodes[i])
		}
	}
}

// TestAreaPackIdentitySkipsRebuild verifies the cheap path: a
// pack that removes nothing leaves attachments untouched.
func TestAreaPackIdentitySkipsRebuild(t *testing.T) {
	db := New()
	area, _ := db.Create("DATA", &memFile{}, codeSchema())
	for _, c := range []string{"A", "B"} {
		rec := dbf.NewRecord(area.Table().Schema())
		rec.Set(area.Table().Schema(), "CODE", c)
		area.Append(rec)
	}

	var buf bytes.Buffer
	cdx.Build(&buf, []cdx.TagSpec{{
		Name: "BYCODE", KeyExpr: "CODE", KeyLen: 10,
		Entries: []cdx.Entry{
			{Key: []byte("A"), RecNo: 1},
			{Key: []byte("B"), RecNo: 2},
		},
	}})
	cdxFile := &memFile{data: buf.Bytes()}
	area.AttachCDX(cdxFile)
	before := append([]byte(nil), cdxFile.data...)

	mapping, err := area.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if !mapping.Identity() {
		t.Error("Identity() false for a pack that removed nothing")
	}
	if !bytes.Equal(cdxFile.data, before) {
		t.Error("identity pack rewrote the CDX")
	}
}

// TestAreaPackResetsRecordPointer verifies that the pointer does
// not survive a pack: the record it referred to may be gone.
func TestAreaPackResetsRecordPointer(t *testing.T) {
	db := New()
	area, _ := db.Create("DATA", &memFile{}, codeSchema())
	for _, c := range []string{"A", "B", "C"} {
		rec := dbf.NewRecord(area.Table().Schema())
		rec.Set(area.Table().Schema(), "CODE", c)
		area.Append(rec)
	}
	area.Table().Delete(1)
	if err := area.GoBottom(); err != nil {
		t.Fatalf("GoBottom: %v", err)
	}

	if _, err := area.Pack(); err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if got := area.Recno(); got != 1 {
		t.Errorf("after Pack, RecNo = %d, want 1 (pointer reset to top)", got)
	}
}

// TestCatalogueIsNotCompactable documents the design decision:
// long names do not depend on record numbers, so AttachedCatalogue
// deliberately does not implement Compactable. Forcing a no-op
// Rebuild into existence would leave a reader wondering what it
// was for.
func TestCatalogueIsNotCompactable(t *testing.T) {
	var c interface{} = &AttachedCatalogue{}
	if _, ok := c.(Compactable); ok {
		t.Error("AttachedCatalogue implements Compactable; it holds no record numbers")
	}
	// AttachedCDX does, because it does.
	var x interface{} = &AttachedCDX{}
	if _, ok := x.(Compactable); !ok {
		t.Error("AttachedCDX does not implement Compactable; it holds record numbers")
	}
}

// TestAreaPackAllCompactsMemo exercises the whole coordination:
// pack the table, compact the memo, rewrite pointers, re-attach,
// and confirm every surviving record still reads its own memo.
func TestAreaPackAllCompactsMemo(t *testing.T) {
	db := New()
	dbfFile := &memFile{}
	area, err := db.Create("DATA", dbfFile, dbf.Schema{Fields: []dbf.Field{
		{Name: "CODE", Type: dbf.Character, Length: 10},
		{Name: "NOTES", Type: dbf.Memo, Length: 10},
	}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := area.CreateMemo(&memFile{}, 0); err != nil {
		t.Fatalf("CreateMemo: %v", err)
	}

	codes := []string{"ALPHA", "BRAVO", "CHARLIE", "DELTA"}
	for _, c := range codes {
		rec := dbf.NewRecord(area.Table().Schema())
		rec.Set(area.Table().Schema(), "CODE", c)
		if _, err := area.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	// Give each record a memo, then rewrite them all once so the
	// file carries orphans as well as live entries.
	for pass := 0; pass < 2; pass++ {
		for recno := uint32(1); recno <= area.Table().RecordCount(); recno++ {
			if err := area.GoTo(recno); err != nil {
				t.Fatalf("GoTo(%d): %v", recno, err)
			}
			if err := area.MemoSet("NOTES", []byte("memo for "+codes[recno-1])); err != nil {
				t.Fatalf("MemoSet: %v", err)
			}
		}
	}

	// Remove BRAVO and DELTA.
	if err := area.Table().Delete(2); err != nil {
		t.Fatalf("Delete(2): %v", err)
	}
	if err := area.Table().Delete(4); err != nil {
		t.Fatalf("Delete(4): %v", err)
	}

	dst := &memFile{}
	recMap, blockMap, err := area.PackAll(dst)
	if err != nil {
		t.Fatalf("PackAll: %v", err)
	}
	if recMap.Kept != 2 {
		t.Errorf("records kept = %d, want 2", recMap.Kept)
	}
	if blockMap.Kept != 2 {
		t.Errorf("memo blocks kept = %d, want 2", blockMap.Kept)
	}
	if blockMap.Dropped == 0 {
		t.Error("no memo blocks dropped, but records were removed and memos rewritten")
	}

	// Every survivor must still read its own memo through the
	// compacted file — the check that fails if pointers were not
	// rewritten or the wrong blocks were carried over.
	for recno, want := range map[uint32]string{
		1: "memo for ALPHA",
		2: "memo for CHARLIE",
	} {
		if err := area.GoTo(recno); err != nil {
			t.Fatalf("GoTo(%d): %v", recno, err)
		}
		got, err := area.MemoGet("NOTES")
		if err != nil {
			t.Fatalf("MemoGet(%d): %v", recno, err)
		}
		if string(got) != want {
			t.Errorf("record %d memo = %q, want %q", recno, got, want)
		}
	}
}

// TestAreaPackAllRefusesWithoutMemo keeps the precondition honest.
func TestAreaPackAllRefusesWithoutMemo(t *testing.T) {
	db := New()
	area, _ := db.Create("DATA", &memFile{}, codeSchema())
	if _, _, err := area.PackAll(&memFile{}); err == nil {
		t.Error("PackAll with no memo attached succeeded; want error")
	}
}

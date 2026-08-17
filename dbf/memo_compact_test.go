package dbf

import (
	"bytes"
	"testing"
)

func memoCompactSchema() Schema {
	return Schema{Fields: []Field{
		{Name: "CODE", Type: Character, Length: 10},
		{Name: "NOTES", Type: Memo, Length: 10},
	}}
}

// buildMemoTable creates a table with a memo file and one memo per
// record, optionally rewriting some memos to create orphans.
func buildMemoTable(t *testing.T, fpt bool) (*Table, *memFile, *memFile) {
	t.Helper()
	dbfFile := &memFile{}
	tbl, err := Create(dbfFile, memoCompactSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	memoFile := &memFile{}

	var put func(content []byte) uint32
	if fpt {
		dbfFile.data[0] = 0xF5
		dbfFile.pos = 0
		tbl, err = Open(dbfFile)
		if err != nil {
			t.Fatalf("reopen as FPT: %v", err)
		}
		f, err := CreateFPT(memoFile, 64)
		if err != nil {
			t.Fatalf("CreateFPT: %v", err)
		}
		put = func(c []byte) uint32 {
			b, err := f.Append(c, MemoText)
			if err != nil {
				t.Fatalf("FPT Append: %v", err)
			}
			return b
		}
	} else {
		f, err := CreateMemo(memoFile)
		if err != nil {
			t.Fatalf("CreateMemo: %v", err)
		}
		put = func(c []byte) uint32 {
			b, err := f.Append(c)
			if err != nil {
				t.Fatalf("DBT Append: %v", err)
			}
			return b
		}
	}

	for _, code := range []string{"A", "B", "C", "D"} {
		block := put([]byte("memo for " + code))
		rec := NewRecord(tbl.Schema())
		rec.Set(tbl.Schema(), "CODE", code)
		rec.Set(tbl.Schema(), "NOTES", string(FormatMemoPointer(block)))
		if _, err := tbl.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	return tbl, dbfFile, memoFile
}

func memoOf(t *testing.T, tbl *Table, recno uint32, src *memFile, fpt bool) []byte {
	t.Helper()
	rec, err := tbl.Get(recno)
	if err != nil {
		t.Fatalf("Get(%d): %v", recno, err)
	}
	raw, _ := rec.Get(tbl.Schema(), "NOTES")
	block, present, err := ParseMemoPointer([]byte(raw.(string)))
	if err != nil || !present {
		return nil
	}
	src.pos = 0
	if fpt {
		f, err := OpenFPT(src)
		if err != nil {
			t.Fatalf("OpenFPT: %v", err)
		}
		content, _, err := f.Get(block)
		if err != nil {
			t.Fatalf("FPT Get(%d): %v", block, err)
		}
		return content
	}
	f, err := OpenMemo(src)
	if err != nil {
		t.Fatalf("OpenMemo: %v", err)
	}
	content, err := f.Get(block)
	if err != nil {
		t.Fatalf("DBT Get(%d): %v", block, err)
	}
	return content
}

// TestCompactMemoDropsUnreachableBlocks is the core case: after a
// pack removes records, their memos are unreachable and must not
// be carried into the compacted file.
func TestCompactMemoAfterPack(t *testing.T) {
	for _, fpt := range []bool{false, true} {
		name := "DBT"
		if fpt {
			name = "FPT"
		}
		t.Run(name, func(t *testing.T) {
			tbl, _, memoFile := buildMemoTable(t, fpt)

			// Remove B and D, then pack.
			if err := tbl.Delete(2); err != nil {
				t.Fatalf("Delete(2): %v", err)
			}
			if err := tbl.Delete(4); err != nil {
				t.Fatalf("Delete(4): %v", err)
			}
			if _, err := tbl.Pack(); err != nil {
				t.Fatalf("Pack: %v", err)
			}

			dst := &memFile{}
			memoFile.pos = 0
			m, err := CompactMemo(tbl, memoFile, dst)
			if err != nil {
				t.Fatalf("CompactMemo: %v", err)
			}
			if m.Kept != 2 {
				t.Errorf("Kept = %d, want 2 (A and C survived)", m.Kept)
			}
			if err := RewriteMemoPointers(tbl, m); err != nil {
				t.Fatalf("RewriteMemoPointers: %v", err)
			}

			// The surviving records must still resolve to their
			// own memo content through the compacted file.
			for recno, want := range map[uint32]string{
				1: "memo for A",
				2: "memo for C",
			} {
				got := memoOf(t, tbl, recno, dst, fpt)
				if string(got) != want {
					t.Errorf("record %d memo = %q, want %q", recno, got, want)
				}
			}
		})
	}
}

// TestCompactMemoReclaimsRewriteOrphans covers the larger source
// of waste: every memo rewrite orphans the previous entry, which
// no pack is involved in creating.
func TestCompactMemoReclaimsRewriteOrphans(t *testing.T) {
	tbl, _, memoFile := buildMemoTable(t, false)

	// Rewrite every memo several times, orphaning the old blocks.
	memoFile.pos = 0
	f, err := OpenMemo(memoFile)
	if err != nil {
		t.Fatalf("OpenMemo: %v", err)
	}
	for round := 0; round < 3; round++ {
		for recno := uint32(1); recno <= tbl.RecordCount(); recno++ {
			rec, _ := tbl.Get(recno)
			block, err := f.Append([]byte("rewritten memo round"))
			if err != nil {
				t.Fatalf("Append: %v", err)
			}
			rec.Set(tbl.Schema(), "NOTES", string(FormatMemoPointer(block)))
			if err := tbl.Put(recno, rec); err != nil {
				t.Fatalf("Put: %v", err)
			}
		}
	}
	sizeBefore := len(memoFile.data)

	dst := &memFile{}
	memoFile.pos = 0
	m, err := CompactMemo(tbl, memoFile, dst)
	if err != nil {
		t.Fatalf("CompactMemo: %v", err)
	}
	if m.Kept != 4 {
		t.Errorf("Kept = %d, want 4 (one live memo per record)", m.Kept)
	}
	if m.Dropped == 0 {
		t.Error("Dropped = 0, but three rounds of rewrites orphaned blocks")
	}
	if len(dst.data) >= sizeBefore {
		t.Errorf("compacted file is %d bytes, source was %d; expected smaller",
			len(dst.data), sizeBefore)
	}
}

// TestCompactMemoIdentityWhenNothingOrphaned verifies the cheap
// case: a memo file with no waste maps every block to itself.
func TestCompactMemoIdentityWhenNothingOrphaned(t *testing.T) {
	tbl, _, memoFile := buildMemoTable(t, false)
	dst := &memFile{}
	memoFile.pos = 0
	m, err := CompactMemo(tbl, memoFile, dst)
	if err != nil {
		t.Fatalf("CompactMemo: %v", err)
	}
	if m.Kept != 4 {
		t.Errorf("Kept = %d, want 4", m.Kept)
	}
	if !m.Identity() {
		t.Error("Identity() = false for a memo file with no orphans")
	}
	// An identity mapping must leave pointers alone.
	before, _ := tbl.Get(1)
	if err := RewriteMemoPointers(tbl, m); err != nil {
		t.Fatalf("RewriteMemoPointers: %v", err)
	}
	after, _ := tbl.Get(1)
	b, _ := before.Get(tbl.Schema(), "NOTES")
	a, _ := after.Get(tbl.Schema(), "NOTES")
	if b != a {
		t.Errorf("identity compaction changed a pointer: %q -> %q", b, a)
	}
}

// TestCompactMemoRefusesTableWithoutMemo guards the precondition.
func TestCompactMemoRefusesTableWithoutMemo(t *testing.T) {
	file := &memFile{}
	tbl, err := Create(file, Schema{Fields: []Field{
		{Name: "CODE", Type: Character, Length: 5},
	}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := CompactMemo(tbl, &memFile{}, &memFile{}); err == nil {
		t.Error("CompactMemo on a memoless table succeeded; want error")
	}
}

// TestBlockMappingLookup covers the accessor directly.
func TestBlockMappingLookup(t *testing.T) {
	m := &BlockMapping{oldToNew: map[uint32]uint32{5: 1, 9: 2}}
	if n, ok := m.Lookup(5); !ok || n != 1 {
		t.Errorf("Lookup(5) = (%d, %v), want (1, true)", n, ok)
	}
	if _, ok := m.Lookup(7); ok {
		t.Error("Lookup(7) reported a mapping that does not exist")
	}
	if _, ok := m.Lookup(0); ok {
		t.Error("Lookup(0) should report absence")
	}
	if m.Identity() {
		t.Error("Identity() true for a mapping that moved blocks")
	}
}

var _ = bytes.Equal

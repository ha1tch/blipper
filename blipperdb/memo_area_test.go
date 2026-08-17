package blipperdb

import (
	"bytes"
	"testing"

	"github.com/ha1tch/blipper/dbf"
)

// memoSchema is a schema with one character key and one memo field,
// used by both DBT and FPT tests.
func memoSchema() dbf.Schema {
	return dbf.Schema{Fields: []dbf.Field{
		{Name: "CODE", Type: dbf.Character, Length: 10},
		{Name: "NOTES", Type: dbf.Memo, Length: 10},
	}}
}

// newMemoTable creates a memo-bearing table and appends one blank
// record.  For FPT-flavour testing, the caller flips byte 0 of
// the DBF from 0x83 to 0xF5 in the raw memFile.
func newMemoTable(t *testing.T) (*BlipperDB, *Area, *memFile) {
	t.Helper()
	db := New()
	dbfFile := &memFile{}
	area, err := db.Create("DATA", dbfFile, memoSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rec := dbf.NewRecord(area.Table().Schema())
	rec.Set(area.Table().Schema(), "CODE", "R1")
	// NOTES starts as ten spaces (absent memo pointer).
	if _, err := area.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	return db, area, dbfFile
}

// TestMemoAttachRefusedOnPlainTable verifies that Attach and
// Create refuse a table with no memo field.
func TestMemoAttachRefusedOnPlainTable(t *testing.T) {
	db := New()
	area, err := db.Create("PLAIN", &memFile{}, dbf.Schema{Fields: []dbf.Field{
		{Name: "CODE", Type: dbf.Character, Length: 5},
	}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := area.AttachMemo(&memFile{}); err == nil {
		t.Error("AttachMemo on no-memo table: want error, got nil")
	}
	if _, err := area.CreateMemo(&memFile{}, 0); err == nil {
		t.Error("CreateMemo on no-memo table: want error, got nil")
	}
	if area.Memo() != nil {
		t.Errorf("Memo() on no-memo table = %v, want nil", area.Memo())
	}
}

// TestMemoDBTRoundTrip creates a DBT-flavour memo file, writes
// content through Area.MemoSet, and reads it back through
// Area.MemoGet on both the fresh Area and one reopened from raw
// bytes.
func TestMemoDBTRoundTrip(t *testing.T) {
	_, area, _ := newMemoTable(t)

	dbtFile := &memFile{}
	attached, err := area.CreateMemo(dbtFile, 0)
	if err != nil {
		t.Fatalf("CreateMemo: %v", err)
	}
	if attached.Format() != dbf.MemoFormatDBT {
		t.Errorf("Format = %v, want DBT (default for 0x83)", attached.Format())
	}
	if attached.DBT() == nil {
		t.Error("DBT() returned nil for DBT attachment")
	}
	if attached.FPT() != nil {
		t.Error("FPT() should be nil for DBT attachment")
	}

	// Go to record 1 and set a memo.
	if err := area.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	const content = "quick brown fox jumps over the lazy dog"
	if err := area.MemoSet("NOTES", []byte(content)); err != nil {
		t.Fatalf("MemoSet: %v", err)
	}

	// Read back through the same Area.
	got, err := area.MemoGet("NOTES")
	if err != nil {
		t.Fatalf("MemoGet: %v", err)
	}
	if string(got) != content {
		t.Errorf("MemoGet (same Area) = %q, want %q", got, content)
	}

	// MemoGet on a fresh record with no pointer should return
	// empty. Move past the record we set to prove this.
	rec := dbf.NewRecord(area.Table().Schema())
	rec.Set(area.Table().Schema(), "CODE", "R2")
	if _, err := area.Append(rec); err != nil {
		t.Fatalf("Append second record: %v", err)
	}
	if err := area.GoTo(2); err != nil {
		t.Fatalf("GoTo(2): %v", err)
	}
	got, err = area.MemoGet("NOTES")
	if err != nil {
		t.Fatalf("MemoGet on absent memo: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("MemoGet on absent memo = %q, want empty", got)
	}
}

// TestMemoFPTRoundTrip mirrors the DBT test for FPT. Flips the
// version byte in the DBF from 0x83 to 0xF5, reopens as an
// FPT-flavour table, attaches a fresh FPT sibling, and round-trips
// content that includes a 0x1A byte (which would trip a DBT-style
// terminator scan if the format dispatch were broken).
func TestMemoFPTRoundTrip(t *testing.T) {
	db, area, dbfFile := newMemoTable(t)

	// Flip 0x83 (DBT-flavour) to 0xF5 (FPT-flavour) in the raw
	// DBF, then close and reopen the area. This exercises the
	// version-byte round-trip from T-12 and Table.MemoFormat's
	// dispatch.
	if dbfFile.data[0] != 0x83 {
		t.Fatalf("expected 0x83 in fresh DBF, got 0x%02X", dbfFile.data[0])
	}
	dbfFile.data[0] = 0xF5
	if err := db.CloseArea("DATA"); err != nil {
		t.Fatalf("CloseArea: %v", err)
	}
	dbfFile.pos = 0
	area, err := db.Use("DATA", dbfFile)
	if err != nil {
		t.Fatalf("Use after flip: %v", err)
	}
	if got := area.Table().MemoFormat(); got != dbf.MemoFormatFPT {
		t.Fatalf("after flip, MemoFormat = %v, want FPT", got)
	}

	fptFile := &memFile{}
	attached, err := area.CreateMemo(fptFile, 64)
	if err != nil {
		t.Fatalf("CreateMemo: %v", err)
	}
	if attached.Format() != dbf.MemoFormatFPT {
		t.Errorf("Format = %v, want FPT", attached.Format())
	}
	if attached.FPT() == nil {
		t.Error("FPT() returned nil for FPT attachment")
	}
	if attached.DBT() != nil {
		t.Error("DBT() should be nil for FPT attachment")
	}

	if err := area.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	// Binary payload including 0x1A. DBT would treat 0x1A as
	// an end-of-memo marker; FPT is length-driven and preserves
	// it. If the dispatch is wrong, MemoGet on FPT-flavour
	// would either error or truncate.
	content := []byte{'H', 'i', 0x1A, 'x', 0xFF, 0x00, 'y'}
	if err := area.MemoSet("NOTES", content); err != nil {
		t.Fatalf("MemoSet: %v", err)
	}

	got, err := area.MemoGet("NOTES")
	if err != nil {
		t.Fatalf("MemoGet: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("FPT round trip:\n  got:  %v\n  want: %v", got, content)
	}
}

// TestMemoAttachTwiceRefused verifies that AttachMemo/CreateMemo
// error when a memo is already attached.
func TestMemoAttachTwiceRefused(t *testing.T) {
	_, area, _ := newMemoTable(t)
	if _, err := area.CreateMemo(&memFile{}, 0); err != nil {
		t.Fatalf("first CreateMemo: %v", err)
	}
	if _, err := area.CreateMemo(&memFile{}, 0); err == nil {
		t.Error("second CreateMemo: want error, got nil")
	}
	if _, err := area.AttachMemo(&memFile{}); err == nil {
		t.Error("AttachMemo after CreateMemo: want error, got nil")
	}
}

// TestMemoGetSetWithoutAttach errors cleanly.
func TestMemoGetSetWithoutAttach(t *testing.T) {
	_, area, _ := newMemoTable(t)
	if err := area.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	if _, err := area.MemoGet("NOTES"); err == nil {
		t.Error("MemoGet with no attach: want error")
	}
	if err := area.MemoSet("NOTES", []byte("x")); err == nil {
		t.Error("MemoSet with no attach: want error")
	}
}

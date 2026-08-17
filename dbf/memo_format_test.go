package dbf

import (
	"strings"
	"testing"
)

// TestMemoFormatOnPlainTable checks that a table with no memo
// field reports MemoFormatNone.
func TestMemoFormatOnPlainTable(t *testing.T) {
	file := &memFile{}
	schema := Schema{Fields: []Field{
		{Name: "CODE", Type: Character, Length: 10},
	}}
	tbl, err := Create(file, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := tbl.MemoFormat(); got != MemoFormatNone {
		t.Errorf("MemoFormat = %v, want MemoFormatNone", got)
	}
}

// TestMemoFormatOnDBTTable checks that a table with a Memo field
// reports MemoFormatDBT (the DBT-flavour default from Create).
func TestMemoFormatOnDBTTable(t *testing.T) {
	file := &memFile{}
	schema := Schema{Fields: []Field{
		{Name: "CODE", Type: Character, Length: 10},
		{Name: "NOTES", Type: Memo, Length: 10},
	}}
	tbl, err := Create(file, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := tbl.MemoFormat(); got != MemoFormatDBT {
		t.Errorf("MemoFormat = %v, want MemoFormatDBT (Create defaults to DBT-flavour for memo schemas)", got)
	}
}

// TestMemoFormatRoundTripsThroughOpen verifies that opening a
// hand-crafted FPT-flavour table (version byte 0xF5) reports
// MemoFormatFPT rather than DBT — that is, the version byte
// survived the header read.
func TestMemoFormatRoundTripsThroughOpen(t *testing.T) {
	file := &memFile{}
	schema := Schema{Fields: []Field{
		{Name: "CODE", Type: Character, Length: 10},
		{Name: "NOTES", Type: Memo, Length: 10},
	}}
	if _, err := Create(file, schema); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if file.data[0] != 0x83 {
		t.Fatalf("Create wrote version byte 0x%02X, want 0x83", file.data[0])
	}
	// Flip byte 0 in the buffer from 0x83 (DBT) to 0xF5 (FPT).
	file.data[0] = 0xF5

	file.pos = 0
	reopened, err := Open(file)
	if err != nil {
		t.Fatalf("Open FPT-flavour table: %v", err)
	}
	if got := reopened.MemoFormat(); got != MemoFormatFPT {
		t.Errorf("MemoFormat after reopen = %v, want MemoFormatFPT", got)
	}
}

// TestMemoFormatPreservedThroughRewrite proves that appending a
// record to an FPT-flavour table (which triggers a header rewrite)
// does not silently demote it to DBT-flavour.
func TestMemoFormatPreservedThroughRewrite(t *testing.T) {
	file := &memFile{}
	schema := Schema{Fields: []Field{
		{Name: "CODE", Type: Character, Length: 10},
		{Name: "NOTES", Type: Memo, Length: 10},
	}}
	if _, err := Create(file, schema); err != nil {
		t.Fatalf("Create: %v", err)
	}
	file.data[0] = 0xF5 // flip to FPT-flavour

	file.pos = 0
	tbl, err := Open(file)
	if err != nil {
		t.Fatalf("Open FPT-flavour: %v", err)
	}
	// Append forces flushHeader → writeHeader, the historical
	// path that would have demoted the version byte.
	rec := NewRecord(tbl.Schema())
	rec.Set(tbl.Schema(), "CODE", "TEST")
	rec.Set(tbl.Schema(), "NOTES", "         ") // absent memo pointer
	if _, err := tbl.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := tbl.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if file.data[0] != 0xF5 {
		t.Errorf("after rewrite, version byte = 0x%02X, want 0xF5 (no silent demotion)", file.data[0])
	}
}

// TestMemoFormatString provides a small diagnostic touchpoint.
func TestMemoFormatString(t *testing.T) {
	for f, want := range map[MemoFormat]string{
		MemoFormatNone: "none",
		MemoFormatDBT:  "DBT",
		MemoFormatFPT:  "FPT",
	} {
		if got := f.String(); got != want {
			t.Errorf("MemoFormat(%d).String() = %q, want %q", f, got, want)
		}
	}
	if got := MemoFormat(42).String(); !strings.Contains(got, "none") {
		t.Errorf("unknown MemoFormat string = %q, want 'none'", got)
	}
}

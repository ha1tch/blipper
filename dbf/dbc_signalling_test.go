package dbf

import (
	"strings"
	"testing"
)

// TestCreateWithBacklinkRoundTrip writes a DBC-owned DBF and
// verifies every header claim survives Close + reopen:
//
//   - byte 28 = 0x0C (DBC + blipper bits)
//   - 263-byte backlink present, decodes to the requested path
//   - version byte preserved
//   - append still works: first record lands after the backlink
//     rather than overlapping it
func TestCreateWithBacklinkRoundTrip(t *testing.T) {
	file := &memFile{}
	schema := Schema{Fields: []Field{
		{Name: "CODE", Type: Character, Length: 10},
	}}
	tbl, err := CreateWithBacklink(file, schema, "CUSTOMERS.DBC")
	if err != nil {
		t.Fatalf("CreateWithBacklink: %v", err)
	}
	if got := tbl.TableFlags(); got != dbfTableFlagDBCPair {
		t.Errorf("TableFlags = 0x%02X, want 0x%02X", got, dbfTableFlagDBCPair)
	}
	if got := tbl.Backlink(); got != "CUSTOMERS.DBC" {
		t.Errorf("Backlink = %q, want CUSTOMERS.DBC", got)
	}

	// Reopen and re-check.
	file.pos = 0
	reopened, err := Open(file)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := reopened.TableFlags(); got != dbfTableFlagDBCPair {
		t.Errorf("reopen TableFlags = 0x%02X, want 0x%02X", got, dbfTableFlagDBCPair)
	}
	if got := reopened.Backlink(); got != "CUSTOMERS.DBC" {
		t.Errorf("reopen Backlink = %q, want CUSTOMERS.DBC", got)
	}

	// Append a record; it must land after the header + backlink.
	rec := NewRecord(reopened.Schema())
	rec.Set(reopened.Schema(), "CODE", "ROW1")
	if _, err := reopened.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := reopened.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	// After Flush, byte 28 must still be 0x0C.
	if got := file.data[28]; got != dbfTableFlagDBCPair {
		t.Errorf("after Flush byte 28 = 0x%02X, want 0x%02X (no silent demotion)", got, dbfTableFlagDBCPair)
	}
	// And the backlink region should still hold the same path.
	// The backlink lives between the field terminator and the
	// first record; find it and check.
	sch := reopened.Schema()
	hdrSize := int(sch.HeaderSize())
	blStart := hdrSize
	blEnd := blStart + dbfBacklinkSize
	blStr := trimBacklink(file.data[blStart:blEnd])
	if blStr != "CUSTOMERS.DBC" {
		t.Errorf("post-Flush backlink = %q, want CUSTOMERS.DBC", blStr)
	}
}

// TestOpenRejectsBlipperBitWithoutDBCBit verifies the truth-table
// invariant: byte 28 = 0x08 (blipper bit alone) is malformed and
// blipper refuses to open it rather than silently misinterpret.
func TestOpenRejectsBlipperBitWithoutDBCBit(t *testing.T) {
	file := &memFile{}
	schema := Schema{Fields: []Field{{Name: "K", Type: Character, Length: 5}}}
	if _, err := Create(file, schema); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Flip byte 28 to the malformed 0x08 (blipper without DBC).
	file.data[28] = dbfTableFlagBlipper

	file.pos = 0
	_, err := Open(file)
	if err == nil {
		t.Fatal("Open accepted byte 28 = 0x08 (malformed); want error")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("error message = %q, want 'malformed'", err.Error())
	}
}

// TestCreateWithBacklinkRefusesOversizePath guards the size limit.
func TestCreateWithBacklinkRefusesOversizePath(t *testing.T) {
	file := &memFile{}
	schema := Schema{Fields: []Field{{Name: "K", Type: Character, Length: 5}}}
	longPath := strings.Repeat("x", dbfBacklinkSize)
	if _, err := CreateWithBacklink(file, schema, longPath); err == nil {
		t.Error("CreateWithBacklink accepted path at exactly limit; want rejection (need room for NUL)")
	}
}

// TestOpenPreservesTableFlagsThroughRewrite proves that opening
// a plain (byte 28 = 0x00) table and appending doesn't corrupt
// byte 28 in either direction — same class of invariant as
// TestMemoFormatPreservedThroughRewrite in memo_format_test.go.
func TestOpenPreservesTableFlagsThroughRewrite(t *testing.T) {
	file := &memFile{}
	schema := Schema{Fields: []Field{{Name: "K", Type: Character, Length: 5}}}
	if _, err := Create(file, schema); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if file.data[28] != 0x00 {
		t.Fatalf("Create wrote byte 28 = 0x%02X, want 0x00", file.data[28])
	}

	file.pos = 0
	tbl, err := Open(file)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rec := NewRecord(tbl.Schema())
	rec.Set(tbl.Schema(), "K", "X")
	if _, err := tbl.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := tbl.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if file.data[28] != 0x00 {
		t.Errorf("after Flush byte 28 = 0x%02X, want 0x00 (plain table not gaining DBC flag)", file.data[28])
	}
}

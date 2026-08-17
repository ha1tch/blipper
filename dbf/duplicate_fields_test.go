package dbf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenToleratesDuplicateFieldNames verifies that a real
// Clipper table carrying duplicate field names (UM.DBF from
// the ha1tch/clipper corpus, three ACCUMDSUM fields at
// positions 12, 13, 14) opens successfully.
//
// The specimen was analysed under T-07 and confirmed to be a
// plain Clipper table, not a VFP DBC-owned one:
//
//   - version byte 0x03
//   - byte 28 = 0x00 (no DBC flag)
//   - no sibling .DBC in the directory
//   - header size 578 = 32 + 17*32 + 1 + 1 (no 263-byte backlink)
//   - field descriptor bytes 12-15 = 99190000 (Clipper stale
//     memory, per oracle §9.2)
//   - field descriptor byte 18 = 0x03 (Clipper stale bytes)
//
// The duplicates are genuine and the application evidently used
// positional access. blipper's Open must accept this to be
// useful against real corpora.
func TestOpenToleratesDuplicateFieldNames(t *testing.T) {
	path := filepath.Join("testdata", "UM.DBF")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	buf := &memFile{data: data}

	tbl, err := Open(buf)
	if err != nil {
		t.Fatalf("Open UM.DBF: %v", err)
	}
	sch := tbl.Schema()
	if len(sch.Fields) != 17 {
		t.Errorf("field count = %d, want 17", len(sch.Fields))
	}

	// Confirm ACCUMDSUM appears exactly three times at the
	// expected positions.
	var positions []int
	for i, f := range sch.Fields {
		if strings.ToUpper(f.Name) == "ACCUMDSUM" {
			positions = append(positions, i)
		}
	}
	wantPositions := []int{12, 13, 14}
	if len(positions) != len(wantPositions) {
		t.Fatalf("ACCUMDSUM found at %v, want %v", positions, wantPositions)
	}
	for i := range wantPositions {
		if positions[i] != wantPositions[i] {
			t.Errorf("ACCUMDSUM occurrence %d at position %d, want %d",
				i, positions[i], wantPositions[i])
		}
	}
}

// TestCreateStillRejectsDuplicateFieldNames verifies the strict
// side of the split: Create refuses schemas with duplicate names,
// because we should not be writing new tables that would be
// ambiguous for named access.
func TestCreateStillRejectsDuplicateFieldNames(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "A", Type: Character, Length: 5},
		{Name: "A", Type: Character, Length: 5}, // deliberate duplicate
	}}
	_, err := Create(&memFile{}, schema)
	if err == nil {
		t.Fatal("Create accepted a schema with duplicate field names; want rejection")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("Create error = %v, want message mentioning duplicate", err)
	}
}

// TestSchemaValidateStillRejectsDuplicates guards the public
// Validate contract independently: callers who Validate schemas
// they'd previously trust to compile fine should still see the
// strict rejection.
func TestSchemaValidateStillRejectsDuplicates(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "X", Type: Character, Length: 5},
		{Name: "X", Type: Character, Length: 5},
	}}
	if err := schema.Validate(); err == nil {
		t.Fatal("Validate accepted duplicates; want rejection (strict)")
	}
}

// TestOpenUMDuplicateResolvesToFirstMatch documents that
// Record.Get(schema, "ACCUMDSUM") on a UM.DBF record returns the
// value of field 12 — Clipper's own behavior via linear scan.
// Callers who need field 13 or 14 must use GetIndex.
func TestOpenUMDuplicateResolvesToFirstMatch(t *testing.T) {
	path := filepath.Join("testdata", "UM.DBF")
	data, _ := os.ReadFile(path)
	tbl, err := Open(&memFile{data: data})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rec, err := tbl.Get(1)
	if err != nil {
		t.Fatalf("Get(1): %v", err)
	}
	// Just prove that named access returns the first-match value:
	// we compare against GetIndex(12) which is what the linear
	// scan should resolve to.
	viaName, err := rec.Get(tbl.Schema(), "ACCUMDSUM")
	if err != nil {
		t.Fatalf("Get by name: %v", err)
	}
	viaIndex, err := rec.GetIndex(12)
	if err != nil {
		t.Fatalf("GetIndex(12): %v", err)
	}
	if viaName != viaIndex {
		t.Errorf("named access returned %v; positional[12] returned %v (want equality)",
			viaName, viaIndex)
	}
}

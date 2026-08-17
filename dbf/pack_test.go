package dbf

import (
	"fmt"
	"testing"
)

func packSchema() Schema {
	return Schema{Fields: []Field{
		{Name: "CODE", Type: Character, Length: 10},
	}}
}

// buildPackTable creates a table with the given codes and marks
// the listed one-based record numbers deleted.
func buildPackTable(t *testing.T, codes []string, deleted ...uint32) (*Table, *memFile) {
	t.Helper()
	file := &memFile{}
	tbl, err := Create(file, packSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, c := range codes {
		rec := NewRecord(tbl.Schema())
		rec.Set(tbl.Schema(), "CODE", c)
		if _, err := tbl.Append(rec); err != nil {
			t.Fatalf("Append %q: %v", c, err)
		}
	}
	for _, d := range deleted {
		if err := tbl.Delete(d); err != nil {
			t.Fatalf("Delete(%d): %v", d, err)
		}
	}
	return tbl, file
}

func codesOf(t *testing.T, tbl *Table) []string {
	t.Helper()
	var out []string
	for i := uint32(1); i <= tbl.RecordCount(); i++ {
		rec, err := tbl.Get(i)
		if err != nil {
			t.Fatalf("Get(%d): %v", i, err)
		}
		v, err := rec.Get(tbl.Schema(), "CODE")
		if err != nil {
			t.Fatalf("Get CODE: %v", err)
		}
		out = append(out, v.(string))
	}
	return out
}

func TestPackRemovesDeletedRecords(t *testing.T) {
	tbl, _ := buildPackTable(t, []string{"A", "B", "C", "D", "E"}, 2, 4)

	m, err := tbl.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if m.Removed != 2 || m.Kept != 3 {
		t.Errorf("mapping: Removed=%d Kept=%d, want 2 and 3", m.Removed, m.Kept)
	}
	if got := tbl.RecordCount(); got != 3 {
		t.Errorf("RecordCount = %d, want 3", got)
	}
	want := []string{"A", "C", "E"}
	got := codesOf(t, tbl)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestPackMappingDescribesTheMove(t *testing.T) {
	tbl, _ := buildPackTable(t, []string{"A", "B", "C", "D", "E"}, 2, 4)
	m, err := tbl.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}

	cases := []struct {
		old      uint32
		wantNew  uint32
		survives bool
	}{
		{1, 1, true},  // A stays first
		{2, 0, false}, // B removed
		{3, 2, true},  // C moves up
		{4, 0, false}, // D removed
		{5, 3, true},  // E moves up
	}
	for _, c := range cases {
		gotNew, gotSurvives := m.Lookup(c.old)
		if gotSurvives != c.survives || gotNew != c.wantNew {
			t.Errorf("Lookup(%d) = (%d, %v), want (%d, %v)",
				c.old, gotNew, gotSurvives, c.wantNew, c.survives)
		}
	}
	if got := m.OldCount(); got != 5 {
		t.Errorf("OldCount = %d, want 5", got)
	}
	// Out-of-range inputs report absence rather than erroring:
	// an index built before the pack may hold stale numbers.
	if _, ok := m.Lookup(0); ok {
		t.Error("Lookup(0) reported a survivor")
	}
	if _, ok := m.Lookup(99); ok {
		t.Error("Lookup(99) reported a survivor")
	}
}

func TestPackWithNothingDeletedIsIdentity(t *testing.T) {
	tbl, file := buildPackTable(t, []string{"A", "B", "C"})
	before := append([]byte(nil), file.data...)

	m, err := tbl.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if !m.Identity() {
		t.Error("Identity() = false for a table with no deleted records")
	}
	if m.Removed != 0 || m.Kept != 3 {
		t.Errorf("mapping: Removed=%d Kept=%d, want 0 and 3", m.Removed, m.Kept)
	}
	// An identity pack must not rewrite the file: callers use
	// Identity() to skip index rebuilds, and that is only sound
	// if nothing moved.
	if string(file.data) != string(before) {
		t.Error("identity pack rewrote the file")
	}
}

func TestPackAllDeletedLeavesEmptyTable(t *testing.T) {
	tbl, _ := buildPackTable(t, []string{"A", "B"}, 1, 2)
	m, err := tbl.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if m.Kept != 0 || m.Removed != 2 {
		t.Errorf("mapping: Kept=%d Removed=%d, want 0 and 2", m.Kept, m.Removed)
	}
	if got := tbl.RecordCount(); got != 0 {
		t.Errorf("RecordCount = %d, want 0", got)
	}
}

func TestPackSurvivesReopen(t *testing.T) {
	tbl, file := buildPackTable(t, []string{"A", "B", "C", "D"}, 1, 3)
	if _, err := tbl.Pack(); err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if err := tbl.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	file.pos = 0
	reopened, err := Open(file)
	if err != nil {
		t.Fatalf("Open after pack: %v", err)
	}
	if got := reopened.RecordCount(); got != 2 {
		t.Fatalf("reopened RecordCount = %d, want 2", got)
	}
	want := []string{"B", "D"}
	got := codesOf(t, reopened)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reopened record %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

// TestPackConsecutiveDeletions covers the case where survivors
// move by varying distances, which a naive fixed-offset shift
// would get wrong.
func TestPackConsecutiveDeletions(t *testing.T) {
	codes := make([]string, 10)
	for i := range codes {
		codes[i] = fmt.Sprintf("R%02d", i+1)
	}
	// Delete 2,3,4 and 7,8: survivors are 1,5,6,9,10 moving to
	// 1,2,3,4,5 — shifts of 0,3,3,5,5.
	tbl, _ := buildPackTable(t, codes, 2, 3, 4, 7, 8)

	m, err := tbl.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if m.Kept != 5 || m.Removed != 5 {
		t.Fatalf("mapping: Kept=%d Removed=%d, want 5 and 5", m.Kept, m.Removed)
	}
	want := []string{"R01", "R05", "R06", "R09", "R10"}
	got := codesOf(t, tbl)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %q, want %q", i+1, got[i], want[i])
		}
	}
	for _, c := range []struct{ old, new uint32 }{
		{1, 1}, {5, 2}, {6, 3}, {9, 4}, {10, 5},
	} {
		if n, ok := m.Lookup(c.old); !ok || n != c.new {
			t.Errorf("Lookup(%d) = (%d, %v), want (%d, true)", c.old, n, ok, c.new)
		}
	}
}

package dbf

import "testing"

// TestReloadObservesAnotherHandlesAppend is T-20's decisive test:
// two independent *Table instances opened over the same
// underlying stream — standing in for two processes sharing a
// file — where a write through one is invisible to the other
// until Reload is called.
//
// Before T-20, no reload path existed at all: Table.RecordCount()
// simply never changed once Open returned, for the life of the
// Table, regardless of what anyone else wrote to the same file.
func TestReloadObservesAnotherHandlesAppend(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "N", Type: Numeric, Length: 4}}}
	buf := &memBuffer{}

	writer, err := Create(buf, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	reader, err := Open(buf)
	if err != nil {
		t.Fatalf("Open (second handle): %v", err)
	}
	if reader.RecordCount() != 0 {
		t.Fatalf("reader.RecordCount() = %d before any writes, want 0", reader.RecordCount())
	}

	rec := NewRecord(schema)
	if err := rec.Set(schema, "N", int64(42)); err != nil {
		t.Fatalf("rec.Set: %v", err)
	}
	if _, err := writer.Append(rec); err != nil {
		t.Fatalf("writer.Append: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("writer.Flush: %v", err)
	}

	// Without Reload, the second handle still sees the stale count.
	if got := reader.RecordCount(); got != 0 {
		t.Errorf("reader.RecordCount() before Reload = %d, want 0 (stale, as documented)", got)
	}

	if err := reader.Reload(); err != nil {
		t.Fatalf("reader.Reload: %v", err)
	}
	if got := reader.RecordCount(); got != 1 {
		t.Errorf("reader.RecordCount() after Reload = %d, want 1", got)
	}

	got, err := reader.Get(1)
	if err != nil {
		t.Fatalf("reader.Get(1) after Reload: %v", err)
	}
	n, _ := got.Get(schema, "N")
	if n != int64(42) {
		t.Errorf("reader.Get(1) after Reload: N = %v, want 42", n)
	}
}

// TestReloadRejectsRecordSizeMismatch confirms Reload treats a
// changed record size as an error rather than a normal update —
// the safety check distinguishing "someone else wrote a record"
// from "this isn't the same table anymore".
func TestReloadRejectsRecordSizeMismatch(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "N", Type: Numeric, Length: 4}}}
	buf := &memBuffer{}

	tbl, err := Create(buf, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Corrupt the on-disk record-size field directly (bytes
	// 10-11 of the header) to simulate a genuinely different file
	// now occupying the same stream.
	buf.data[10] = 99
	buf.data[11] = 0

	if err := tbl.Reload(); err == nil {
		t.Error("Reload succeeded despite a record-size mismatch, want an error")
	}
	// The stale RecordCount must not have been silently replaced
	// by anything from the mismatched header.
	if got := tbl.RecordCount(); got != 0 {
		t.Errorf("RecordCount() after a rejected Reload = %d, want unchanged (0)", got)
	}
}

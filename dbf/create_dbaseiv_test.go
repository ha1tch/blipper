package dbf

import "testing"

// TestCreateDBaseIVTableRoundTrip writes a fresh 0x8B table with a
// Memo field and a B (DBaseBinary) field, reopens it, and confirms
// everything T-31 established about reading this lineage also
// holds for what CreateDBaseIV writes: version byte, the B/G
// lineage dispatch, and memo pointer round-tripping.
func TestCreateDBaseIVTableRoundTrip(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "NAME", Type: Character, Length: 10},
		{Name: "PHOTO", Type: DBaseBinary, Length: 10},
		{Name: "NOTES", Type: Memo, Length: 10},
	}}

	buf := &memBuffer{}
	tbl, err := CreateDBaseIV(buf, schema, DBaseIVTable)
	if err != nil {
		t.Fatalf("CreateDBaseIV: %v", err)
	}
	if got := tbl.versionByte; got != dbfVersionDBaseIV {
		t.Errorf("versionByte = 0x%02X, want 0x%02X", got, dbfVersionDBaseIV)
	}

	rec := NewRecord(schema)
	if err := rec.Set(schema, "NAME", "ALPHA"); err != nil {
		t.Fatalf("rec.Set NAME: %v", err)
	}
	if err := rec.Set(schema, "PHOTO", "0000000042"); err != nil {
		t.Fatalf("rec.Set PHOTO: %v", err)
	}
	if err := rec.Set(schema, "NOTES", "0000000007"); err != nil {
		t.Fatalf("rec.Set NOTES: %v", err)
	}
	if _, err := tbl.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := tbl.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Reopen as a fresh Open would — this is the real test: does
	// the byte-level output this function produces actually
	// parse back through the same isDBaseLineage/remap path real
	// dBASE 5.0 output does?
	reopened, err := Open(buf)
	if err != nil {
		t.Fatalf("Open (reopen): %v", err)
	}
	if got := reopened.versionByte; got != dbfVersionDBaseIV {
		t.Errorf("reopened versionByte = 0x%02X, want 0x%02X", got, dbfVersionDBaseIV)
	}

	reopenedSchema := reopened.Schema()
	photoField, err := schemaFieldIndex(reopenedSchema, "PHOTO")
	if err != nil {
		t.Fatalf("schemaFieldIndex(PHOTO): %v", err)
	}
	if got := reopenedSchema.Fields[photoField].Type; got != DBaseBinary {
		t.Errorf("reopened PHOTO field type = %c, want DBaseBinary — the remap on read did not recover what this function wrote", rune(got))
	}

	got, err := reopened.Get(1)
	if err != nil {
		t.Fatalf("Get(1): %v", err)
	}
	name, _ := got.Get(reopenedSchema, "NAME")
	if name != "ALPHA" {
		t.Errorf("NAME = %v, want ALPHA", name)
	}
	photo, _ := got.Get(reopenedSchema, "PHOTO")
	if photo != "0000000042" {
		t.Errorf("PHOTO = %v, want \"0000000042\"", photo)
	}
	notes, _ := got.Get(reopenedSchema, "NOTES")
	if notes != "0000000007" {
		t.Errorf("NOTES = %v, want \"0000000007\"", notes)
	}
}

// TestCreateDBaseIVSQLVariants confirms both SQL-variant version
// bytes write and reopen correctly for a memo-free schema —
// covering the two bytes T-31's own read support added that this
// package had never had a write path for at all.
func TestCreateDBaseIVSQLVariants(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "ID", Type: Numeric, Length: 4}}}

	for _, kind := range []DBaseIVTableKind{DBaseIVSQLTable, DBaseIVSQLSystem} {
		buf := &memBuffer{}
		tbl, err := CreateDBaseIV(buf, schema, kind)
		if err != nil {
			t.Fatalf("CreateDBaseIV(%s): %v", kind, err)
		}
		if got := tbl.versionByte; got != byte(kind) {
			t.Errorf("%s: versionByte = 0x%02X, want 0x%02X", kind, got, byte(kind))
		}

		reopened, err := Open(buf)
		if err != nil {
			t.Fatalf("%s: reopen: %v", kind, err)
		}
		if got := reopened.versionByte; got != byte(kind) {
			t.Errorf("%s: reopened versionByte = 0x%02X, want 0x%02X", kind, got, byte(kind))
		}
	}
}

// TestCreateDBaseIVRejectsMismatchedMemo guards the API surface:
// DBaseIVTable without a memo field, and the SQL variants with
// one, are both refused rather than silently producing an
// unverified shape.
func TestCreateDBaseIVRejectsMismatchedMemo(t *testing.T) {
	noMemo := Schema{Fields: []Field{{Name: "ID", Type: Numeric, Length: 4}}}
	withMemo := Schema{Fields: []Field{{Name: "NOTES", Type: Memo, Length: 10}}}

	if _, err := CreateDBaseIV(&memBuffer{}, noMemo, DBaseIVTable); err == nil {
		t.Error("CreateDBaseIV(DBaseIVTable) with no Memo field succeeded, want an error")
	}
	if _, err := CreateDBaseIV(&memBuffer{}, withMemo, DBaseIVSQLTable); err == nil {
		t.Error("CreateDBaseIV(DBaseIVSQLTable) with a Memo field succeeded, want an error")
	}
	if _, err := CreateDBaseIV(&memBuffer{}, withMemo, DBaseIVSQLSystem); err == nil {
		t.Error("CreateDBaseIV(DBaseIVSQLSystem) with a Memo field succeeded, want an error")
	}
}

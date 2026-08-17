package dbf

import (
	"bytes"
	"testing"
)

// TestVarcharExactDecodeSignificantTrailingSpaces is T-36's
// decisive test: a stored value whose real content legitimately
// ends in spaces, which the space-trim approximation shipped in
// v0.9.16 would have silently discarded.
//
// Schema mirrors S10's worked example directly: Field1 C(1),
// Field2 V(10) not null. Field2's content is "AB   " — 5 bytes,
// with 3 significant trailing spaces — stored full-width per S7's
// documented layout: content, then padding to fill the field,
// then the actual length in the field's own last byte.
func TestVarcharExactDecodeSignificantTrailingSpaces(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "FIELD1", Type: Character, Length: 1},
		{Name: "FIELD2", Type: Varchar, Length: 10},
		{Name: "_NULLFLAGS", Type: System, Length: 1, SystemColumn: true},
	}}

	// FIELD2 raw bytes: "AB   " (5, with 3 significant trailing
	// spaces) + 4 pad spaces + length byte (5). 5+4+1 = 10.
	field2Raw := []byte("AB   " + "    ")
	field2Raw = append(field2Raw, 5)
	if len(field2Raw) != 10 {
		t.Fatalf("test setup: field2Raw is %d bytes, want 10", len(field2Raw))
	}

	// _NullFlags: bit 0 is FIELD2's full bit (its only nullable/
	// varlen neighbour), set to 1 (not full).
	nullFlags := []byte{0x01}

	rec := Record{Values: []any{
		"A",
		string(field2Raw), // placeholder; decodeVarLenExact reads raw independently below
		nullFlags,
	}}
	_ = rec // constructed for shape; the real exercise is decodeVarLenExact directly

	got, err := decodeVarLenExact(field2Raw, schema.Fields[1], schema, 1, nullFlags)
	if err != nil {
		t.Fatalf("decodeVarLenExact: %v", err)
	}
	want := "AB   "
	if got != want {
		t.Errorf("decoded = %q, want %q (significant trailing spaces preserved)", got, want)
	}

	// Confirm the old approximation would have gotten this wrong,
	// so the test is actually exercising the fix rather than a
	// case where both approaches happen to agree. Worth stating
	// precisely: the old approximation is worse than "loses
	// significant trailing spaces" alone — TrimRight only strips
	// space characters, so the raw length byte (0x05 here, not a
	// space) survives untouched at the end of its output too.
	approximation := decodeValueApproximationForTest(field2Raw)
	if approximation == want {
		t.Fatal("test is not decisive: the space-trim approximation already matches the exact answer")
	}
	wantApproximation := "AB       \x05"
	if approximation != wantApproximation {
		t.Errorf("sanity check: approximation = %q, want %q", approximation, wantApproximation)
	}
}

// decodeValueApproximationForTest reproduces the pre-T-36
// space-trim approximation directly, to confirm the decisive test
// above is actually decisive rather than accidentally passing.
func decodeValueApproximationForTest(raw []byte) string {
	return string(bytes.TrimRight(raw, " "))
}

// TestVarcharExactDecodeFullField confirms the full-field case
// (bit clear) still decodes correctly and agrees with the
// approximation, since a full field has no length byte to read —
// the two code paths must produce the same answer here.
func TestVarcharExactDecodeFullField(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "FIELD1", Type: Varchar, Length: 5},
		{Name: "_NULLFLAGS", Type: System, Length: 1, SystemColumn: true},
	}}

	raw := []byte("HELLO")    // exactly 5 bytes, fills the field
	nullFlags := []byte{0x00} // bit 0 clear: full

	got, err := decodeVarLenExact(raw, schema.Fields[0], schema, 0, nullFlags)
	if err != nil {
		t.Fatalf("decodeVarLenExact: %v", err)
	}
	if got != "HELLO" {
		t.Errorf("decoded = %q, want \"HELLO\"", got)
	}
}

// TestVarbinaryExactDecodeSignificantTrailingSpaces mirrors the
// Varchar case for Varbinary, confirming the []byte return type.
func TestVarbinaryExactDecodeSignificantTrailingSpaces(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "FIELD1", Type: Varbinary, Length: 10},
		{Name: "_NULLFLAGS", Type: System, Length: 1, SystemColumn: true},
	}}

	raw := append([]byte("XY   "+"    "), 5)
	nullFlags := []byte{0x01}

	got, err := decodeVarLenExact(raw, schema.Fields[0], schema, 0, nullFlags)
	if err != nil {
		t.Fatalf("decodeVarLenExact: %v", err)
	}
	gotBytes, ok := got.([]byte)
	if !ok {
		t.Fatalf("decoded as %T, want []byte", got)
	}
	if string(gotBytes) != "XY   " {
		t.Errorf("decoded = %q, want \"XY   \"", gotBytes)
	}
}

// TestDecodeRecordUsesExactVarcharDecode is the end-to-end check:
// decodeRecord itself, not decodeVarLenExact called directly,
// produces the exact answer for a record containing a
// significant-trailing-space Varchar value.
func TestDecodeRecordUsesExactVarcharDecode(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "NAME", Type: Varchar, Length: 10},
		{Name: "_NULLFLAGS", Type: System, Length: 1, SystemColumn: true},
	}}

	field1 := append([]byte("AB   "+"    "), 5)
	nullFlags := []byte{0x01}

	src := append([]byte{activeMarker}, field1...)
	src = append(src, nullFlags...)

	rec, err := decodeRecord(src, schema)
	if err != nil {
		t.Fatalf("decodeRecord: %v", err)
	}
	got, _ := rec.Get(schema, "NAME")
	if got != "AB   " {
		t.Errorf("decodeRecord: NAME = %q, want \"AB   \"", got)
	}
}

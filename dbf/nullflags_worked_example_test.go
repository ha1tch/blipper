package dbf

import "testing"

// TestNullFlagsWorkedExample reproduces, byte-for-byte, the
// worked example in a VFP 9 book chapter (source S10,
// docs/VFP30_FORMAT.md) that gave the complete _NullFlags bit
// algorithm — the first source found this session to explain how
// Varchar/Varbinary fields interact with nullable-field bit
// counting, rather than just the ordinary-nullable-field case.
//
// Schema: Field1 C(1), Field2 V(1) not null, Field3 C(1) null,
// Field4 V(1) null. Raw record bytes and _NullFlags values are
// transcribed directly from the chapter's own hex-dump comments.
// This is a synthetic, fully reproducible oracle — no external
// specimen needed, since the source states the exact bytes.
func TestNullFlagsWorkedExample(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "FIELD1", Type: Character, Length: 1},
		{Name: "FIELD2", Type: Varchar, Length: 1},
		{Name: "FIELD3", Type: Character, Length: 1, Nullable: true},
		{Name: "FIELD4", Type: Varchar, Length: 1, Nullable: true},
		{Name: "_NULLFLAGS", Type: System, Length: 1, SystemColumn: true},
	}}

	type record struct {
		f2Full        bool // Field2 (not nullable) full?
		f3Null        bool
		f4Null        bool
		f4Full        bool
		nullFlagsByte byte
	}
	cases := []record{
		{f2Full: true, f3Null: false, f4Null: false, f4Full: true, nullFlagsByte: 0x00},
		{f2Full: false, f3Null: false, f4Null: false, f4Full: true, nullFlagsByte: 0x01},
		{f2Full: true, f3Null: false, f4Null: false, f4Full: false, nullFlagsByte: 0x04},
		{f2Full: true, f3Null: true, f4Null: false, f4Full: true, nullFlagsByte: 0x02},
		{f2Full: true, f3Null: false, f4Null: true, f4Full: false, nullFlagsByte: 0x0C},
		{f2Full: true, f3Null: true, f4Null: true, f4Full: false, nullFlagsByte: 0x0E},
		{f2Full: false, f3Null: true, f4Null: true, f4Full: false, nullFlagsByte: 0x0F},
	}

	for i, c := range cases {
		rec := Record{Values: []any{
			"A",
			"A", // Field2 content is irrelevant to the bit checks below
			"A", "A",
			[]byte{c.nullFlagsByte},
		}}

		gotF3Null, err := rec.IsNull(schema, "FIELD3")
		if err != nil {
			t.Fatalf("case %d: IsNull(FIELD3): %v", i, err)
		}
		if gotF3Null != c.f3Null {
			t.Errorf("case %d (nullflags=0x%02X): FIELD3 null = %v, want %v", i, c.nullFlagsByte, gotF3Null, c.f3Null)
		}

		gotF4Null, err := rec.IsNull(schema, "FIELD4")
		if err != nil {
			t.Fatalf("case %d: IsNull(FIELD4): %v", i, err)
		}
		if gotF4Null != c.f4Null {
			t.Errorf("case %d (nullflags=0x%02X): FIELD4 null = %v, want %v", i, c.nullFlagsByte, gotF4Null, c.f4Null)
		}

		gotF2Full, err := rec.IsFull(schema, "FIELD2")
		if err != nil {
			t.Fatalf("case %d: IsFull(FIELD2): %v", i, err)
		}
		if gotF2Full != c.f2Full {
			t.Errorf("case %d (nullflags=0x%02X): FIELD2 full = %v, want %v", i, c.nullFlagsByte, gotF2Full, c.f2Full)
		}

		gotF4Full, err := rec.IsFull(schema, "FIELD4")
		if err != nil {
			t.Fatalf("case %d: IsFull(FIELD4): %v", i, err)
		}
		if gotF4Full != c.f4Full {
			t.Errorf("case %d (nullflags=0x%02X): FIELD4 full = %v, want %v", i, c.nullFlagsByte, gotF4Full, c.f4Full)
		}
	}
}

// TestFullBitRejectsNonVarLenType guards IsFull's API surface.
func TestFullBitRejectsNonVarLenType(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "F", Type: Character, Length: 1},
	}}
	rec := Record{Values: []any{"A"}}
	if _, err := rec.IsFull(schema, "F"); err == nil {
		t.Error("IsFull succeeded on a Character field")
	}
}

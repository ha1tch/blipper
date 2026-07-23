package dbf

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testSchema(t *testing.T) Schema {
	t.Helper()

	schema := Schema{
		Fields: []Field{
			{Name: "NAME", Type: Character, Length: 20},
			{Name: "AGE", Type: Numeric, Length: 3},
			{Name: "BALANCE", Type: Numeric, Length: 10, Decimals: 2},
			{Name: "RATE", Type: Float, Length: 8, Decimals: 3},
			{Name: "ACTIVE", Type: Logical, Length: 1},
			{Name: "BORN", Type: Date, Length: 8},
		},
	}

	if err := schema.Validate(); err != nil {
		t.Fatalf("test schema invalid: %v", err)
	}

	return schema
}

func TestSchemaValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		schema Schema
	}{
		{"empty", Schema{}},
		{"no name", Schema{Fields: []Field{
			{Name: "", Type: Character, Length: 1},
		}}},
		{"long name", Schema{Fields: []Field{
			{Name: "TOOLONGNAME1", Type: Character, Length: 1},
		}}},
		{"duplicate", Schema{Fields: []Field{
			{Name: "A", Type: Character, Length: 1},
			{Name: "a", Type: Character, Length: 1},
		}}},
		{"zero char", Schema{Fields: []Field{
			{Name: "A", Type: Character, Length: 0},
		}}},
		{"bad decimals", Schema{Fields: []Field{
			{Name: "A", Type: Numeric, Length: 3, Decimals: 3},
		}}},
		{"bad date len", Schema{Fields: []Field{
			{Name: "A", Type: Date, Length: 7},
		}}},
		{"bad logical len", Schema{Fields: []Field{
			{Name: "A", Type: Logical, Length: 2},
		}}},
		{"bad type", Schema{Fields: []Field{
			{Name: "A", Type: FieldType('X'), Length: 1},
		}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.schema.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestSchemaSizes(t *testing.T) {
	schema := testSchema(t)

	// 1 + 20 + 3 + 10 + 8 + 1 + 8
	if got, want := schema.RecordSize(), uint16(51); got != want {
		t.Fatalf("RecordSize = %d, want %d", got, want)
	}

	// 32 + 6*32 + 1
	if got, want := schema.HeaderSize(), uint16(225); got != want {
		t.Fatalf("HeaderSize = %d, want %d", got, want)
	}
}

func TestRecordCodecRoundTrip(t *testing.T) {
	schema := testSchema(t)

	record := NewRecord(schema)

	born := time.Date(1984, time.March, 7, 0, 0, 0, 0, time.UTC)

	set := func(name string, v any) {
		t.Helper()
		if err := record.Set(schema, name, v); err != nil {
			t.Fatalf("Set %s: %v", name, err)
		}
	}

	set("NAME", "ADA LOVELACE")
	set("AGE", 36)
	set("BALANCE", 1234.56)
	set("RATE", -0.125)
	set("ACTIVE", true)
	set("BORN", born)

	raw := make([]byte, schema.RecordSize())

	if err := encodeRecord(raw, schema, record); err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := decodeRecord(raw, schema)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.Deleted {
		t.Fatalf("decoded record unexpectedly deleted")
	}

	get := func(name string) any {
		t.Helper()
		v, err := decoded.Get(schema, name)
		if err != nil {
			t.Fatalf("Get %s: %v", name, err)
		}
		return v
	}

	if got := get("NAME"); got != "ADA LOVELACE" {
		t.Errorf("NAME = %q", got)
	}
	if got := get("AGE"); got != int64(36) {
		t.Errorf("AGE = %v (%T)", got, got)
	}
	if got := get("BALANCE"); got != 1234.56 {
		t.Errorf("BALANCE = %v", got)
	}
	if got := get("RATE"); got != -0.125 {
		t.Errorf("RATE = %v", got)
	}
	if got := get("ACTIVE"); got != true {
		t.Errorf("ACTIVE = %v", got)
	}
	if got := get("BORN"); !got.(time.Time).Equal(born) {
		t.Errorf("BORN = %v", got)
	}
}

func TestRecordCodecWireFormat(t *testing.T) {
	schema := Schema{
		Fields: []Field{
			{Name: "NAME", Type: Character, Length: 5},
			{Name: "QTY", Type: Numeric, Length: 4},
			{Name: "OK", Type: Logical, Length: 1},
			{Name: "D", Type: Date, Length: 8},
		},
	}

	record := NewRecord(schema)
	record.Set(schema, "NAME", "AB")
	record.Set(schema, "QTY", 42)
	record.Set(schema, "OK", true)
	record.Set(schema, "D",
		time.Date(2026, time.July, 23, 0, 0, 0, 0, time.UTC))

	raw := make([]byte, schema.RecordSize())

	if err := encodeRecord(raw, schema, record); err != nil {
		t.Fatalf("encode: %v", err)
	}

	want := " AB     42T20260723"

	if got := string(raw); got != want {
		t.Fatalf("wire format = %q, want %q", got, want)
	}
}

func TestRecordCodecBlankFields(t *testing.T) {
	schema := testSchema(t)

	// A raw record of all-blank field data is the classic freshly
	// APPENDed dBASE record.
	raw := []byte(strings.Repeat(" ", int(schema.RecordSize())))

	decoded, err := decodeRecord(raw, schema)
	if err != nil {
		t.Fatalf("decode blank: %v", err)
	}

	if v, _ := decoded.Get(schema, "AGE"); v != int64(0) {
		t.Errorf("blank AGE = %v (%T)", v, v)
	}
	if v, _ := decoded.Get(schema, "BALANCE"); v != float64(0) {
		t.Errorf("blank BALANCE = %v", v)
	}
	if v, _ := decoded.Get(schema, "ACTIVE"); v != false {
		t.Errorf("blank ACTIVE = %v", v)
	}
	if v, _ := decoded.Get(schema, "BORN"); !v.(time.Time).IsZero() {
		t.Errorf("blank BORN = %v", v)
	}
}

func TestRecordCodecCharacterTruncates(t *testing.T) {
	schema := Schema{
		Fields: []Field{{Name: "C", Type: Character, Length: 3}},
	}

	record := NewRecord(schema)
	record.Set(schema, "C", "ABCDEF")

	raw := make([]byte, schema.RecordSize())

	if err := encodeRecord(raw, schema, record); err != nil {
		t.Fatalf("encode: %v", err)
	}

	if got := string(raw[1:]); got != "ABC" {
		t.Fatalf("truncated value = %q, want %q", got, "ABC")
	}
}

func TestRecordCodecNumericOverflowIsError(t *testing.T) {
	schema := Schema{
		Fields: []Field{{Name: "N", Type: Numeric, Length: 3}},
	}

	record := NewRecord(schema)
	record.Set(schema, "N", 1234)

	raw := make([]byte, schema.RecordSize())

	if err := encodeRecord(raw, schema, record); err == nil {
		t.Fatalf("expected overflow error")
	}
}

func TestRecordCodecMemoRoundTrip(t *testing.T) {
	schema := Schema{
		Fields: []Field{{Name: "M", Type: Memo, Length: 10}},
	}

	// A memo block reference as Clipper would store it.
	raw := []byte(" " + "        12")

	decoded, err := decodeRecord(raw, schema)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	out := make([]byte, len(raw))

	if err := encodeRecord(out, schema, decoded); err != nil {
		t.Fatalf("encode: %v", err)
	}

	if string(out) != string(raw) {
		t.Fatalf("memo round trip changed bytes: %q -> %q", raw, out)
	}
}

func TestRecordCodecBadMarker(t *testing.T) {
	schema := Schema{
		Fields: []Field{{Name: "C", Type: Character, Length: 1}},
	}

	_, err := decodeRecord([]byte("Xa"), schema)

	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("expected ErrInvalidRecord, got %v", err)
	}
}

func TestDeletedMarkerRoundTrip(t *testing.T) {
	schema := Schema{
		Fields: []Field{{Name: "C", Type: Character, Length: 1}},
	}

	record := NewRecord(schema)
	record.Deleted = true

	raw := make([]byte, schema.RecordSize())

	if err := encodeRecord(raw, schema, record); err != nil {
		t.Fatalf("encode: %v", err)
	}

	if raw[0] != deletedMarker {
		t.Fatalf("marker = %q", raw[0])
	}

	decoded, err := decodeRecord(raw, schema)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !decoded.Deleted {
		t.Fatalf("Deleted flag lost in round trip")
	}
}

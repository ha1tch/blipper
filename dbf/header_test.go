package dbf

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestHeaderRoundTrip(t *testing.T) {
	schema := testSchema(t)

	header := Header{
		LastUpdate: time.Date(2026, time.July, 23, 0, 0, 0, 0, time.UTC),
		CodePage:   0x01,
	}

	var buf bytes.Buffer

	err := writeHeader(
		&buf,
		header,
		schema.HeaderSize(),
		schema.RecordSize(),
		7,
	)
	if err != nil {
		t.Fatalf("writeHeader: %v", err)
	}

	if buf.Len() != fileHeaderSize {
		t.Fatalf("header is %d bytes, want %d", buf.Len(), fileHeaderSize)
	}

	got, info, err := readHeader(&buf)
	if err != nil {
		t.Fatalf("readHeader: %v", err)
	}

	if !got.LastUpdate.Equal(header.LastUpdate) {
		t.Errorf("LastUpdate = %v, want %v", got.LastUpdate, header.LastUpdate)
	}
	if got.CodePage != header.CodePage {
		t.Errorf("CodePage = %d, want %d", got.CodePage, header.CodePage)
	}
	if info.recordCount != 7 {
		t.Errorf("recordCount = %d, want 7", info.recordCount)
	}
	if info.headerSize != schema.HeaderSize() {
		t.Errorf("headerSize = %d, want %d", info.headerSize, schema.HeaderSize())
	}
	if info.recordSize != schema.RecordSize() {
		t.Errorf("recordSize = %d, want %d", info.recordSize, schema.RecordSize())
	}
}

// TestHeaderStructuralOffsets pins the exact dBASE III+ byte layout.
func TestHeaderStructuralOffsets(t *testing.T) {
	header := Header{
		LastUpdate: time.Date(1996, time.December, 5, 0, 0, 0, 0, time.UTC),
		CodePage:   0x02,
	}

	var buf bytes.Buffer

	if err := writeHeader(&buf, header, 225, 51, 260); err != nil {
		t.Fatalf("writeHeader: %v", err)
	}

	raw := buf.Bytes()

	if raw[0] != 0x03 {
		t.Errorf("version byte = 0x%02X, want 0x03", raw[0])
	}
	if raw[1] != 96 || raw[2] != 12 || raw[3] != 5 {
		t.Errorf("date bytes = %d %d %d, want 96 12 5", raw[1], raw[2], raw[3])
	}
	if n := binary.LittleEndian.Uint32(raw[4:8]); n != 260 {
		t.Errorf("record count = %d, want 260", n)
	}
	if n := binary.LittleEndian.Uint16(raw[8:10]); n != 225 {
		t.Errorf("header size = %d, want 225", n)
	}
	if n := binary.LittleEndian.Uint16(raw[10:12]); n != 51 {
		t.Errorf("record size = %d, want 51", n)
	}
	if raw[29] != 0x02 {
		t.Errorf("code page byte = 0x%02X, want 0x02", raw[29])
	}
}

func TestHeaderRejectsUnknownVersion(t *testing.T) {
	raw := make([]byte, fileHeaderSize)
	raw[0] = 0x8B // dBASE IV with memo

	if _, _, err := readHeader(bytes.NewReader(raw)); err == nil {
		t.Fatalf("expected version error")
	}
}

func TestFieldsRoundTrip(t *testing.T) {
	schema := testSchema(t)

	var buf bytes.Buffer

	if err := writeFields(&buf, schema.Fields); err != nil {
		t.Fatalf("writeFields: %v", err)
	}

	// 32 bytes per descriptor plus the 0x0D terminator.
	if want := len(schema.Fields)*fieldDescriptorSize + 1; buf.Len() != want {
		t.Fatalf("descriptors are %d bytes, want %d", buf.Len(), want)
	}

	fields, err := readFields(&buf)
	if err != nil {
		t.Fatalf("readFields: %v", err)
	}

	if len(fields) != len(schema.Fields) {
		t.Fatalf("read %d fields, want %d", len(fields), len(schema.Fields))
	}

	for i, want := range schema.Fields {
		if fields[i] != want {
			t.Errorf("field %d = %+v, want %+v", i, fields[i], want)
		}
	}
}

func TestFieldDescriptorLayout(t *testing.T) {
	var buf bytes.Buffer

	field := Field{Name: "BALANCE", Type: Numeric, Length: 10, Decimals: 2}

	if err := writeField(&buf, field); err != nil {
		t.Fatalf("writeField: %v", err)
	}

	raw := buf.Bytes()

	if len(raw) != fieldDescriptorSize {
		t.Fatalf("descriptor is %d bytes, want %d", len(raw), fieldDescriptorSize)
	}

	if got := string(raw[:7]); got != "BALANCE" {
		t.Errorf("name bytes = %q", got)
	}
	if raw[7] != 0 {
		t.Errorf("name not NUL terminated: 0x%02X", raw[7])
	}
	if raw[11] != 'N' {
		t.Errorf("type byte = %q, want 'N'", raw[11])
	}
	if raw[16] != 10 {
		t.Errorf("length byte = %d, want 10", raw[16])
	}
	if raw[17] != 2 {
		t.Errorf("decimals byte = %d, want 2", raw[17])
	}
}

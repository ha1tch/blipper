package dbf

import (
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

// memFile is an in-memory io.ReadWriteSeeker for tests.
type memFile struct {
	data []byte
	pos  int64
}

func (m *memFile) Read(p []byte) (int, error) {
	if m.pos >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += int64(n)
	return n, nil
}

func (m *memFile) Write(p []byte) (int, error) {
	end := m.pos + int64(len(p))
	if end > int64(len(m.data)) {
		grown := make([]byte, end)
		copy(grown, m.data)
		m.data = grown
	}
	copy(m.data[m.pos:end], p)
	m.pos = end
	return len(p), nil
}

func (m *memFile) Seek(offset int64, whence int) (int64, error) {
	var base int64
	switch whence {
	case io.SeekStart:
		base = 0
	case io.SeekCurrent:
		base = m.pos
	case io.SeekEnd:
		base = int64(len(m.data))
	default:
		return 0, fmt.Errorf("bad whence %d", whence)
	}
	pos := base + offset
	if pos < 0 {
		return 0, fmt.Errorf("negative position")
	}
	m.pos = pos
	return pos, nil
}

func newTestTable(t *testing.T) (*Table, *memFile) {
	t.Helper()

	file := &memFile{}

	table, err := Create(file, testSchema(t))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	return table, file
}

func appendPerson(t *testing.T, table *Table, name string, age int) uint32 {
	t.Helper()

	schema := table.Schema()
	record := NewRecord(schema)

	if err := record.Set(schema, "NAME", name); err != nil {
		t.Fatalf("Set NAME: %v", err)
	}
	if err := record.Set(schema, "AGE", age); err != nil {
		t.Fatalf("Set AGE: %v", err)
	}

	recno, err := table.Append(record)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	return recno
}

func TestCreateFileStructure(t *testing.T) {
	table, file := newTestTable(t)

	schema := table.Schema()

	// header + descriptors + 0x0D (inside HeaderSize) + 0x1A
	if want := int(schema.HeaderSize()) + 1; len(file.data) != want {
		t.Fatalf("empty table is %d bytes, want %d", len(file.data), want)
	}

	if file.data[schema.HeaderSize()-1] != headerTerminator {
		t.Errorf("missing header terminator")
	}
	if file.data[len(file.data)-1] != fileTerminator {
		t.Errorf("missing EOF marker")
	}
}

func TestAppendGetRoundTrip(t *testing.T) {
	table, file := newTestTable(t)

	r1 := appendPerson(t, table, "FIRST", 10)
	r2 := appendPerson(t, table, "SECOND", 20)

	if r1 != 1 || r2 != 2 {
		t.Fatalf("recnos = %d, %d; want 1, 2", r1, r2)
	}

	if table.RecordCount() != 2 {
		t.Fatalf("RecordCount = %d, want 2", table.RecordCount())
	}

	// EOF marker must follow the last record.
	if file.data[len(file.data)-1] != fileTerminator {
		t.Errorf("EOF marker missing after append")
	}

	record, err := table.Get(2)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	schema := table.Schema()

	if v, _ := record.Get(schema, "NAME"); v != "SECOND" {
		t.Errorf("NAME = %v", v)
	}
	if v, _ := record.Get(schema, "AGE"); v != int64(20) {
		t.Errorf("AGE = %v", v)
	}
}

func TestGetBounds(t *testing.T) {
	table, _ := newTestTable(t)

	appendPerson(t, table, "ONLY", 1)

	if _, err := table.Get(0); err == nil {
		t.Errorf("Get(0) should fail: record numbers are one-based")
	}

	_, err := table.Get(2)
	if !errors.Is(err, ErrEOF) {
		t.Errorf("Get past end: got %v, want ErrEOF", err)
	}
}

func TestPutOverwrites(t *testing.T) {
	table, _ := newTestTable(t)

	appendPerson(t, table, "BEFORE", 1)

	schema := table.Schema()
	record := NewRecord(schema)
	record.Set(schema, "NAME", "AFTER")
	record.Set(schema, "AGE", 2)

	if err := table.Put(1, record); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := table.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if v, _ := got.Get(schema, "NAME"); v != "AFTER" {
		t.Errorf("NAME = %v", v)
	}

	if table.RecordCount() != 1 {
		t.Errorf("Put changed record count to %d", table.RecordCount())
	}
}

func TestDeleteAndUndelete(t *testing.T) {
	table, _ := newTestTable(t)

	appendPerson(t, table, "VICTIM", 1)

	if err := table.Delete(1); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	record, err := table.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !record.Deleted {
		t.Fatalf("record not marked deleted")
	}

	// Data must be preserved (design01 §9).
	if v, _ := record.Get(table.Schema(), "NAME"); v != "VICTIM" {
		t.Errorf("deleted record lost data: NAME = %v", v)
	}

	if err := table.Undelete(1); err != nil {
		t.Fatalf("Undelete: %v", err)
	}

	record, _ = table.Get(1)
	if record.Deleted {
		t.Fatalf("record still deleted after Undelete")
	}
}

func TestOpenRereadsCreatedFile(t *testing.T) {
	table, file := newTestTable(t)

	appendPerson(t, table, "PERSISTED", 33)

	reopened, err := Open(file)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if reopened.RecordCount() != 1 {
		t.Fatalf("RecordCount = %d, want 1", reopened.RecordCount())
	}

	if len(reopened.Schema().Fields) != len(table.Schema().Fields) {
		t.Fatalf("schema field count changed across reopen")
	}

	record, err := reopened.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if v, _ := record.Get(reopened.Schema(), "NAME"); v != "PERSISTED" {
		t.Errorf("NAME = %v", v)
	}
}

func TestOpenRejectsRecordSizeMismatch(t *testing.T) {
	_, file := newTestTable(t)

	// Corrupt the on-disk record size (bytes 10..11).
	file.data[10] = 0xFF
	file.data[11] = 0x00

	if _, err := Open(file); err == nil {
		t.Fatalf("expected record size mismatch error")
	}
}

func TestOpenHonoursPaddedHeader(t *testing.T) {
	table, file := newTestTable(t)

	appendPerson(t, table, "PADDED", 1)

	schema := table.Schema()

	// Rebuild the file with 16 bytes of padding between the header
	// terminator and record data, the way some writers do.
	const pad = 16

	recSize := int(schema.RecordSize())
	hdrSize := int(schema.HeaderSize())

	padded := &memFile{}
	padded.Write(file.data[:hdrSize])
	padded.Write(make([]byte, pad))
	padded.Write(file.data[hdrSize : hdrSize+recSize])
	padded.Write([]byte{fileTerminator})

	// Patch the on-disk header size.
	newHdr := uint16(hdrSize + pad)
	padded.data[8] = byte(newHdr)
	padded.data[9] = byte(newHdr >> 8)

	reopened, err := Open(padded)
	if err != nil {
		t.Fatalf("Open padded: %v", err)
	}

	record, err := reopened.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v, _ := record.Get(reopened.Schema(), "NAME"); v != "PADDED" {
		t.Errorf("NAME = %v", v)
	}

	// A header rewrite must preserve the padded header size.
	if err := reopened.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if got := uint16(padded.data[8]) | uint16(padded.data[9])<<8; got != newHdr {
		t.Fatalf("flush shrank header size to %d, want %d", got, newHdr)
	}
}

func TestCursorTraversal(t *testing.T) {
	table, _ := newTestTable(t)

	names := []string{"A", "B", "C", "D"}

	for i, name := range names {
		appendPerson(t, table, name, i)
	}

	table.Delete(2)

	cursor := table.Cursor()
	schema := table.Schema()

	var visited []string
	var deletedSeen int

	for cursor.Next() {
		record := cursor.Record()
		if record.Deleted {
			deletedSeen++
		}
		v, _ := record.Get(schema, "NAME")
		visited = append(visited, v.(string))

		if want := uint32(len(visited)); cursor.Recno() != want {
			t.Errorf("Recno = %d, want %d", cursor.Recno(), want)
		}
	}

	if err := cursor.Err(); err != nil {
		t.Fatalf("cursor error: %v", err)
	}

	if len(visited) != len(names) {
		t.Fatalf("visited %d records, want %d", len(visited), len(names))
	}

	// The cursor exposes deleted records; callers filter.
	if deletedSeen != 1 {
		t.Errorf("saw %d deleted records, want 1", deletedSeen)
	}

	for i, name := range names {
		if visited[i] != name {
			t.Errorf("position %d = %q, want %q", i, visited[i], name)
		}
	}
}

func TestFlushStampsDate(t *testing.T) {
	table, file := newTestTable(t)

	if err := table.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	now := time.Now()

	if file.data[1] != byte(now.Year()-1900) {
		t.Errorf("year byte = %d, want %d", file.data[1], now.Year()-1900)
	}
	if file.data[2] != byte(now.Month()) {
		t.Errorf("month byte = %d, want %d", file.data[2], now.Month())
	}
}

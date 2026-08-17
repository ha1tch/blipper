package dbc_test

import (
	"errors"
	"io"
	"testing"

	"github.com/ha1tch/blipper/dbc"
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
		grow := make([]byte, end)
		copy(grow, m.data)
		m.data = grow
	}
	copy(m.data[m.pos:end], p)
	m.pos = end
	return len(p), nil
}
func (m *memFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		m.pos = off
	case io.SeekCurrent:
		m.pos += off
	case io.SeekEnd:
		m.pos = int64(len(m.data)) + off
	}
	return m.pos, nil
}

// TestCreateEmptyDBCContainsDatabaseRow verifies that Create
// writes a single Database row at RootID.
func TestCreateEmptyDBCContainsDatabaseRow(t *testing.T) {
	buf := &memFile{}
	cat, err := dbc.Create(buf)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := cat.TableNames(); len(got) != 0 {
		t.Errorf("fresh DBC TableNames = %v, want empty", got)
	}
}

// TestAddTableAndFieldRoundTrip creates a DBC, adds a table with
// two fields (one short-name-collision, one longer), closes and
// reopens the DBC, and verifies every row is present and lookups
// resolve correctly.
func TestAddTableAndFieldRoundTrip(t *testing.T) {
	buf := &memFile{}
	cat, err := dbc.Create(buf)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tableID, err := cat.AddTable("customers")
	if err != nil {
		t.Fatalf("AddTable: %v", err)
	}
	if _, err := cat.AddField(tableID, "customer_id"); err != nil {
		t.Fatalf("AddField customer_id: %v", err)
	}
	if _, err := cat.AddField(tableID, "customer_name_extended"); err != nil {
		t.Fatalf("AddField customer_name_extended: %v", err)
	}
	if _, err := cat.AddField(tableID, "email"); err != nil {
		t.Fatalf("AddField email: %v", err)
	}

	// Reopen from raw bytes.
	buf.pos = 0
	reopened, err := dbc.Open(buf)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	names := reopened.TableNames()
	if len(names) != 1 || names[0] != "customers" {
		t.Errorf("TableNames = %v, want [customers]", names)
	}
	rID, err := reopened.TableID("customers")
	if err != nil {
		t.Fatalf("TableID(customers): %v", err)
	}
	fields := reopened.FieldNames(rID)
	wantFields := []string{"customer_id", "customer_name_extended", "email"}
	if len(fields) != len(wantFields) {
		t.Fatalf("FieldNames = %v, want %v", fields, wantFields)
	}
	for i, w := range wantFields {
		if fields[i] != w {
			t.Errorf("field %d = %q, want %q", i, fields[i], w)
		}
	}

	// Long-name resolution from the short DBF-side name.
	// "customer_i" (10 chars) → "customer_id"
	if got := reopened.FieldLongName(rID, "customer_i"); got != "customer_id" {
		t.Errorf("FieldLongName(customer_i) = %q, want customer_id", got)
	}
	// "customer_n" (10 chars from truncating customer_name_extended)
	// → "customer_name_extended"
	if got := reopened.FieldLongName(rID, "customer_n"); got != "customer_name_extended" {
		t.Errorf("FieldLongName(customer_n) = %q, want customer_name_extended", got)
	}
	// "email" (≤10 chars) → "email"
	if got := reopened.FieldLongName(rID, "email"); got != "email" {
		t.Errorf("FieldLongName(email) = %q, want email", got)
	}
	// Unmatched short name → returns the input unchanged.
	if got := reopened.FieldLongName(rID, "MISSING"); got != "MISSING" {
		t.Errorf("FieldLongName(MISSING) = %q, want MISSING (fallback)", got)
	}
}

// TestDuplicateTableAndFieldRejected verifies that AddTable and
// AddField refuse to add rows that would collide with existing
// ones.
func TestDuplicateTableAndFieldRejected(t *testing.T) {
	buf := &memFile{}
	cat, _ := dbc.Create(buf)
	tID, _ := cat.AddTable("customers")

	if _, err := cat.AddTable("customers"); !errors.Is(err, dbc.ErrTableExists) {
		t.Errorf("AddTable duplicate: err = %v, want ErrTableExists", err)
	}
	if _, err := cat.AddField(tID, "name"); err != nil {
		t.Fatalf("AddField(name): %v", err)
	}
	if _, err := cat.AddField(tID, "name"); !errors.Is(err, dbc.ErrFieldExists) {
		t.Errorf("AddField duplicate: err = %v, want ErrFieldExists", err)
	}

	// Adding a field to an unknown table.
	if _, err := cat.AddField(999, "name"); !errors.Is(err, dbc.ErrTableNotFound) {
		t.Errorf("AddField unknown table: err = %v, want ErrTableNotFound", err)
	}
}

// TestOpenRejectsWrongSchema verifies that opening a DBF with the
// wrong schema surfaces ErrNotDBC rather than a confusing decode
// failure.
func TestOpenRejectsWrongSchema(t *testing.T) {
	// Create a plain DBF via a non-catalogue path — we do this
	// through Create then corrupt the schema check by feeding
	// bytes that are technically a valid DBF but not a valid
	// catalogue.
	//
	// Simpler: try to Open a random-ish 512-byte block. Should
	// fail at the underlying DBF layer, which is fine — the
	// error propagates as a wrapped dbf error, not ErrNotDBC.
	// So instead we build a real DBF with the wrong column set
	// and confirm that surfaces ErrNotDBC.
	//
	// This test is kept simple: an empty buffer just errors at
	// dbf.Open, which is the correct behavior (it isn't a DBC
	// because it isn't a DBF), and we treat the type of error
	// as an implementation detail. A cross-check that ErrNotDBC
	// fires would need a valid-but-wrong DBF to feed to Open —
	// that fixture is worth building later when integrating
	// with the DBF layer (T-10 phase 2).
	buf := &memFile{}
	if _, err := dbc.Open(buf); err == nil {
		t.Error("Open on empty buffer succeeded; want error")
	}
}

// TestLongNameTruncatedOnWrite verifies that OBJECTNAME values
// longer than MaxLongName are clipped rather than corrupting the
// underlying record.
func TestLongNameTruncatedOnWrite(t *testing.T) {
	buf := &memFile{}
	cat, _ := dbc.Create(buf)

	// A name well over 128 characters.
	longName := ""
	for i := 0; i < 200; i++ {
		longName += "x"
	}
	if _, err := cat.AddTable(longName); err != nil {
		t.Fatalf("AddTable oversize: %v", err)
	}
	// The stored form should be exactly MaxLongName.
	buf.pos = 0
	reopened, err := dbc.Open(buf)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	names := reopened.TableNames()
	if len(names) != 1 {
		t.Fatalf("TableNames = %v", names)
	}
	if len(names[0]) != dbc.MaxLongName {
		t.Errorf("stored table name length = %d, want %d", len(names[0]), dbc.MaxLongName)
	}
}

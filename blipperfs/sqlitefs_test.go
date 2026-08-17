package blipperfs

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/ha1tch/blipper/cdx"
	"github.com/ha1tch/blipper/dbf"
	"github.com/ha1tch/blipper/sqlitefs"
)

// TestSQLiteTablespace mirrors TestFATImageAsTablespace: a whole
// xBase dataset inside a single container, created and reopened
// through the ordinary API. The difference that matters is that
// this container is transactional.
func TestSQLiteTablespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tablespace.db")

	fs, err := SQLiteTablespace(path)
	if err != nil {
		t.Fatalf("SQLiteTablespace: %v", err)
	}
	s := NewSession(fs)

	spec := TableSpec{
		Schema: dbf.Schema{Fields: []dbf.Field{
			{Name: "CUSTOMER_C", Type: dbf.Character, Length: 10},
			{Name: "NOTES", Type: dbf.Memo, Length: 10},
		}},
		MemoFormat:    dbf.MemoFormatFPT,
		FPTBlockSize:  64,
		TableLongName: "customers",
		LongNames:     map[string]string{"CUSTOMER_C": "customer_code"},
		Tags: []cdx.TagSpec{{
			Name:    "BYCODE",
			KeyExpr: "CUSTOMER_C",
			KeyLen:  10,
			Entries: []cdx.Entry{{Key: []byte("ALPHA"), RecNo: 1}},
		}},
	}

	area, err := s.CreateTable("CUST", "CUSTOMER", spec)
	if err != nil {
		t.Fatalf("CreateTable into SQLite tablespace: %v", err)
	}

	rec := dbf.NewRecord(area.Table().Schema())
	rec.Set(area.Table().Schema(), "CUSTOMER_C", "ALPHA")
	if _, err := area.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := area.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	memo := []byte("a memo stored in a SQLite tablespace")
	if err := area.MemoSet("NOTES", memo); err != nil {
		t.Fatalf("MemoSet: %v", err)
	}
	if err := area.Table().Flush(); err != nil {
		t.Fatalf("table Flush: %v", err)
	}

	// Commit the container: every file lands together.
	fl, ok := fs.(Flusher)
	if !ok {
		t.Fatal("SQLite-backed FileSet does not implement Flusher")
	}
	if err := fl.Flush(); err != nil {
		t.Fatalf("FileSet Flush: %v", err)
	}

	for _, want := range []string{"CUSTOMER.DBF", "CUSTOMER.FPT", "CUSTOMER.DBC", "CUSTOMER.CDX"} {
		if !fs.Exists(want) {
			t.Errorf("%s missing; tablespace holds %v", want, fs.List())
		}
	}

	// Close and reopen the database entirely.
	if closer, ok := fs.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	reopened, err := SQLiteTablespace(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	s2, err := OpenFileSet(reopened)
	if err != nil {
		t.Fatalf("OpenFileSet: %v", err)
	}
	area2, err := s2.Area("CUSTOMER")
	if err != nil {
		t.Fatalf("Area(CUSTOMER): %v", err)
	}

	if area2.Memo() == nil {
		t.Error("memo sibling not resolved from tablespace")
	}
	if area2.Catalogue() == nil {
		t.Error("catalogue sibling not resolved from tablespace")
	}
	if area2.CDX() == nil {
		t.Error("CDX sibling not resolved from tablespace")
	}
	if got := area2.CatalogueLongName("CUSTOMER_C"); got != "customer_code" {
		t.Errorf("CatalogueLongName = %q, want customer_code", got)
	}

	if err := area2.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	got, err := area2.MemoGet("NOTES")
	if err != nil {
		t.Fatalf("MemoGet: %v", err)
	}
	if !bytes.Equal(got, memo) {
		t.Errorf("memo = %q, want %q", got, memo)
	}
}

// TestSQLiteTablespaceChunkSizeOption verifies the option reaches
// the store.
func TestSQLiteTablespaceChunkSizeOption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chunked.db")
	fs, err := SQLiteTablespace(path, sqlitefs.WithChunkSize(4096))
	if err != nil {
		t.Fatalf("SQLiteTablespace: %v", err)
	}
	store, ok := fs.(interface{ Store() *sqlitefs.FS })
	if !ok {
		t.Fatal("adapter does not expose Store()")
	}
	if got := store.Store().ChunkSize(); got != 4096 {
		t.Errorf("ChunkSize = %d, want 4096", got)
	}
}

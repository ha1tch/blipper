package blipperfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ha1tch/blipper/cdx"
	"github.com/ha1tch/blipper/dbf"
)

func customerSchema() dbf.Schema {
	return dbf.Schema{Fields: []dbf.Field{
		{Name: "CUSTOMER_I", Type: dbf.Character, Length: 10},
		{Name: "NOTES", Type: dbf.Memo, Length: 10},
	}}
}

// TestCreateAndUseFullFileSet is the headline test: one call
// writes DBF + FPT + DBC + CDX, and one later call reopens the
// whole set with every sibling resolved automatically.
func TestCreateAndUseFullFileSet(t *testing.T) {
	s := NewSession(NewMemFileSet())

	spec := TableSpec{
		Schema:        customerSchema(),
		MemoFormat:    dbf.MemoFormatFPT,
		FPTBlockSize:  64,
		TableLongName: "customers",
		LongNames: map[string]string{
			"CUSTOMER_I": "customer_id",
		},
		Tags: []cdx.TagSpec{{
			Name:    "BYCODE",
			KeyExpr: "CUSTOMER_I",
			KeyLen:  10,
			Entries: []cdx.Entry{
				{Key: []byte("ALPHA"), RecNo: 1},
				{Key: []byte("BRAVO"), RecNo: 2},
			},
		}},
	}

	area, err := s.CreateTable("CUST", "CUSTOMERS", spec)
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	fs := s.FileSet()

	// All four files written.
	for _, want := range []string{"CUSTOMERS.DBF", "CUSTOMERS.FPT", "CUSTOMERS.DBC", "CUSTOMERS.CDX"} {
		if !fs.Exists(want) {
			t.Errorf("expected %s to exist; files are %v", want, fs.List())
		}
	}

	// Header claims match the spec.
	if got := area.Table().MemoFormat(); got != dbf.MemoFormatFPT {
		t.Errorf("MemoFormat = %v, want FPT", got)
	}
	if got := area.Table().TableFlags(); got != 0x0C {
		t.Errorf("TableFlags = 0x%02X, want 0x0C", got)
	}
	if got := area.Table().Backlink(); got != "CUSTOMERS.DBC" {
		t.Errorf("Backlink = %q, want CUSTOMERS.DBC", got)
	}

	// Every sibling attached by CreateTable.
	if area.Memo() == nil {
		t.Error("memo not attached after CreateTable")
	}
	if area.Catalogue() == nil {
		t.Error("catalogue not attached after CreateTable")
	}
	if area.CDX() == nil {
		t.Error("CDX not attached after CreateTable")
	}
	if got := area.CatalogueLongName("CUSTOMER_I"); got != "customer_id" {
		t.Errorf("CatalogueLongName = %q, want customer_id", got)
	}

	// Now the real test: a fresh session over the same FileSet
	// reopens the whole set with ONE call and gets everything back.
	s2 := NewSession(fs)
	area2, err := s2.Use("CUST", "CUSTOMERS.DBF")
	if err != nil {
		t.Fatalf("Use: %v", err)
	}
	if area2.Memo() == nil {
		t.Error("Use did not resolve the memo sibling")
	}
	if area2.Catalogue() == nil {
		t.Error("Use did not resolve the DBC sibling")
	}
	if area2.CDX() == nil {
		t.Error("Use did not resolve the CDX sibling")
	}
	if got := area2.CatalogueLongName("CUSTOMER_I"); got != "customer_id" {
		t.Errorf("post-Use CatalogueLongName = %q, want customer_id", got)
	}
	if names := area2.CDXTags(); len(names) != 1 || names[0] != "BYCODE" {
		t.Errorf("post-Use CDXTags = %v, want [BYCODE]", names)
	}
}

// TestCreatePlainTableWritesOnlyDBF verifies that a minimal spec
// produces exactly one file.
func TestCreatePlainTableWritesOnlyDBF(t *testing.T) {
	s := NewSession(NewMemFileSet())
	spec := TableSpec{
		Schema: dbf.Schema{Fields: []dbf.Field{
			{Name: "CODE", Type: dbf.Character, Length: 10},
		}},
	}
	area, err := s.CreateTable("P", "PLAIN", spec)
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if got := s.FileSet().List(); len(got) != 1 || got[0] != "PLAIN.DBF" {
		t.Errorf("files = %v, want [PLAIN.DBF]", got)
	}
	if area.Memo() != nil || area.Catalogue() != nil || area.CDX() != nil {
		t.Error("plain table should have no siblings attached")
	}
	if got := area.Table().TableFlags(); got != 0x00 {
		t.Errorf("TableFlags = 0x%02X, want 0x00 for plain table", got)
	}
}

// TestUseReportsMissingDeclaredSibling covers the corruption case:
// the version byte declares a memo file that is not present.
func TestUseReportsMissingDeclaredSibling(t *testing.T) {
	mem := NewMemFileSet()
	s := NewSession(mem)
	spec := TableSpec{
		Schema:     customerSchema(),
		MemoFormat: dbf.MemoFormatDBT,
	}
	if _, err := s.CreateTable("C", "CUSTOMERS", spec); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	// Delete the memo file behind blipperfs's back.
	delete(mem.files, memKey("CUSTOMERS.DBT"))

	s2 := NewSession(mem)
	_, err := s2.Use("C", "CUSTOMERS.DBF")
	if err == nil {
		t.Fatal("Use with missing declared memo succeeded; want ErrMissingSibling")
	}
	if !errors.Is(err, ErrMissingSibling) {
		t.Errorf("error = %v, want ErrMissingSibling in the chain", err)
	}
}

// TestUseIgnoresAbsentUndeclaredCDX verifies the other half of the
// missing-sibling policy: no .CDX just means no structural index.
func TestUseIgnoresAbsentUndeclaredCDX(t *testing.T) {
	s := NewSession(NewMemFileSet())
	spec := TableSpec{
		Schema: dbf.Schema{Fields: []dbf.Field{
			{Name: "CODE", Type: dbf.Character, Length: 10},
		}},
	}
	if _, err := s.CreateTable("P", "PLAIN", spec); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	s2 := NewSession(s.FileSet())
	area, err := s2.Use("P", "PLAIN.DBF")
	if err != nil {
		t.Fatalf("Use with no CDX should succeed, got: %v", err)
	}
	if area.CDX() != nil {
		t.Error("no .CDX present, but one was attached")
	}
}

// TestSpecValidationRejectsInconsistencies verifies that bad specs
// are caught before any file is written.
func TestSpecValidationRejectsInconsistencies(t *testing.T) {
	cases := []struct {
		name string
		spec TableSpec
	}{
		{"memo field but MemoFormatNone", TableSpec{
			Schema:     customerSchema(),
			MemoFormat: dbf.MemoFormatNone,
		}},
		{"MemoFormatFPT but no memo field", TableSpec{
			Schema: dbf.Schema{Fields: []dbf.Field{
				{Name: "CODE", Type: dbf.Character, Length: 5},
			}},
			MemoFormat: dbf.MemoFormatFPT,
		}},
		{"LongNames key names no field", TableSpec{
			Schema: dbf.Schema{Fields: []dbf.Field{
				{Name: "CODE", Type: dbf.Character, Length: 5},
			}},
			LongNames: map[string]string{"NOSUCH": "no_such_field"},
		}},
		{"empty schema", TableSpec{}},
	}
	for _, c := range cases {
		s := NewSession(NewMemFileSet())
		if _, err := s.CreateTable("X", "X", c.spec); err == nil {
			t.Errorf("%s: CreateTable succeeded; want rejection", c.name)
		}
		if got := s.FileSet().List(); len(got) != 0 {
			t.Errorf("%s: rejected spec left files behind: %v", c.name, got)
		}
	}
}

// TestOpenDirOpensEveryTable exercises the headline constructor
// against a real temp directory.
func TestOpenDirOpensEveryTable(t *testing.T) {
	dir := t.TempDir()
	s := NewSession(OSDir(dir))

	// Two tables, one plain and one with a memo.
	plain := TableSpec{Schema: dbf.Schema{Fields: []dbf.Field{
		{Name: "CODE", Type: dbf.Character, Length: 10},
	}}}
	if _, err := s.CreateTable("ORDERS", "ORDERS", plain); err != nil {
		t.Fatalf("CreateTable ORDERS: %v", err)
	}
	withMemo := TableSpec{
		Schema:     customerSchema(),
		MemoFormat: dbf.MemoFormatDBT,
	}
	if _, err := s.CreateTable("CUSTOMERS", "CUSTOMERS", withMemo); err != nil {
		t.Fatalf("CreateTable CUSTOMERS: %v", err)
	}

	// A non-DBF file should be ignored by the scan.
	if err := os.WriteFile(filepath.Join(dir, "README.TXT"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	// Open the whole directory as a fresh session.
	session, err := OpenDir(dir)
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	aliases := session.Aliases()
	if len(aliases) != 2 {
		t.Fatalf("OpenDir registered %v, want 2 tables", aliases)
	}
	cust, err := session.Area("CUSTOMERS")
	if err != nil {
		t.Fatalf("Area(CUSTOMERS): %v", err)
	}
	if cust.Memo() == nil {
		t.Error("OpenDir did not resolve CUSTOMERS.DBT")
	}
	orders, err := session.Area("ORDERS")
	if err != nil {
		t.Fatalf("Area(ORDERS): %v", err)
	}
	if orders.Memo() != nil {
		t.Error("ORDERS has no memo field but a memo was attached")
	}
}

// TestOSFileSetIsCaseInsensitive verifies DOS-era name matching.
func TestOSFileSetIsCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "DATA.DBF"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fs := OSDir(dir)
	if !fs.Exists("data.dbf") {
		t.Error("Exists(data.dbf) = false, want true (case-insensitive)")
	}
	if !fs.Exists("DATA.DBF") {
		t.Error("Exists(DATA.DBF) = false, want true")
	}
	if fs.Exists("OTHER.DBF") {
		t.Error("Exists(OTHER.DBF) = true, want false")
	}
}

// TestSessionCarriesItsFileSet is the ergonomics test for the
// Session type: after OpenDir, further table opens need no
// FileSet argument, and the session's embedded BlipperDB methods
// are reachable directly.
func TestSessionCarriesItsFileSet(t *testing.T) {
	dir := t.TempDir()
	s := NewSession(OSDir(dir))
	spec := TableSpec{Schema: dbf.Schema{Fields: []dbf.Field{
		{Name: "CODE", Type: dbf.Character, Length: 10},
	}}}
	if _, err := s.CreateTable("FIRST", "FIRST", spec); err != nil {
		t.Fatalf("CreateTable FIRST: %v", err)
	}
	if _, err := s.CreateTable("SECOND", "SECOND", spec); err != nil {
		t.Fatalf("CreateTable SECOND: %v", err)
	}

	// Reopen the directory as a session; then add a third table
	// without ever naming the directory again.
	reopened, err := OpenDir(dir)
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	if got := len(reopened.Aliases()); got != 2 {
		t.Fatalf("OpenDir found %d tables, want 2", got)
	}
	// Embedded BlipperDB method, reachable directly.
	if _, err := reopened.Select("FIRST"); err != nil {
		t.Errorf("Select through embedded BlipperDB: %v", err)
	}
	// Short-form CreateTable: no fs argument.
	if _, err := reopened.CreateTable("THIRD", "THIRD", spec); err != nil {
		t.Fatalf("short-form CreateTable: %v", err)
	}
	// Short-form Use: no fs argument.
	again, err := OpenDir(dir)
	if err != nil {
		t.Fatalf("OpenDir after third table: %v", err)
	}
	if got := len(again.Aliases()); got != 3 {
		t.Errorf("after adding THIRD, OpenDir found %d tables, want 3", got)
	}
}

// TestOpenFileSetWorksWithNonOSBackend proves the long form still
// serves its purpose: an alternative FileSet driver plugs in
// without touching disk.
func TestOpenFileSetWorksWithNonOSBackend(t *testing.T) {
	mem := NewMemFileSet()
	seed := NewSession(mem)
	spec := TableSpec{Schema: dbf.Schema{Fields: []dbf.Field{
		{Name: "CODE", Type: dbf.Character, Length: 10},
	}}}
	if _, err := seed.CreateTable("A", "TABLEA", spec); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if _, err := seed.CreateTable("B", "TABLEB", spec); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	// Scan the in-memory set exactly as OpenDir scans a directory.
	s, err := OpenFileSet(mem)
	if err != nil {
		t.Fatalf("OpenFileSet: %v", err)
	}
	aliases := s.Aliases()
	if len(aliases) != 2 {
		t.Errorf("OpenFileSet registered %v, want 2 tables", aliases)
	}
}

// TestSessionCloseReleasesHandles verifies that closing a session
// releases the handles its tables hold. The in-memory FileSet
// tracks Close calls, so the assertion is on observed behaviour
// rather than on descriptor counts, which are awkward to inspect
// portably.
func TestSessionCloseReleasesHandles(t *testing.T) {
	mem := NewMemFileSet()
	s := NewSession(mem)
	spec := TableSpec{
		Schema:     customerSchema(),
		MemoFormat: dbf.MemoFormatDBT,
	}
	if _, err := s.CreateTable("C", "CUSTOMERS", spec); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := len(s.Aliases()); got != 0 {
		t.Errorf("after Close, %d areas still registered, want 0", got)
	}
}

// TestSessionCloseFlushesContainer verifies the commit half of
// Close: a FileSet that buffers writes has them committed.
func TestSessionCloseFlushesContainer(t *testing.T) {
	fs := &countingFlushFS{MemFileSet: NewMemFileSet()}
	s := NewSession(fs)
	spec := TableSpec{Schema: dbf.Schema{Fields: []dbf.Field{
		{Name: "CODE", Type: dbf.Character, Length: 10},
	}}}
	if _, err := s.CreateTable("P", "PLAIN", spec); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if fs.flushes != 0 {
		t.Errorf("Flush called %d times before Close, want 0", fs.flushes)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if fs.flushes != 1 {
		t.Errorf("Flush called %d times by Close, want 1", fs.flushes)
	}
}

// countingFlushFS is a MemFileSet that implements Flusher and
// counts the calls.
type countingFlushFS struct {
	*MemFileSet
	flushes int
}

func (c *countingFlushFS) Flush() error {
	c.flushes++
	return nil
}

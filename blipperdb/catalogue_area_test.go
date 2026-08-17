package blipperdb

import (
	"errors"
	"testing"

	"github.com/ha1tch/blipper/dbc"
	"github.com/ha1tch/blipper/dbf"
)

// customerSchema is a two-field DBF schema whose short field
// names would collide under naive truncation ("customer_i" from
// two different long names). Used to exercise the shortname
// disambiguation path.
func customerSchema() dbf.Schema {
	return dbf.Schema{Fields: []dbf.Field{
		{Name: "CUSTOMER_I", Type: dbf.Character, Length: 10}, // was "customer_id"
		{Name: "CUSTOMER_N", Type: dbf.Character, Length: 30}, // was "customer_name"
	}}
}

func TestCatalogueEndToEndRoundTrip(t *testing.T) {
	// Create a DBC-owned DBF via the raw dbf package (CreateWithBacklink),
	// then wrap it in a BlipperDB area. This is the pattern a
	// caller would use before blipperdb learns a higher-level
	// "create a paired DBF+DBC" helper.
	dbfFile := &memFile{}
	dbfTable, err := dbf.CreateWithBacklink(dbfFile, customerSchema(), "CUSTOMERS.DBC")
	if err != nil {
		t.Fatalf("CreateWithBacklink: %v", err)
	}
	if dbfTable.TableFlags() != 0x0C {
		t.Errorf("DBF TableFlags = 0x%02X, want 0x0C", dbfTable.TableFlags())
	}
	if dbfTable.Backlink() != "CUSTOMERS.DBC" {
		t.Errorf("DBF Backlink = %q, want CUSTOMERS.DBC", dbfTable.Backlink())
	}

	// Wrap the DBF in a BlipperDB area by re-opening it via Use.
	// (BlipperDB.Create writes a fresh DBF; here we want to attach
	// an existing one.)
	dbfFile.pos = 0
	db := New()
	area, err := db.Use("DATA", dbfFile)
	if err != nil {
		t.Fatalf("Use: %v", err)
	}

	// Create the .DBC via the Area's CreateCatalogue.
	dbcFile := &memFile{}
	cat, err := area.CreateCatalogue(dbcFile, "customers")
	if err != nil {
		t.Fatalf("CreateCatalogue: %v", err)
	}
	if _, err := cat.AddField("customer_id"); err != nil {
		t.Fatalf("AddField customer_id: %v", err)
	}
	if _, err := cat.AddField("customer_name"); err != nil {
		t.Fatalf("AddField customer_name: %v", err)
	}

	// Long-name resolution via sugar and via direct AttachedCatalogue.
	if got := area.CatalogueLongName("CUSTOMER_I"); got != "customer_id" {
		t.Errorf("CatalogueLongName(CUSTOMER_I) = %q, want customer_id", got)
	}
	if got := area.CatalogueLongName("CUSTOMER_N"); got != "customer_name" {
		t.Errorf("CatalogueLongName(CUSTOMER_N) = %q, want customer_name", got)
	}
	// Unmatched short name → fallback returns input unchanged.
	if got := area.CatalogueLongName("MISSING"); got != "MISSING" {
		t.Errorf("CatalogueLongName(MISSING) = %q, want MISSING (fallback)", got)
	}

	// Reopen the DBC and re-attach.
	if err := db.CloseArea("DATA"); err != nil {
		t.Fatalf("CloseArea: %v", err)
	}
	dbfFile.pos = 0
	area2, err := db.Use("DATA", dbfFile)
	if err != nil {
		t.Fatalf("Use after close: %v", err)
	}
	dbcFile.pos = 0
	if _, err := area2.AttachCatalogue(dbcFile, "customers"); err != nil {
		t.Fatalf("AttachCatalogue: %v", err)
	}
	if got := area2.CatalogueLongName("CUSTOMER_I"); got != "customer_id" {
		t.Errorf("post-reopen CatalogueLongName(CUSTOMER_I) = %q, want customer_id", got)
	}
}

func TestCatalogueLongNameWithoutAttachIsFallback(t *testing.T) {
	// No catalogue attached → CatalogueLongName returns input.
	db := New()
	area, err := db.Create("PLAIN", &memFile{}, dbf.Schema{Fields: []dbf.Field{
		{Name: "K", Type: dbf.Character, Length: 5},
	}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := area.CatalogueLongName("ANYTHING"); got != "ANYTHING" {
		t.Errorf("CatalogueLongName without attach = %q, want ANYTHING (fallback)", got)
	}
	if area.Catalogue() != nil {
		t.Errorf("Catalogue() with no attach = %v, want nil", area.Catalogue())
	}
}

func TestCatalogueAttachRefusesMissingTable(t *testing.T) {
	// Build a DBC with a different table name, then try to
	// attach it looking for something else.
	dbcFile := &memFile{}
	cat, err := dbc.Create(dbcFile)
	if err != nil {
		t.Fatalf("dbc.Create: %v", err)
	}
	if _, err := cat.AddTable("some_other_table"); err != nil {
		t.Fatalf("AddTable: %v", err)
	}

	db := New()
	area, err := db.Create("DATA", &memFile{}, dbf.Schema{Fields: []dbf.Field{
		{Name: "K", Type: dbf.Character, Length: 5},
	}})
	if err != nil {
		t.Fatalf("db.Create: %v", err)
	}
	dbcFile.pos = 0
	_, err = area.AttachCatalogue(dbcFile, "customers")
	if err == nil {
		t.Fatal("AttachCatalogue with missing table row succeeded; want error")
	}
	if !errors.Is(err, dbc.ErrTableNotFound) {
		t.Errorf("error = %v, want dbc.ErrTableNotFound in the chain", err)
	}
}

func TestCatalogueAttachTwiceRefused(t *testing.T) {
	db := New()
	area, _ := db.Create("DATA", &memFile{}, dbf.Schema{Fields: []dbf.Field{
		{Name: "K", Type: dbf.Character, Length: 5},
	}})
	if _, err := area.CreateCatalogue(&memFile{}, "customers"); err != nil {
		t.Fatalf("first CreateCatalogue: %v", err)
	}
	if _, err := area.CreateCatalogue(&memFile{}, "customers"); err == nil {
		t.Error("second CreateCatalogue: want error, got nil")
	}
	if _, err := area.AttachCatalogue(&memFile{}, "customers"); err == nil {
		t.Error("AttachCatalogue after Create: want error, got nil")
	}
}

// TestDBCOpenRejectsPlainDBF closes the loose end from T-10 phase 1:
// a valid DBF with the wrong schema surfaces ErrNotDBC rather than
// a confusing decode failure.
func TestDBCOpenRejectsPlainDBF(t *testing.T) {
	// Build a plain non-DBC DBF and try to open it via dbc.Open.
	plainFile := &memFile{}
	if _, err := dbf.Create(plainFile, dbf.Schema{Fields: []dbf.Field{
		{Name: "K", Type: dbf.Character, Length: 5},
	}}); err != nil {
		t.Fatalf("dbf.Create: %v", err)
	}
	plainFile.pos = 0
	_, err := dbc.Open(plainFile)
	if err == nil {
		t.Fatal("dbc.Open on plain DBF succeeded; want ErrNotDBC")
	}
	if !errors.Is(err, dbc.ErrNotDBC) {
		t.Errorf("error = %v, want dbc.ErrNotDBC in the chain", err)
	}
}

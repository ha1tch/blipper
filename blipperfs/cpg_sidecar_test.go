package blipperfs

import (
	"testing"

	"github.com/ha1tch/blipper/blipperdb"
	"github.com/ha1tch/blipper/dbf"
)

// TestCpgSidecarResolvesUTF8 is T-27's decisive test, using the
// same pattern found in real GIS data this session (source:
// docs/RESEARCH_NOTES.md's "§5 .cpg sidecars are the real encoding
// channel for GIS data") — header byte 29 = 0x00 (declares
// nothing) alongside a .cpg file naming UTF-8, and text containing
// characters that exist in no single-byte DOS or Windows code
// page at all: Ċ (U+010A) and Ħ (U+0126), as seen in real Malta
// GADM shapefiles. That makes this decisive rather than
// suggestive — a wrong resolution fails loudly (mangled bytes)
// rather than producing plausible-looking wrong text.
func TestCpgSidecarResolvesUTF8(t *testing.T) {
	fs := NewMemFileSet()
	db := blipperdb.New()

	area, err := CreateTable(db, fs, "T", "PLACES", TableSpec{
		Schema: dbf.Schema{Fields: []dbf.Field{
			{Name: "NAME", Type: dbf.Character, Length: 30},
		}},
	})
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	rec := dbf.NewRecord(area.Table().Schema())
	if err := rec.Set(area.Table().Schema(), "NAME", "Ċentrali, Ħal Balzan"); err != nil {
		t.Fatalf("rec.Set: %v", err)
	}
	if _, err := area.Table().Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := area.Table().Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	db.CloseArea("T")

	// The sibling .cpg, written directly the way a real GIS
	// producer would ship it — a bare content declaration, no
	// DBF structure at all.
	cpgHandle, err := fs.Create("PLACES.CPG")
	if err != nil {
		t.Fatalf("create .cpg: %v", err)
	}
	if _, err := cpgHandle.Write([]byte("UTF-8")); err != nil {
		t.Fatalf("write .cpg: %v", err)
	}

	area2, err := Use(db, fs, "T2", "PLACES.DBF")
	if err != nil {
		t.Fatalf("Use: %v", err)
	}

	enc := area2.Table().Encoding()
	if enc.Source != dbf.EncodingSourceCpgSidecar {
		t.Errorf("Encoding().Source = %v, want EncodingSourceCpgSidecar", enc.Source)
	}

	rec2, err := area2.Table().Get(1)
	if err != nil {
		t.Fatalf("Get(1): %v", err)
	}
	got, err := rec2.Get(area2.Table().Schema(), "NAME")
	if err != nil {
		t.Fatalf("rec.Get: %v", err)
	}
	want := "Ċentrali, Ħal Balzan"
	if got != want {
		t.Errorf("NAME = %q, want %q", got, want)
	}
}

// TestNoCpgSidecarFallsBackToIdentity confirms tier 4 still
// applies correctly when no .cpg exists — the resolution should
// not require a .cpg to function, only prefer one when present.
func TestNoCpgSidecarFallsBackToIdentity(t *testing.T) {
	fs := NewMemFileSet()
	db := blipperdb.New()

	_, err := CreateTable(db, fs, "T", "NOCPG", TableSpec{
		Schema: dbf.Schema{Fields: []dbf.Field{
			{Name: "NAME", Type: dbf.Character, Length: 10},
		}},
	})
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	db.CloseArea("T")

	area, err := Use(db, fs, "T2", "NOCPG.DBF")
	if err != nil {
		t.Fatalf("Use: %v", err)
	}
	if got := area.Table().Encoding().Source; got != dbf.EncodingSourceIdentity {
		t.Errorf("Encoding().Source = %v, want EncodingSourceIdentity (no .cpg present)", got)
	}
}

// TestCpgSidecarUnrecognizedReportsError confirms an unparseable
// .cpg value surfaces as an error rather than silently falling
// back — matching the same failure semantics as a malformed CDX
// or DBC sibling, and matching ParseCpgEncoding's own documented
// behaviour (dbf/encoding.go).
func TestCpgSidecarUnrecognizedReportsError(t *testing.T) {
	fs := NewMemFileSet()
	db := blipperdb.New()

	_, err := CreateTable(db, fs, "T", "BADCPG", TableSpec{
		Schema: dbf.Schema{Fields: []dbf.Field{
			{Name: "NAME", Type: dbf.Character, Length: 10},
		}},
	})
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	db.CloseArea("T")

	cpgHandle, err := fs.Create("BADCPG.CPG")
	if err != nil {
		t.Fatalf("create .cpg: %v", err)
	}
	if _, err := cpgHandle.Write([]byte("NOT-A-REAL-ENCODING")); err != nil {
		t.Fatalf("write .cpg: %v", err)
	}

	area, err := Use(db, fs, "T2", "BADCPG.DBF")
	if err == nil {
		t.Error("Use succeeded with an unrecognized .cpg value, want an error")
	}
	// Matching the existing sibling-failure contract: the area is
	// still usable even though resolution reported a problem.
	if area == nil {
		t.Error("Use returned a nil area alongside the error, want a usable one per the existing sibling-failure contract")
	}
}

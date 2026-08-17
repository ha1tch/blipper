package blipperfs

import (
	"fmt"

	"github.com/ha1tch/blipper/blipperdb"
	"github.com/ha1tch/blipper/cdx"
	"github.com/ha1tch/blipper/dbf"
)

// TableSpec describes a table to create, including every sibling
// file it should carry. Zero values mean "no such sibling":
// MemoFormatNone writes no memo file, an empty LongNames map
// writes no catalogue, an empty Tags slice writes no CDX.
type TableSpec struct {
	// Schema is the DBF field layout. Required.
	Schema dbf.Schema

	// MemoFormat selects the memo sibling: MemoFormatNone (no
	// memo file), MemoFormatDBT (stem.DBT), or MemoFormatFPT
	// (stem.FPT). Must be consistent with Schema: a schema with
	// a Memo field and MemoFormatNone is rejected, as is a memo
	// format with no Memo field in the schema.
	MemoFormat dbf.MemoFormat

	// FPTBlockSize sets the block size for FPT memo files.
	// Ignored for DBT (fixed at 512). Zero uses the package
	// default (64, matching Clipper's DBFCDX).
	FPTBlockSize uint16

	// LongNames maps DBF short field names to their catalogued
	// long forms. When non-empty, CreateTable writes a .DBC
	// sibling, sets the table's DBC-owned and blipper-provenance
	// flags, and writes the backlink. Keys must name fields
	// present in Schema.
	LongNames map[string]string

	// TableLongName is the long name registered for the table
	// itself in the catalogue. Defaults to the stem when empty.
	// Only meaningful when LongNames is non-empty.
	TableLongName string

	// Tags describes CDX index tags to build. When non-empty,
	// CreateTable writes a stem.CDX sibling. Entries within each
	// TagSpec must already be sorted; CreateTable does not sort
	// them (matching cdx.Build's contract).
	Tags []cdx.TagSpec
}

// CreateTable writes a complete file-set for one table and
// registers it in db under the given alias.
//
// Depending on the spec this writes between one and four files:
//
//	stem.DBF   always
//	stem.DBT   when MemoFormat is MemoFormatDBT
//	stem.FPT   when MemoFormat is MemoFormatFPT
//	stem.DBC   when LongNames is non-empty
//	stem.CDX   when Tags is non-empty
//
// The DBF's version byte and table flags are set to match, and
// the DBC backlink is written when a catalogue is present, so
// that a later Use resolves every sibling automatically.
func CreateTable(db *blipperdb.BlipperDB, fs FileSet, alias, stem string, spec TableSpec) (*blipperdb.Area, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}

	dbfName := stem + ".DBF"
	dbfHandle, err := fs.Create(dbfName)
	if err != nil {
		return nil, fmt.Errorf("blipperfs: create %s: %w", dbfName, err)
	}

	// The DBF is written with a backlink when a catalogue is
	// requested; otherwise plainly.
	wantCatalogue := len(spec.LongNames) > 0
	dbcName := stem + ".DBC"
	if wantCatalogue {
		if _, err := dbf.CreateWithBacklink(dbfHandle, spec.Schema, dbcName); err != nil {
			return nil, fmt.Errorf("blipperfs: create %s with backlink: %w", dbfName, err)
		}
	} else {
		if _, err := dbf.Create(dbfHandle, spec.Schema); err != nil {
			return nil, fmt.Errorf("blipperfs: create %s: %w", dbfName, err)
		}
	}

	// A DBT-flavour table is what dbf.Create writes for a memo
	// schema. An FPT-flavour table needs the version byte
	// promoted, which means rewriting byte 0 before reopening.
	if spec.MemoFormat == dbf.MemoFormatFPT {
		if err := promoteToFPT(dbfHandle); err != nil {
			return nil, err
		}
	}

	// Reopen through blipperdb so the Area sees the final header.
	reopened, err := fs.Open(dbfName)
	if err != nil {
		return nil, fmt.Errorf("blipperfs: reopen %s: %w", dbfName, err)
	}
	area, err := db.Use(alias, reopened)
	if err != nil {
		return nil, fmt.Errorf("blipperfs: use %s: %w", dbfName, err)
	}

	// Memo sibling.
	switch spec.MemoFormat {
	case dbf.MemoFormatDBT:
		h, err := fs.Create(stem + ".DBT")
		if err != nil {
			return nil, fmt.Errorf("blipperfs: create %s.DBT: %w", stem, err)
		}
		if _, err := area.CreateMemo(h, 0); err != nil {
			return nil, fmt.Errorf("blipperfs: create memo: %w", err)
		}
	case dbf.MemoFormatFPT:
		h, err := fs.Create(stem + ".FPT")
		if err != nil {
			return nil, fmt.Errorf("blipperfs: create %s.FPT: %w", stem, err)
		}
		if _, err := area.CreateMemo(h, spec.FPTBlockSize); err != nil {
			return nil, fmt.Errorf("blipperfs: create memo: %w", err)
		}
	}

	// Catalogue sibling.
	if wantCatalogue {
		tableLongName := spec.TableLongName
		if tableLongName == "" {
			tableLongName = stem
		}
		h, err := fs.Create(dbcName)
		if err != nil {
			return nil, fmt.Errorf("blipperfs: create %s: %w", dbcName, err)
		}
		cat, err := area.CreateCatalogue(h, tableLongName)
		if err != nil {
			return nil, fmt.Errorf("blipperfs: create catalogue: %w", err)
		}
		// Register long names in schema order so the catalogue's
		// row order matches the DBF's field order — deterministic
		// output rather than map-iteration order.
		for _, f := range spec.Schema.Fields {
			long, ok := spec.LongNames[f.Name]
			if !ok {
				continue
			}
			if _, err := cat.AddField(long); err != nil {
				return nil, fmt.Errorf("blipperfs: register long name %q: %w", long, err)
			}
		}
	}

	// CDX sibling.
	if len(spec.Tags) > 0 {
		h, err := fs.Create(stem + ".CDX")
		if err != nil {
			return nil, fmt.Errorf("blipperfs: create %s.CDX: %w", stem, err)
		}
		if err := cdx.Build(h, spec.Tags); err != nil {
			return nil, fmt.Errorf("blipperfs: build CDX: %w", err)
		}
		// Reopen for reading and attach.
		rh, err := fs.Open(stem + ".CDX")
		if err != nil {
			return nil, fmt.Errorf("blipperfs: reopen %s.CDX: %w", stem, err)
		}
		if _, err := area.AttachCDX(rh); err != nil {
			return nil, fmt.Errorf("blipperfs: attach CDX: %w", err)
		}
	}

	return area, nil
}

// validateSpec catches inconsistencies before any file is written,
// so a rejected spec leaves no partial file-set behind.
func validateSpec(spec TableSpec) error {
	if len(spec.Schema.Fields) == 0 {
		return fmt.Errorf("blipperfs: spec has no fields")
	}
	hasMemoField := spec.Schema.HasMemo()
	switch spec.MemoFormat {
	case dbf.MemoFormatNone:
		if hasMemoField {
			return fmt.Errorf("blipperfs: schema has a Memo field but MemoFormat is None")
		}
	case dbf.MemoFormatDBT, dbf.MemoFormatFPT:
		if !hasMemoField {
			return fmt.Errorf("blipperfs: MemoFormat is %s but schema has no Memo field", spec.MemoFormat)
		}
	default:
		return fmt.Errorf("blipperfs: unknown MemoFormat %v", spec.MemoFormat)
	}
	for short := range spec.LongNames {
		found := false
		for _, f := range spec.Schema.Fields {
			if f.Name == short {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("blipperfs: LongNames key %q names no field in the schema", short)
		}
	}
	return nil
}

// promoteToFPT rewrites byte 0 of a freshly created memo-bearing
// DBF from the DBT-flavour 0x83 to the FPT-flavour 0xF5.
//
// dbf.Create picks 0x83 for any memo schema, which is the right
// default for the stream API. blipperfs knows which memo format
// the caller asked for, so it corrects the byte here rather than
// widening dbf.Create's signature.
func promoteToFPT(h interface {
	Seek(int64, int) (int64, error)
	Write([]byte) (int, error)
}) error {
	if _, err := h.Seek(0, 0); err != nil {
		return fmt.Errorf("blipperfs: seek for FPT promotion: %w", err)
	}
	if _, err := h.Write([]byte{0xF5}); err != nil {
		return fmt.Errorf("blipperfs: write FPT version byte: %w", err)
	}
	return nil
}

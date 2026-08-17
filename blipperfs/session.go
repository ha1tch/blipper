package blipperfs

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ha1tch/blipper/blipperdb"
	"github.com/ha1tch/blipper/dbc"
	"github.com/ha1tch/blipper/dbf"
)

// ErrMissingSibling is returned when a table declares a sibling
// file that the FileSet does not contain. Per T-10's truth table,
// a declared-but-absent sibling is corruption, not an ordinary
// "no index attached" condition.
var ErrMissingSibling = errors.New("blipperfs: declared sibling file is missing")

// Use opens the named table from fs and registers it in db under
// the given alias, resolving every sibling the table declares.
//
// Resolution follows FoxPro's USE semantics:
//
//   - Memo file: the version byte says DBT (0x83) or FPT (0xF5).
//     The sibling is stem.DBT or stem.FPT accordingly. If the
//     table declares a memo format and the file is absent, Use
//     returns ErrMissingSibling.
//   - DBC catalogue: table-flags bit 2 says the table is
//     DBC-owned; the backlink says where. If declared and absent,
//     ErrMissingSibling.
//   - Structural CDX: conventional stem.CDX. Attached when
//     present, silently skipped when absent — nothing in the DBF
//     declares it, so absence is not an error.
//
// Free NTX indexes are never attached automatically: nothing in
// the DBF names them, and globbing *.NTX would attach indexes the
// caller never asked for. Use blipperdb.Area.SetIndex explicitly.
func Use(db *blipperdb.BlipperDB, fs FileSet, alias, name string) (*blipperdb.Area, error) {
	handle, err := fs.Open(name)
	if err != nil {
		return nil, fmt.Errorf("blipperfs: open %s: %w", name, err)
	}
	area, err := db.Use(alias, handle)
	if err != nil {
		return nil, fmt.Errorf("blipperfs: use %s: %w", name, err)
	}
	if err := resolveSiblings(area, fs, stemOf(name)); err != nil {
		// Leave the area registered; the caller may still want the
		// table even though a sibling failed. Report the failure.
		return area, err
	}
	return area, nil
}

// resolveSiblings attaches every sibling the table declares.
func resolveSiblings(area *blipperdb.Area, fs FileSet, stem string) error {
	table := area.Table()

	// Memo file, declared by the version byte.
	switch table.MemoFormat() {
	case dbf.MemoFormatDBT:
		if err := attachMemo(area, fs, stem+".DBT"); err != nil {
			return err
		}
	case dbf.MemoFormatFPT:
		if err := attachMemo(area, fs, stem+".FPT"); err != nil {
			return err
		}
	}

	// DBC catalogue, declared by table-flags bit 2 plus backlink.
	if table.TableFlags()&dbcOwnedBit != 0 {
		backlink := table.Backlink()
		if backlink == "" {
			return fmt.Errorf("%w: table is DBC-owned but backlink is empty", ErrMissingSibling)
		}
		if !fs.Exists(backlink) {
			return fmt.Errorf("%w: %s (backlink from %s)", ErrMissingSibling, backlink, stem)
		}
		handle, err := fs.Open(backlink)
		if err != nil {
			return fmt.Errorf("blipperfs: open catalogue %s: %w", backlink, err)
		}
		// Which DBC row describes this table cannot be derived
		// from the filename: CreateTable lets the caller pick a
		// TableLongName independent of the stem, and a
		// hand-authored DBC may use any name at all. Resolve by
		// looking at what the catalogue actually contains.
		longName, err := resolveTableLongName(handle, stem)
		if err != nil {
			return fmt.Errorf("blipperfs: resolve table in %s: %w", backlink, err)
		}
		if _, err := area.AttachCatalogue(handle, longName); err != nil {
			return fmt.Errorf("blipperfs: attach catalogue %s: %w", backlink, err)
		}
	}

	// Structural CDX, conventional rather than declared: attach
	// when present, skip silently when absent.
	cdxName := stem + ".CDX"
	if fs.Exists(cdxName) {
		handle, err := fs.Open(cdxName)
		if err != nil {
			return fmt.Errorf("blipperfs: open %s: %w", cdxName, err)
		}
		if _, err := area.AttachCDX(handle); err != nil {
			return fmt.Errorf("blipperfs: attach %s: %w", cdxName, err)
		}
	}

	// Encoding sidecar (T-27), conventional like CDX: attach when
	// present, skip silently when absent. This is tier 2 of the
	// four-way resolution — explicit override, .cpg sidecar,
	// header byte 29, identity — and overrides whatever Open
	// already established from byte 29, since a .cpg's presence
	// means the file's own byte 29 declaration (commonly absent
	// or wrong for GIS-produced DBFs) should not be trusted over
	// it. A caller calling SetCodePage/SetEncoding afterward still
	// wins, being later and explicit.
	cpgName := stem + ".CPG"
	if fs.Exists(cpgName) {
		handle, err := fs.Open(cpgName)
		if err != nil {
			return fmt.Errorf("blipperfs: open %s: %w", cpgName, err)
		}
		content, err := io.ReadAll(handle)
		if err != nil {
			return fmt.Errorf("blipperfs: read %s: %w", cpgName, err)
		}
		enc, err := dbf.ParseCpgEncoding(content)
		if err != nil {
			return fmt.Errorf("blipperfs: %s: %w", cpgName, err)
		}
		table.SetEncoding(enc)
	}

	return nil
}

// attachMemo opens and attaches a declared memo sibling, treating
// absence as corruption.
func attachMemo(area *blipperdb.Area, fs FileSet, name string) error {
	if !fs.Exists(name) {
		return fmt.Errorf("%w: %s (declared by version byte)", ErrMissingSibling, name)
	}
	handle, err := fs.Open(name)
	if err != nil {
		return fmt.Errorf("blipperfs: open %s: %w", name, err)
	}
	if _, err := area.AttachMemo(handle); err != nil {
		return fmt.Errorf("blipperfs: attach %s: %w", name, err)
	}
	return nil
}

// dbcOwnedBit mirrors dbf's internal table-flags bit 2. Kept here
// as a named constant so blipperfs does not export blipper's
// truth table to callers, and does not depend on an unexported
// constant in dbf.
const dbcOwnedBit = 0x04

// stemOf returns a filename without its extension.
func stemOf(name string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext)
}

// extOf returns a filename's extension, including the dot.
func extOf(name string) string { return filepath.Ext(name) }

// resolveTableLongName decides which catalogue row describes the
// table being opened.
//
// The mapping from a .DBF to its row in a .DBC is not recoverable
// from the filename: CreateTable lets the caller choose a
// TableLongName independent of the stem, and a hand-authored DBC
// may use any naming at all. Three cases, in order:
//
//  1. A row whose long name matches the stem case-insensitively —
//     the obvious intent when the names line up.
//  2. Exactly one table row in the catalogue — a single-table DBC
//     leaves no ambiguity, whatever the row is called.
//  3. Several rows and no stem match — genuinely ambiguous, so
//     report it rather than guessing. The caller can attach
//     explicitly via Area.AttachCatalogue with the right name.
//
// The handle is rewound before returning so the caller can reuse it.
func resolveTableLongName(handle io.ReadWriteSeeker, stem string) (string, error) {
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	cat, err := dbc.Open(handle)
	if err != nil {
		return "", err
	}
	names := cat.TableNames()
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	for _, n := range names {
		if strings.EqualFold(n, stem) {
			return n, nil
		}
	}
	if len(names) == 1 {
		return names[0], nil
	}
	if len(names) == 0 {
		return "", fmt.Errorf("catalogue contains no table rows")
	}
	return "", fmt.Errorf("catalogue contains %d tables (%v) and none matches stem %q; attach explicitly", len(names), names, stem)
}

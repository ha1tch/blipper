// Package blipperdb: DBC catalogue attachment.
//
// This file adds Area-level attach/create/lookup support for
// .DBC (VFP-format long-name catalogue) sidecars. Same shape as
// blipperdb/cdx_area.go (AttachedCDX) and blipperdb/memo_area.go
// (AttachedMemo): the raw dbc package handles the format, and
// the Area holds one attachment plus sugar for the common
// lookup pattern.
//
// A DBC pairs to a DBF by long table name, resolved on attach.
// The Area caches the resulting ObjectID so subsequent
// long-name resolutions are single linear scans in-memory
// rather than round-trips through the DBC's tree.
package blipperdb

import (
	"fmt"
	"io"

	"github.com/ha1tch/blipper/dbc"
)

// AttachedCatalogue is a DBC attached to an Area, paired with a
// specific table by long name. FieldLongName and AddField
// dispatch through the cached ObjectID.
type AttachedCatalogue struct {
	cat     *dbc.Catalogue
	tableID dbc.ObjectID

	// src is retained so Area.close can release the handle.
	src io.ReadWriteSeeker
}

// Catalogue returns the underlying dbc.Catalogue. Useful for
// callers who need direct access — for example to enumerate
// every table in a multi-table DBC — but ordinary use goes
// through the Area methods.
func (a *AttachedCatalogue) Catalogue() *dbc.Catalogue { return a.cat }

// TableID returns the ObjectID of the DBC row for this Area's
// table. Cached at attach time.
func (a *AttachedCatalogue) TableID() dbc.ObjectID { return a.tableID }

// FieldLongName resolves a DBF-side short field name (≤ 10 chars,
// as it appears in the field descriptor) to the long name stored
// in the catalogue. Returns the input unchanged when no long-name
// entry matches — the same dBASE III+ fallback that
// dbc.Catalogue.FieldLongName uses.
func (a *AttachedCatalogue) FieldLongName(shortName string) string {
	return a.cat.FieldLongName(a.tableID, shortName)
}

// AddField registers a long field name for this Area's table.
// The corresponding DBF field descriptor should carry the ≤10-char
// truncation as its short name. Returns the DBC OBJECTID of the
// new Field row for callers who want it; most callers ignore it.
func (a *AttachedCatalogue) AddField(longName string) (dbc.ObjectID, error) {
	return a.cat.AddField(a.tableID, longName)
}

// AttachCatalogue opens a DBC from rw and pairs this Area's table
// with the DBC row whose OBJECTNAME matches tableLongName. Errors
// if the DBC cannot be opened, if the named table row is missing,
// or if a catalogue is already attached.
//
// The reader stays owned by the caller.
func (a *Area) AttachCatalogue(rw io.ReadWriteSeeker, tableLongName string) (*AttachedCatalogue, error) {
	if a.catalogue != nil {
		return nil, fmt.Errorf("blipperdb: catalogue already attached")
	}
	cat, err := dbc.Open(rw)
	if err != nil {
		return nil, fmt.Errorf("blipperdb: open .DBC: %w", err)
	}
	tid, err := cat.TableID(tableLongName)
	if err != nil {
		return nil, fmt.Errorf("blipperdb: resolve table %q: %w", tableLongName, err)
	}
	a.catalogue = &AttachedCatalogue{cat: cat, tableID: tid, src: rw}
	return a.catalogue, nil
}

// CreateCatalogue creates a fresh .DBC at rw, adds a Table row
// for tableLongName, and attaches the catalogue. Errors if a
// catalogue is already attached.
func (a *Area) CreateCatalogue(rw io.ReadWriteSeeker, tableLongName string) (*AttachedCatalogue, error) {
	if a.catalogue != nil {
		return nil, fmt.Errorf("blipperdb: catalogue already attached")
	}
	cat, err := dbc.Create(rw)
	if err != nil {
		return nil, fmt.Errorf("blipperdb: create .DBC: %w", err)
	}
	tid, err := cat.AddTable(tableLongName)
	if err != nil {
		return nil, fmt.Errorf("blipperdb: add table row %q: %w", tableLongName, err)
	}
	a.catalogue = &AttachedCatalogue{cat: cat, tableID: tid, src: rw}
	return a.catalogue, nil
}

// Catalogue returns the attached catalogue, or nil if none.
func (a *Area) Catalogue() *AttachedCatalogue { return a.catalogue }

// CatalogueLongName is sugar for a.Catalogue().FieldLongName(shortName)
// with a friendly fallback when no catalogue is attached. Returns
// the input unchanged if no catalogue is attached or if the short
// name has no long-name entry — the invariant is that this method
// always returns some name (never fails), matching how a
// legacy dBASE III+ reader that ignores the catalogue would
// behave.
func (a *Area) CatalogueLongName(shortName string) string {
	if a.catalogue == nil {
		return shortName
	}
	return a.catalogue.FieldLongName(shortName)
}

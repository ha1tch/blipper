// Package blipperdb provides the session layer for a Clipper-like
// runtime: a BlipperDB object pools open tables and their indexes in
// named work areas, so a language session can USE A, USE B, USE C
// and switch between them.
//
// The conventional import alias is bdb:
//
//	import bdb "github.com/ha1tch/blipper/blipperdb"
//
// Ownership: unlike the dbf and ntx packages, which never close what
// they are given, a BlipperDB owns the readers and writers handed to
// Use, Create, SetIndex and CreateIndex. If they implement io.Closer,
// closing an area closes them.
package blipperdb

import (
	"fmt"
	"io"
	"strings"

	"github.com/ha1tch/blipper/dbf"
)

// BlipperDB pools open tables and indexes in named work areas.
//
// A BlipperDB is not safe for concurrent use.
type BlipperDB struct {
	areas   map[string]*Area
	order   []string
	current string
}

// New returns an empty BlipperDB with no open areas.
func New() *BlipperDB {
	return &BlipperDB{
		areas: make(map[string]*Area),
	}
}

// Use opens the DBF table in rw under the given alias and selects
// that area, mirroring Clipper's USE.
//
// If the alias is already in use, its area is closed first and
// replaced.
func (db *BlipperDB) Use(alias string, rw io.ReadWriteSeeker) (*Area, error) {
	name, err := normalizeAlias(alias)
	if err != nil {
		return nil, err
	}

	table, err := dbf.Open(rw)
	if err != nil {
		return nil, fmt.Errorf("USE %s: %w", name, err)
	}

	return db.install(name, table, rw)
}

// Create writes a new, empty DBF table to rw, opens it under the
// given alias and selects that area.
func (db *BlipperDB) Create(
	alias string,
	rw io.ReadWriteSeeker,
	schema dbf.Schema,
) (*Area, error) {
	name, err := normalizeAlias(alias)
	if err != nil {
		return nil, err
	}

	table, err := dbf.Create(rw, schema)
	if err != nil {
		return nil, fmt.Errorf("CREATE %s: %w", name, err)
	}

	return db.install(name, table, rw)
}

// Select makes the named area current and returns it.
func (db *BlipperDB) Select(alias string) (*Area, error) {
	name, err := normalizeAlias(alias)
	if err != nil {
		return nil, err
	}

	area, ok := db.areas[name]
	if !ok {
		return nil, fmt.Errorf("no area %q", name)
	}

	db.current = name

	return area, nil
}

// Current returns the selected area, or nil when no area is open.
func (db *BlipperDB) Current() *Area {
	if db.current == "" {
		return nil
	}

	return db.areas[db.current]
}

// Area returns the named area without selecting it.
func (db *BlipperDB) Area(alias string) (*Area, error) {
	name, err := normalizeAlias(alias)
	if err != nil {
		return nil, err
	}

	area, ok := db.areas[name]
	if !ok {
		return nil, fmt.Errorf("no area %q", name)
	}

	return area, nil
}

// Aliases returns the open aliases in the order their areas were
// opened.
func (db *BlipperDB) Aliases() []string {
	return append([]string(nil), db.order...)
}

// CloseArea closes the named area, its table source and all its
// index sources.
func (db *BlipperDB) CloseArea(alias string) error {
	name, err := normalizeAlias(alias)
	if err != nil {
		return err
	}

	area, ok := db.areas[name]
	if !ok {
		return fmt.Errorf("no area %q", name)
	}

	err = area.close()

	delete(db.areas, name)

	for i, n := range db.order {
		if n == name {
			db.order = append(db.order[:i], db.order[i+1:]...)
			break
		}
	}

	if db.current == name {
		db.current = ""
	}

	return err
}

// CloseAll closes every open area, mirroring Clipper's CLOSE ALL.
//
// All areas are closed even if some fail; the first error is
// returned.
func (db *BlipperDB) CloseAll() error {
	var first error

	for _, name := range append([]string(nil), db.order...) {
		if err := db.CloseArea(name); err != nil && first == nil {
			first = err
		}
	}

	return first
}

// install replaces any existing area under name, registers the new
// one and selects it.
func (db *BlipperDB) install(
	name string,
	table *dbf.Table,
	src io.ReadWriteSeeker,
) (*Area, error) {
	if _, exists := db.areas[name]; exists {
		if err := db.CloseArea(name); err != nil {
			return nil, fmt.Errorf(
				"closing previous area %q: %w",
				name,
				err,
			)
		}
	}

	area := &Area{
		alias: name,
		table: table,
		src:   src,
	}

	if err := area.GoTop(); err != nil {
		return nil, err
	}

	db.areas[name] = area
	db.order = append(db.order, name)
	db.current = name

	return area, nil
}

func normalizeAlias(alias string) (string, error) {
	name := strings.ToUpper(strings.TrimSpace(alias))

	if name == "" {
		return "", fmt.Errorf("empty alias")
	}

	return name, nil
}

func closeIfCloser(v any) error {
	if c, ok := v.(io.Closer); ok {
		return c.Close()
	}

	return nil
}

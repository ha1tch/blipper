package dbc

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ha1tch/blipper/dbf"
)

// Errors returned by Catalogue operations.
var (
	// ErrNotDBC is returned when Open cannot recognise the
	// underlying file as a valid DBC (missing required columns,
	// no Database row, etc.).
	ErrNotDBC = errors.New("dbc: not a valid DBC (schema mismatch)")

	// ErrTableExists is returned by AddTable when a table with
	// the same long name is already present.
	ErrTableExists = errors.New("dbc: table with that long name already exists")

	// ErrTableNotFound is returned by long-name lookups when
	// the requested table is not registered.
	ErrTableNotFound = errors.New("dbc: table not found")

	// ErrFieldExists is returned by AddField when a field with
	// the same long name is already registered under the same
	// table.
	ErrFieldExists = errors.New("dbc: field with that long name already exists in table")
)

// Catalogue is an open .DBC file. It caches every row in memory
// on Open, which is trivial at expected DBC sizes (a few dozen to
// a few thousand rows), and lets lookups be linear scans without
// re-reading the DBF each time.
type Catalogue struct {
	table  *dbf.Table
	rows   []row
	nextID ObjectID
}

// Create writes an empty .DBC to rw and returns a handle. The
// only row present after Create is the Database row (OBJECTID
// RootID, no PARENTID).
func Create(rw io.ReadWriteSeeker) (*Catalogue, error) {
	schema := catalogueSchema()
	table, err := dbf.Create(rw, schema)
	if err != nil {
		return nil, fmt.Errorf("dbc: create underlying DBF: %w", err)
	}
	cat := &Catalogue{table: table, nextID: RootID + 1}
	if err := cat.appendRow(row{
		ObjectID:   RootID,
		ParentID:   0,
		ObjectType: ObjectTypeDatabase,
		ObjectName: "",
	}); err != nil {
		return nil, err
	}
	return cat, nil
}

// Open reads an existing .DBC from rw. Verifies the schema
// matches the 8 canonical columns and that the tree is
// well-formed (exactly one Database row, every non-root PARENTID
// resolves to a known row).
func Open(rw io.ReadWriteSeeker) (*Catalogue, error) {
	table, err := dbf.Open(rw)
	if err != nil {
		return nil, fmt.Errorf("dbc: open underlying DBF: %w", err)
	}
	if err := checkSchema(table.Schema()); err != nil {
		return nil, err
	}
	cat := &Catalogue{table: table, nextID: RootID}
	// Load every row into memory.
	for i := uint32(1); i <= table.RecordCount(); i++ {
		rec, err := table.Get(i)
		if err != nil {
			return nil, fmt.Errorf("dbc: read record %d: %w", i, err)
		}
		r, err := decodeRow(table.Schema(), rec)
		if err != nil {
			return nil, fmt.Errorf("dbc: decode row %d: %w", i, err)
		}
		cat.rows = append(cat.rows, r)
		if r.ObjectID >= cat.nextID {
			cat.nextID = r.ObjectID + 1
		}
	}
	if err := cat.validateTree(); err != nil {
		return nil, err
	}
	return cat, nil
}

// AddTable registers a table by its long name and returns the
// new row's OBJECTID. Long name is truncated to MaxLongName if
// longer.
func (c *Catalogue) AddTable(longName string) (ObjectID, error) {
	longName = truncateName(longName)
	if longName == "" {
		return 0, fmt.Errorf("dbc: table long name must not be empty")
	}
	for _, r := range c.rows {
		if r.ObjectType == ObjectTypeTable && r.ObjectName == longName {
			return 0, ErrTableExists
		}
	}
	id := c.nextID
	c.nextID++
	if err := c.appendRow(row{
		ObjectID:   id,
		ParentID:   RootID,
		ObjectType: ObjectTypeTable,
		ObjectName: longName,
	}); err != nil {
		return 0, err
	}
	return id, nil
}

// AddField registers a field's long name under the named parent
// table. The short name that would appear in the DBF field
// descriptor is derived at read time by truncating longName to
// 10 characters (dBASE III+'s field-name limit).
func (c *Catalogue) AddField(tableID ObjectID, longName string) (ObjectID, error) {
	longName = truncateName(longName)
	if longName == "" {
		return 0, fmt.Errorf("dbc: field long name must not be empty")
	}
	// Verify the parent exists and is a Table row.
	var parent *row
	for i := range c.rows {
		if c.rows[i].ObjectID == tableID && c.rows[i].ObjectType == ObjectTypeTable {
			parent = &c.rows[i]
			break
		}
	}
	if parent == nil {
		return 0, ErrTableNotFound
	}
	for _, r := range c.rows {
		if r.ParentID == tableID && r.ObjectType == ObjectTypeField && r.ObjectName == longName {
			return 0, ErrFieldExists
		}
	}
	id := c.nextID
	c.nextID++
	if err := c.appendRow(row{
		ObjectID:   id,
		ParentID:   tableID,
		ObjectType: ObjectTypeField,
		ObjectName: longName,
	}); err != nil {
		return 0, err
	}
	return id, nil
}

// TableID returns the OBJECTID of the table with the given long
// name, or ErrTableNotFound.
func (c *Catalogue) TableID(longName string) (ObjectID, error) {
	longName = strings.TrimSpace(longName)
	for _, r := range c.rows {
		if r.ObjectType == ObjectTypeTable && r.ObjectName == longName {
			return r.ObjectID, nil
		}
	}
	return 0, ErrTableNotFound
}

// TableNames returns the long names of every registered table
// in registration order.
func (c *Catalogue) TableNames() []string {
	out := make([]string, 0)
	for _, r := range c.rows {
		if r.ObjectType == ObjectTypeTable {
			out = append(out, r.ObjectName)
		}
	}
	return out
}

// FieldLongName returns the long name of the field whose short
// (≤10-char) form matches the input, within the specified table.
// Returns the input unchanged when no long-name entry matches —
// callers can treat that as "no promotion, use the short name
// directly", which is what dBASE III+ readers do anyway.
//
// The short-name derivation is a case-insensitive comparison of
// the first ten characters of each Field row's OBJECTNAME. If
// two long names truncate to the same short form, the first
// registered wins (matches Clipper's linear-scan resolution
// documented in T-07).
func (c *Catalogue) FieldLongName(tableID ObjectID, shortName string) string {
	short := strings.ToUpper(strings.TrimSpace(shortName))
	for _, r := range c.rows {
		if r.ParentID == tableID && r.ObjectType == ObjectTypeField {
			candidateShort := shortenName(r.ObjectName)
			if strings.ToUpper(candidateShort) == short {
				return r.ObjectName
			}
		}
	}
	return shortName
}

// FieldNames returns the long names of every field registered
// under the given table, in registration order.
func (c *Catalogue) FieldNames(tableID ObjectID) []string {
	out := make([]string, 0)
	for _, r := range c.rows {
		if r.ParentID == tableID && r.ObjectType == ObjectTypeField {
			out = append(out, r.ObjectName)
		}
	}
	return out
}

// Table returns the underlying dbf.Table. Useful for callers that
// need direct access — for example to write the last-updated
// stamp — but ordinary use should go through Catalogue methods.
func (c *Catalogue) Table() *dbf.Table { return c.table }

// --- internal ---

// appendRow writes r to the underlying DBF and updates the
// in-memory cache.
func (c *Catalogue) appendRow(r row) error {
	rec := dbf.NewRecord(c.table.Schema())
	rec.Set(c.table.Schema(), FieldObjectID, int64(r.ObjectID))
	rec.Set(c.table.Schema(), FieldParentID, int64(r.ParentID))
	rec.Set(c.table.Schema(), FieldObjectType, padTo(r.ObjectType, 10))
	rec.Set(c.table.Schema(), FieldObjectName, padTo(r.ObjectName, MaxLongName))
	// Memo columns (PROPERTY, CODE, USER) stay at all-spaces
	// (absent memo pointer) — see package doc.
	rec.Set(c.table.Schema(), FieldProperty, "          ")
	rec.Set(c.table.Schema(), FieldCode, "          ")
	rec.Set(c.table.Schema(), FieldRIInfo, "      ")
	rec.Set(c.table.Schema(), FieldUser, "          ")
	if _, err := c.table.Append(rec); err != nil {
		return fmt.Errorf("dbc: append row: %w", err)
	}
	c.rows = append(c.rows, r)
	return nil
}

// validateTree checks that Open loaded a well-formed catalogue:
// exactly one Database row at RootID, every other row has a
// PARENTID pointing at a Table or Database row that exists.
func (c *Catalogue) validateTree() error {
	seenIDs := map[ObjectID]string{}
	databaseCount := 0
	for _, r := range c.rows {
		if _, exists := seenIDs[r.ObjectID]; exists {
			return fmt.Errorf("dbc: duplicate OBJECTID %d", r.ObjectID)
		}
		seenIDs[r.ObjectID] = r.ObjectType
		if r.ObjectType == ObjectTypeDatabase {
			databaseCount++
		}
	}
	if databaseCount != 1 {
		return fmt.Errorf("dbc: expected exactly 1 Database row, found %d", databaseCount)
	}
	for _, r := range c.rows {
		if r.ObjectType == ObjectTypeDatabase {
			continue
		}
		parentType, ok := seenIDs[r.ParentID]
		if !ok {
			return fmt.Errorf("dbc: row %d has dangling PARENTID %d", r.ObjectID, r.ParentID)
		}
		if r.ObjectType == ObjectTypeTable && parentType != ObjectTypeDatabase {
			return fmt.Errorf("dbc: Table row %d parent %d has type %s, want Database", r.ObjectID, r.ParentID, parentType)
		}
		if r.ObjectType == ObjectTypeField && parentType != ObjectTypeTable {
			return fmt.Errorf("dbc: Field row %d parent %d has type %s, want Table", r.ObjectID, r.ParentID, parentType)
		}
	}
	return nil
}

// catalogueSchema returns the 8-column DBF schema used by every
// .DBC file. Column names, types, and lengths match VFP's own.
func catalogueSchema() dbf.Schema {
	return dbf.Schema{Fields: []dbf.Field{
		{Name: FieldObjectID, Type: dbf.Numeric, Length: 10, Decimals: 0},
		{Name: FieldParentID, Type: dbf.Numeric, Length: 10, Decimals: 0},
		{Name: FieldObjectType, Type: dbf.Character, Length: 10},
		{Name: FieldObjectName, Type: dbf.Character, Length: MaxLongName},
		{Name: FieldProperty, Type: dbf.Memo, Length: 10},
		{Name: FieldCode, Type: dbf.Memo, Length: 10},
		{Name: FieldRIInfo, Type: dbf.Character, Length: 6},
		{Name: FieldUser, Type: dbf.Memo, Length: 10},
	}}
}

// checkSchema verifies that the DBF opened as a Catalogue has
// exactly the columns we expect. VFP-only columns (Timestamp,
// Class, Reserved) that some real DBC files carry are unlikely
// to appear in blipper-written files and are treated as errors
// here — we do not want to silently misinterpret an unfamiliar
// row.
func checkSchema(s dbf.Schema) error {
	want := catalogueSchema().Fields
	if len(s.Fields) != len(want) {
		return fmt.Errorf("%w: got %d columns, want %d", ErrNotDBC, len(s.Fields), len(want))
	}
	for i, w := range want {
		got := s.Fields[i]
		if !strings.EqualFold(got.Name, w.Name) {
			return fmt.Errorf("%w: column %d name %q, want %q", ErrNotDBC, i, got.Name, w.Name)
		}
		if got.Type != w.Type {
			return fmt.Errorf("%w: column %d %q type %c, want %c", ErrNotDBC, i, got.Name, got.Type, w.Type)
		}
	}
	return nil
}

// decodeRow reads one Catalogue row from a DBF record.
func decodeRow(s dbf.Schema, rec dbf.Record) (row, error) {
	getInt := func(name string) (ObjectID, error) {
		raw, err := rec.Get(s, name)
		if err != nil {
			return 0, err
		}
		switch v := raw.(type) {
		case int64:
			return ObjectID(v), nil
		case float64:
			return ObjectID(v), nil
		case string:
			// Numeric N-fields decode as strings when a
			// higher-level decoder isn't in play; parse.
			return 0, fmt.Errorf("unexpected string for numeric column %s: %q", name, v)
		default:
			return 0, fmt.Errorf("unexpected type %T for numeric column %s", raw, name)
		}
	}
	getStr := func(name string) (string, error) {
		raw, err := rec.Get(s, name)
		if err != nil {
			return "", err
		}
		v, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("unexpected type %T for character column %s", raw, name)
		}
		return strings.TrimRight(v, " "), nil
	}
	oid, err := getInt(FieldObjectID)
	if err != nil {
		return row{}, err
	}
	pid, err := getInt(FieldParentID)
	if err != nil {
		return row{}, err
	}
	ot, err := getStr(FieldObjectType)
	if err != nil {
		return row{}, err
	}
	on, err := getStr(FieldObjectName)
	if err != nil {
		return row{}, err
	}
	return row{
		ObjectID:   oid,
		ParentID:   pid,
		ObjectType: ot,
		ObjectName: on,
	}, nil
}

// truncateName clips a long name to MaxLongName characters and
// strips leading/trailing spaces.
func truncateName(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > MaxLongName {
		s = s[:MaxLongName]
	}
	return s
}

// shortenName returns the ≤10-char short form of a long name,
// matching what a dBASE III+ field descriptor holds.
func shortenName(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// padTo right-pads s with spaces to width, or truncates.
func padTo(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

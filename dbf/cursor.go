package dbf

// Cursor returns a cursor positioned before the first record.
//
// The cursor visits every record in physical order, including records
// marked as deleted; callers filter on Record.Deleted as needed,
// mirroring Clipper's SET DELETED OFF default.
func (t *Table) Cursor() *Cursor {
	return &Cursor{
		table: t,
	}
}

// Next advances the cursor to the next record.
//
// It returns false when the cursor moves past the last record or when
// an error occurs; Err distinguishes the two.
func (c *Cursor) Next() bool {
	if c.err != nil {
		return false
	}

	if c.recno >= c.table.recordCount {
		return false
	}

	c.recno++

	record, err := c.table.Get(c.recno)
	if err != nil {
		c.err = err
		return false
	}

	c.current = record

	return true
}

// Record returns the record at the current cursor position.
//
// It is only valid after a call to Next that returned true.
func (c *Cursor) Record() Record {
	return c.current
}

// Recno returns the one-based record number at the current cursor
// position.
//
// It is only valid after a call to Next that returned true.
func (c *Cursor) Recno() uint32 {
	return c.recno
}

// Err returns the first error encountered during traversal.
func (c *Cursor) Err() error {
	return c.err
}

package blipperdb

import (
	"fmt"
	"io"

	"github.com/ha1tch/blipper/dbf"
	"github.com/ha1tch/blipper/ntx"
)

// Area is one work area: an open table, its attached indexes, a
// controlling order, and a record pointer.
//
// The record pointer follows xBase conventions: Eof reports true
// after skipping past the last record, Bof after skipping before the
// first. Deleted records are visible, mirroring SET DELETED OFF.
type Area struct {
	alias string
	table *dbf.Table
	src   io.ReadWriteSeeker

	indexes []*attachedIndex

	// order selects the controlling index: 0 is natural (physical)
	// order, n selects indexes[n-1].
	order int

	recno uint32
	atEOF bool
	atBOF bool
}

// attachedIndex pairs an open index with the key function that
// derives its keys from records.
type attachedIndex struct {
	index *ntx.Index
	keyFn ntx.KeyFunc
	src   io.ReadWriteSeeker
}

// Alias returns the area's alias.
func (a *Area) Alias() string {
	return a.alias
}

// Table returns the area's table for direct access.
//
// Writes made directly to the table bypass index maintenance; use
// the Area write operations when indexes are attached.
func (a *Area) Table() *dbf.Table {
	return a.table
}

// SetIndex opens an existing NTX index from rw and attaches it,
// mirroring SET INDEX TO. The first attached index becomes the
// controlling order.
//
// keyFn must derive exactly the keys the index was built with.
func (a *Area) SetIndex(rw io.ReadWriteSeeker, keyFn ntx.KeyFunc) (*ntx.Index, error) {
	index, err := ntx.Open(rw)
	if err != nil {
		return nil, err
	}

	a.attach(index, keyFn, rw)

	return index, nil
}

// CreateIndex creates a new NTX index in rw, populates it from every
// record of the table, and attaches it, mirroring INDEX ON. The
// first attached index becomes the controlling order.
func (a *Area) CreateIndex(
	rw io.ReadWriteSeeker,
	opts ntx.Options,
	keyFn ntx.KeyFunc,
) (*ntx.Index, error) {
	index, err := ntx.Create(rw, opts)
	if err != nil {
		return nil, err
	}

	if err := ntx.Build(index, a.table, keyFn); err != nil {
		return nil, err
	}

	a.attach(index, keyFn, rw)

	return index, nil
}

func (a *Area) attach(index *ntx.Index, keyFn ntx.KeyFunc, src io.ReadWriteSeeker) {
	a.indexes = append(a.indexes, &attachedIndex{
		index: index,
		keyFn: keyFn,
		src:   src,
	})

	if len(a.indexes) == 1 {
		a.order = 1
	}
}

// SetOrder selects the controlling order: 0 for natural order, n for
// the n-th attached index, mirroring SET ORDER TO.
func (a *Area) SetOrder(order int) error {
	if order < 0 || order > len(a.indexes) {
		return fmt.Errorf(
			"order %d out of range: %d indexes attached",
			order,
			len(a.indexes),
		)
	}

	a.order = order

	return nil
}

// Order returns the controlling order.
func (a *Area) Order() int {
	return a.order
}

// controlling returns the controlling index, or nil in natural
// order.
func (a *Area) controlling() *attachedIndex {
	if a.order == 0 {
		return nil
	}

	return a.indexes[a.order-1]
}

// Eof reports whether the record pointer has moved past the last
// record.
func (a *Area) Eof() bool {
	return a.atEOF
}

// Bof reports whether the record pointer has moved before the first
// record.
func (a *Area) Bof() bool {
	return a.atBOF
}

// Recno returns the current record number, 0 when the table is
// empty.
func (a *Area) Recno() uint32 {
	return a.recno
}

// Record reads the record at the pointer.
func (a *Area) Record() (dbf.Record, error) {
	if a.atEOF || a.recno == 0 {
		return dbf.Record{}, fmt.Errorf("%s: at end of file", a.alias)
	}

	return a.table.Get(a.recno)
}

// GoTop moves the pointer to the first record in the controlling
// order, mirroring GO TOP.
func (a *Area) GoTop() error {
	a.atBOF = false

	if ctl := a.controlling(); ctl != nil {
		entry, found, err := ctl.index.Min()
		if err != nil {
			return err
		}

		if !found {
			return a.setEmpty()
		}

		a.recno = entry.Recno
		a.atEOF = false

		return nil
	}

	if a.table.RecordCount() == 0 {
		return a.setEmpty()
	}

	a.recno = 1
	a.atEOF = false

	return nil
}

// GoBottom moves the pointer to the last record in the controlling
// order, mirroring GO BOTTOM.
func (a *Area) GoBottom() error {
	a.atBOF = false

	if ctl := a.controlling(); ctl != nil {
		entry, found, err := ctl.index.Max()
		if err != nil {
			return err
		}

		if !found {
			return a.setEmpty()
		}

		a.recno = entry.Recno
		a.atEOF = false

		return nil
	}

	if a.table.RecordCount() == 0 {
		return a.setEmpty()
	}

	a.recno = a.table.RecordCount()
	a.atEOF = false

	return nil
}

// GoTo moves the pointer to a record number, mirroring GO n.
func (a *Area) GoTo(recno uint32) error {
	if recno == 0 || recno > a.table.RecordCount() {
		return fmt.Errorf(
			"%s: record %d out of range",
			a.alias,
			recno,
		)
	}

	a.recno = recno
	a.atEOF = false
	a.atBOF = false

	return nil
}

// Skip moves the pointer n records forward (or backward for negative
// n) in the controlling order, mirroring SKIP n.
//
// Skipping past the last record leaves the pointer on the last
// record with Eof true; skipping before the first leaves it on the
// first with Bof true, following xBase conventions.
func (a *Area) Skip(n int) error {
	for ; n > 0; n-- {
		if err := a.skipForward(); err != nil {
			return err
		}
		if a.atEOF {
			return nil
		}
	}

	for ; n < 0; n++ {
		if err := a.skipBackward(); err != nil {
			return err
		}
		if a.atBOF {
			return nil
		}
	}

	return nil
}

func (a *Area) skipForward() error {
	if a.recno == 0 {
		a.atEOF = true
		return nil
	}

	a.atBOF = false

	if ctl := a.controlling(); ctl != nil {
		key, err := a.currentKey(ctl)
		if err != nil {
			return err
		}

		entry, found, err := ctl.index.Successor(key, a.recno)
		if err != nil {
			return err
		}

		if !found {
			a.atEOF = true
			return nil
		}

		a.recno = entry.Recno

		return nil
	}

	if a.recno >= a.table.RecordCount() {
		a.atEOF = true
		return nil
	}

	a.recno++

	return nil
}

func (a *Area) skipBackward() error {
	if a.recno == 0 {
		a.atBOF = true
		return nil
	}

	a.atEOF = false

	if ctl := a.controlling(); ctl != nil {
		key, err := a.currentKey(ctl)
		if err != nil {
			return err
		}

		entry, found, err := ctl.index.Predecessor(key, a.recno)
		if err != nil {
			return err
		}

		if !found {
			a.atBOF = true
			return nil
		}

		a.recno = entry.Recno

		return nil
	}

	if a.recno <= 1 {
		a.atBOF = true
		return nil
	}

	a.recno--

	return nil
}

// Seek positions the pointer at the first record whose key is
// greater than or equal to the given key in the controlling order,
// mirroring SEEK with SOFTSEEK ON.
//
// It reports whether the key matched exactly (after space padding to
// the key size). With no match at all, the pointer parks at the last
// record with Eof true.
func (a *Area) Seek(key []byte) (bool, error) {
	ctl := a.controlling()
	if ctl == nil {
		return false, fmt.Errorf("%s: SEEK requires a controlling index", a.alias)
	}

	entry, found, err := ctl.index.FirstGE(key)
	if err != nil {
		return false, err
	}

	if !found {
		a.atEOF = true
		return false, nil
	}

	a.recno = entry.Recno
	a.atEOF = false
	a.atBOF = false

	// An exact match up to the sought prefix.
	matched := len(key) <= len(entry.Key) &&
		string(entry.Key[:len(key)]) == string(key)

	return matched, nil
}

// Append adds a record after the last one, inserts its keys into
// every attached index, and moves the pointer to it, mirroring
// APPEND.
func (a *Area) Append(record dbf.Record) (uint32, error) {
	recno, err := a.table.Append(record)
	if err != nil {
		return 0, err
	}

	for _, att := range a.indexes {
		if _, err := att.index.Insert(att.keyFn(record), recno); err != nil {
			return 0, fmt.Errorf(
				"%s: updating index after append: %w",
				a.alias,
				err,
			)
		}
	}

	a.recno = recno
	a.atEOF = false
	a.atBOF = false

	return recno, nil
}

// Replace overwrites the record at the pointer, removing its old
// keys from every attached index and inserting the new ones,
// mirroring REPLACE.
func (a *Area) Replace(record dbf.Record) error {
	if a.recno == 0 {
		return fmt.Errorf("%s: no current record", a.alias)
	}

	old, err := a.table.Get(a.recno)
	if err != nil {
		return err
	}

	if err := a.table.Put(a.recno, record); err != nil {
		return err
	}

	for _, att := range a.indexes {
		oldKey := att.keyFn(old)
		newKey := att.keyFn(record)

		if string(oldKey) == string(newKey) {
			continue
		}

		// A unique index may not hold this record; a miss is fine.
		if _, err := att.index.Delete(oldKey, a.recno); err != nil {
			return fmt.Errorf(
				"%s: updating index after replace: %w",
				a.alias,
				err,
			)
		}

		if _, err := att.index.Insert(newKey, a.recno); err != nil {
			return fmt.Errorf(
				"%s: updating index after replace: %w",
				a.alias,
				err,
			)
		}
	}

	return nil
}

// Delete marks the record at the pointer as deleted, mirroring
// DELETE.
//
// Indexes are unchanged: Clipper keeps deleted records in its
// indexes.
func (a *Area) Delete() error {
	if a.recno == 0 {
		return fmt.Errorf("%s: no current record", a.alias)
	}

	return a.table.Delete(a.recno)
}

// Recall clears the deletion mark of the record at the pointer,
// mirroring RECALL.
func (a *Area) Recall() error {
	if a.recno == 0 {
		return fmt.Errorf("%s: no current record", a.alias)
	}

	return a.table.Undelete(a.recno)
}

func (a *Area) currentKey(att *attachedIndex) ([]byte, error) {
	record, err := a.table.Get(a.recno)
	if err != nil {
		return nil, err
	}

	return att.keyFn(record), nil
}

func (a *Area) setEmpty() error {
	a.recno = 0
	a.atEOF = true

	return nil
}

func (a *Area) close() error {
	var first error

	for _, att := range a.indexes {
		if err := closeIfCloser(att.src); err != nil && first == nil {
			first = err
		}
	}

	a.indexes = nil

	if err := closeIfCloser(a.src); err != nil && first == nil {
		first = err
	}

	return first
}

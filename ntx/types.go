// Package ntx implements Nantucket Clipper 5.x compatible .NTX index
// files.
//
// An NTX file is a modified B-tree with 1024-byte pages. Keys are
// fixed-size byte strings compared as unsigned bytes, with the DBF
// record number as tiebreak; producing key bytes from records is the
// caller's responsibility via KeyFunc and the helpers in keys.go.
package ntx

import (
	"io"

	"github.com/ha1tch/blipper/dbf"
)

// KeyFunc derives the fixed-size index key bytes for a record.
type KeyFunc func(dbf.Record) []byte

// Options describes a new index.
type Options struct {
	// KeyExpr is the Clipper key expression stored in the header,
	// e.g. "UPPER(NAME)". It documents how keys are derived; this
	// package does not evaluate it.
	KeyExpr string

	// KeySize is the fixed key length in bytes (1..250).
	KeySize uint16

	// Decimals is the decimal count recorded for numeric keys.
	Decimals uint16

	// Unique indexes keep only the first record for each key value,
	// matching Clipper's INDEX ON ... UNIQUE.
	Unique bool
}

// Index represents an open NTX index file.
type Index struct {
	rw io.ReadWriteSeeker

	keyExpr  string
	keySize  uint16
	decimals uint16
	unique   bool

	itemSize uint16
	maxItem  uint16
	halfPage uint16

	root     int64
	nextFree int64
	fileSize int64
}

// Entry is one index entry: a key and the record number it points at.
type Entry struct {
	Key   []byte
	Recno uint32
}

// Cursor traverses an index in key order.
type Cursor struct {
	index   *Index
	stack   []frame
	current Entry
	err     error
}

// frame is one level of a cursor's descent.
type frame struct {
	offset int64
	index  int
	// entered reports whether the child left of items[index] has
	// already been visited.
	entered bool
}

// Package blipperdb: CDX tag attachment.
//
// This file adds a read-oriented attachment mechanism for CDX
// tags, mirroring SET INDEX TO for compound indexes. Writes made
// through the Area (Append, Replace) do NOT update attached CDX
// tags in this batch; that requires an index-maintenance layer
// (Insert, Delete, Update) which the cdx package will grow in a
// following phase. Callers who need write-through consistency
// should rebuild the CDX from scratch after a write pass by
// re-invoking BuildCDX with fresh entries derived from the table.
//
// The attachment surface is deliberately small:
//
//	AttachCDX opens a CDX and makes its tags available.
//	SetOrderCDX selects one tag as the controlling order.
//	TraverseCDX walks the controlling tag's leaves without changing
//	the record pointer, useful for full index-order scans.
//
// Ordering via CDX tags is orthogonal to the NTX SetOrder integer
// numbering (which selects among attached NTX files); tag names
// are the unit of selection here.
package blipperdb

import (
	"fmt"
	"io"

	"github.com/ha1tch/blipper/cdx"
)

// AttachedCDX pairs an open CDX file with the Area it serves.
// Tags become available by name via SetOrderCDX.
type AttachedCDX struct {
	file        *cdx.File
	controlling string // empty means no CDX-based controlling order

	// src is retained so Area.close can release the handle.
	src io.ReadSeeker

	// rw is non-nil when the CDX was attached through a
	// writable stream. Rebuild needs to write the file back,
	// so a CDX attached read-only reports that rather than
	// failing obscurely partway through a pack.
	rw io.ReadWriteSeeker
}

// AttachCDX opens a CDX from the supplied reader and stores it on
// the area. Subsequent calls replace the previous attachment.
//
// The reader stays owned by the caller: closing it is the caller's
// responsibility, and the CDX package uses it only for reads.
func (a *Area) AttachCDX(rw io.ReadSeeker) (*AttachedCDX, error) {
	f, err := cdx.Open(rw)
	if err != nil {
		return nil, fmt.Errorf("blipperdb: attach cdx: %w", err)
	}
	attached := &AttachedCDX{file: f, src: rw}
	if w, ok := rw.(io.ReadWriteSeeker); ok {
		attached.rw = w
	}
	a.cdx = attached
	return a.cdx, nil
}

// CDX returns the currently attached CDX, or nil if none.
func (a *Area) CDX() *AttachedCDX { return a.cdx }

// SetOrderCDX makes the named tag the controlling order for this
// area. Passing "" clears the CDX-based controlling order (falls
// back to natural order unless an NTX SetOrder is active).
func (a *Area) SetOrderCDX(tagName string) error {
	if a.cdx == nil {
		return fmt.Errorf("blipperdb: no cdx attached")
	}
	if tagName == "" {
		a.cdx.controlling = ""
		return nil
	}
	if _, err := a.cdx.file.Tag(tagName); err != nil {
		return err
	}
	a.cdx.controlling = tagName
	return nil
}

// TraverseCDX walks the controlling CDX tag's entries in index
// order, calling fn with the record number for each entry. It
// does not move the Area's record pointer; callers wanting to
// visit records may call Area.Goto(entry.RecNo) inside fn.
func (a *Area) TraverseCDX(fn func(recNo uint32) error) error {
	if a.cdx == nil {
		return fmt.Errorf("blipperdb: no cdx attached")
	}
	if a.cdx.controlling == "" {
		return fmt.Errorf("blipperdb: no controlling cdx tag; call SetOrderCDX first")
	}
	tag, err := a.cdx.file.Tag(a.cdx.controlling)
	if err != nil {
		return err
	}
	return a.cdx.file.Traverse(tag, func(e cdx.Entry) error {
		return fn(e.RecNo)
	})
}

// CDXTags returns the names of every tag in the attached CDX,
// preserving the order the tag directory yields them (i.e.
// alphabetical under MACHINE collation). If no CDX is attached
// the return value is nil.
func (a *Area) CDXTags() []string {
	if a.cdx == nil {
		return nil
	}
	return a.cdx.file.TagNames()
}

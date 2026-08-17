// Package blipperdb: shared-access modes and locking.
//
// Blipper has had no locking of any kind: every type documents
// itself as unsafe for concurrent use and means it. For a
// single-process tool that is honest and sufficient. For anything
// else it is a hazard where a caller can be silently wrong rather
// than obviously blocked, which is why this landed at a higher
// priority than the rest of the backlog.
//
// The vocabulary is Clipper's, deliberately. USE ... EXCLUSIVE
// versus SHARED, FLOCK for the whole file, RLOCK for the current
// record, UNLOCK to release. Those semantics are documented,
// understood by anyone who worked with these files, and match
// what the formats were designed around — inventing a new
// vocabulary would only obscure a well-mapped problem.
//
// This is the first stage: modes, the Locker seam, and
// enforcement of exclusive access. Shared record locking against
// other processes needs cache invalidation on lock acquisition —
// Table.recordCount, cdx nodes, dbc rows and the fatfs FAT are
// all cached — and that is a larger change scoped separately
// rather than smuggled in here.
package blipperdb

import (
	"errors"
	"fmt"
	"io"
)

// OpenMode selects how an area shares its table with other users.
type OpenMode int

const (
	// Exclusive opens the table for this process alone. Writes
	// need no further locking, which is why it is the default:
	// the common case is one process owning its data, and
	// requiring a lock call for that would be ceremony.
	Exclusive OpenMode = iota

	// Shared opens the table alongside other users. Write
	// operations require an explicit lock — FLock for the file
	// or RLock for a record — and fail without one, rather than
	// writing and hoping.
	Shared
)

func (m OpenMode) String() string {
	if m == Shared {
		return "SHARED"
	}
	return "EXCLUSIVE"
}

// Errors returned by the locking layer.
var (
	// ErrNotLocked is returned by a write attempted on a shared
	// area with no lock held.
	ErrNotLocked = errors.New("blipperdb: write requires a lock in SHARED mode")

	// ErrLockUnsupported is returned when the underlying storage
	// cannot lock. A FAT image has no locking mechanism, so
	// saying so is better than pretending a lock was taken.
	ErrLockUnsupported = errors.New("blipperdb: underlying storage does not support locking")

	// ErrLockHeld is returned when another user holds a
	// conflicting lock.
	ErrLockHeld = errors.New("blipperdb: lock held by another user")
)

// Locker is implemented by storage that can coordinate access
// between users.
//
// It sits on the stream rather than on the FileSet because locks
// are per-file: two areas over one directory lock independently.
// Implementations that cannot lock simply do not implement it,
// following the Flusher precedent — OSDir provides real file
// locks, sqlitefs has SQLite's own, and a FAT image has none and
// says so.
//
// Regions are byte ranges within the file. dBASE and FoxPro
// locked conventional offsets rather than whole files, so an
// implementation aiming at interoperability locks the same
// regions those products used; blipper's own conventions are
// defined by lockRegion below.
type Locker interface {
	// LockRegion takes a lock over a byte range, blocking or
	// failing according to the implementation. exclusive selects
	// a write lock over a read lock.
	LockRegion(off, length int64, exclusive bool) error

	// UnlockRegion releases a previously taken lock.
	UnlockRegion(off, length int64) error
}

// Lock-region conventions.
//
// dBASE and FoxPro placed their locks at offsets far beyond any
// real data so that a lock byte never collided with a record.
// Blipper follows the same idea with its own constants: what
// matters within one library is that file and record locks cannot
// overlap, and that a record lock's offset is derived from its
// number so two records never share one.
const (
	// lockFileBase is the offset of the whole-file lock byte.
	lockFileBase = int64(1) << 30

	// lockRecordBase is where per-record lock bytes begin.
	lockRecordBase = int64(1) << 31
)

// lockRegionForRecord returns the byte range representing a lock
// on one record.
func lockRegionForRecord(recno uint32) (off, length int64) {
	return lockRecordBase + int64(recno), 1
}

// heldLock records what an area currently has locked, so Unlock
// can release exactly that and a double-lock can be refused.
type heldLock struct {
	off    int64
	length int64
	recno  uint32 // 0 for a file lock
}

// Mode returns the area's open mode.
func (a *Area) Mode() OpenMode { return a.mode }

// locker returns the area's stream as a Locker, or nil when the
// storage cannot lock.
func (a *Area) locker() Locker {
	if l, ok := a.src.(Locker); ok {
		return l
	}
	return nil
}

// FLock takes an exclusive lock on the whole file, mirroring
// Clipper's FLOCK().
//
// In Exclusive mode this succeeds trivially: the area already has
// the file to itself, and requiring a real lock would only make
// the common case awkward.
func (a *Area) FLock() error {
	if a.mode == Exclusive {
		a.held = &heldLock{off: lockFileBase, length: 1}
		return nil
	}
	l := a.locker()
	if l == nil {
		return ErrLockUnsupported
	}
	if a.held != nil {
		return fmt.Errorf("blipperdb: already holding a lock")
	}
	if err := l.LockRegion(lockFileBase, 1, true); err != nil {
		return fmt.Errorf("%w: %v", ErrLockHeld, err)
	}
	a.held = &heldLock{off: lockFileBase, length: 1}
	return nil
}

// RLock takes an exclusive lock on the current record, mirroring
// Clipper's RLOCK().
func (a *Area) RLock() error {
	if a.recno == 0 {
		return fmt.Errorf("blipperdb: no current record to lock")
	}
	off, length := lockRegionForRecord(a.recno)
	if a.mode == Exclusive {
		a.held = &heldLock{off: off, length: length, recno: a.recno}
		return nil
	}
	l := a.locker()
	if l == nil {
		return ErrLockUnsupported
	}
	if a.held != nil {
		return fmt.Errorf("blipperdb: already holding a lock")
	}
	if err := l.LockRegion(off, length, true); err != nil {
		return fmt.Errorf("%w: %v", ErrLockHeld, err)
	}
	a.held = &heldLock{off: off, length: length, recno: a.recno}
	return nil
}

// Unlock releases whatever lock the area holds, mirroring
// Clipper's UNLOCK. Releasing nothing is not an error, matching
// Clipper.
func (a *Area) Unlock() error {
	if a.held == nil {
		return nil
	}
	held := a.held
	a.held = nil
	if a.mode == Exclusive {
		return nil
	}
	l := a.locker()
	if l == nil {
		return nil
	}
	return l.UnlockRegion(held.off, held.length)
}

// Locked reports whether the area holds a lock.
func (a *Area) Locked() bool { return a.held != nil }

// checkWritable is called by every write path. In Exclusive mode
// it always passes; in Shared mode it requires a lock covering
// what is about to be written.
//
// recno is the record being written, or 0 for an operation
// affecting the whole file such as Append or Pack — those need a
// file lock, since they change the record count.
func (a *Area) checkWritable(recno uint32) error {
	if a.mode == Exclusive {
		return nil
	}
	if a.held == nil {
		return ErrNotLocked
	}
	if a.held.recno == 0 {
		return nil // a file lock covers everything
	}
	if recno == 0 {
		return fmt.Errorf("%w: this operation changes the file and needs FLock, not RLock",
			ErrNotLocked)
	}
	if a.held.recno != recno {
		return fmt.Errorf("%w: holding a lock on record %d, writing record %d",
			ErrNotLocked, a.held.recno, recno)
	}
	return nil
}

// UseMode opens a table in the given mode, registering it under
// the alias.
//
// BlipperDB.Use opens Exclusive, preserving the behaviour every
// existing caller relies on. A caller wanting shared access asks
// for it, which is the right way round: sharing changes what
// operations require and should not arrive by surprise.
func (db *BlipperDB) UseMode(alias string, rw io.ReadWriteSeeker, mode OpenMode) (*Area, error) {
	area, err := db.Use(alias, rw)
	if err != nil {
		return nil, err
	}
	area.mode = mode
	return area, nil
}

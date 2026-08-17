package blipperfs

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// lockableFile wraps an *os.File with byte-range locking, so a
// blipperdb.Area over a real file can coordinate with other
// processes.
//
// The locks are POSIX record locks via fcntl, not flock. That
// choice matters: fcntl locks apply to byte ranges, which is what
// per-record locking needs, and they are what other database
// software on the same platform uses. flock would lock whole
// files only.
//
// A caveat worth stating rather than discovering: POSIX record
// locks are released when *any* descriptor for the file is
// closed by this process, and they do not stack per-descriptor.
// Two areas in one process over the same path will therefore
// interfere. Within a process, exclusive mode is the right
// answer; these locks exist to coordinate between processes.
type lockableFile struct {
	*os.File
}

// LockRegion takes a POSIX record lock over a byte range.
func (f *lockableFile) LockRegion(off, length int64, exclusive bool) error {
	typ := int16(unix.F_RDLCK)
	if exclusive {
		typ = unix.F_WRLCK
	}
	lk := &unix.Flock_t{
		Type:   typ,
		Whence: io.SeekStart,
		Start:  off,
		Len:    length,
	}
	// F_SETLK rather than F_SETLKW: fail immediately when another
	// user holds the range, rather than blocking indefinitely. A
	// caller that wants to wait can retry, which keeps the
	// blocking policy where the caller can see it.
	if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, lk); err != nil {
		return fmt.Errorf("locking bytes %d..%d: %w", off, off+length, err)
	}
	return nil
}

// UnlockRegion releases a lock over a byte range.
func (f *lockableFile) UnlockRegion(off, length int64) error {
	lk := &unix.Flock_t{
		Type:   unix.F_UNLCK,
		Whence: io.SeekStart,
		Start:  off,
		Len:    length,
	}
	if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, lk); err != nil {
		return fmt.Errorf("unlocking bytes %d..%d: %w", off, off+length, err)
	}
	return nil
}

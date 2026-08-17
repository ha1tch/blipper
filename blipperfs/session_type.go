package blipperfs

import (
	"fmt"
	"strings"

	"github.com/ha1tch/blipper/blipperdb"
)

// Session is a BlipperDB bound to the FileSet its tables came
// from. Binding the two removes the most common piece of
// bookkeeping in the package API: a caller who opened a directory
// should not have to carry the directory around separately in
// order to open one more table from it.
//
// The embedded *blipperdb.BlipperDB means every session method
// — Select, Area, Aliases, CloseArea, CloseAll — is available
// directly:
//
//	s, _ := blipperfs.OpenDir("/data")
//	area, _ := s.Select("CUSTOMERS")
//	s.Use("ARCHIVE", "ARCHIVE.DBF")   // same directory, no fs argument
//
// For an alternative backend — an in-memory set in tests, or a
// future WebDAV or object-store driver — construct the FileSet
// explicitly and use NewSession:
//
//	s := blipperfs.NewSession(myRemoteFS)
//	area, _ := s.Use("CUST", "CUSTOMERS.DBF")
type Session struct {
	*blipperdb.BlipperDB
	fs FileSet
}

// NewSession returns an empty Session over the supplied FileSet.
// This is the constructor to use with a non-OS backend; OpenDir
// is the shorthand for the common on-disk case.
func NewSession(fs FileSet) *Session {
	return &Session{BlipperDB: blipperdb.New(), fs: fs}
}

// FileSet returns the session's underlying FileSet, for callers
// that need to inspect or share it.
func (s *Session) FileSet() FileSet { return s.fs }

// Use opens the named table from the session's FileSet and
// registers it under the given alias, resolving every sibling the
// table declares. See the package-level Use for the resolution
// rules.
func (s *Session) Use(alias, name string) (*blipperdb.Area, error) {
	return Use(s.BlipperDB, s.fs, alias, name)
}

// CreateTable writes a complete file-set into the session's
// FileSet and registers it under the given alias. See the
// package-level CreateTable for what the spec controls.
func (s *Session) CreateTable(alias, stem string, spec TableSpec) (*blipperdb.Area, error) {
	return CreateTable(s.BlipperDB, s.fs, alias, stem, spec)
}

// OpenDir opens every table in a directory as one session.
//
// Scans path for *.DBF files, derives each alias from the file
// stem (uppercased, matching xBase alias conventions), and opens
// each with full sibling resolution. Returns a Session bound to
// that directory, so further Use and CreateTable calls need no
// FileSet argument.
//
// A data directory was the database in these applications; this
// is the constructor that matches that model.
//
// If any table fails to open, OpenDir returns the partially
// populated session alongside the error, so callers can decide
// whether to proceed with what opened successfully.
func OpenDir(path string) (*Session, error) {
	return OpenFileSet(OSDir(path))
}

// OpenFileSet is OpenDir for an arbitrary backend: it scans the
// FileSet for *.DBF entries and opens each one. Use this with a
// MemFileSet in tests, or with a custom driver.
func OpenFileSet(fs FileSet) (*Session, error) {
	s := NewSession(fs)
	var firstErr error
	for _, name := range fs.List() {
		if !strings.EqualFold(extOf(name), ".DBF") {
			continue
		}
		alias := strings.ToUpper(stemOf(name))
		if _, err := s.Use(alias, name); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("blipperfs: opening %s: %w", name, err)
		}
	}
	return s, firstErr
}

// Close releases every open table in the session and commits the
// FileSet if it buffers writes.
//
// Order matters: areas are closed first so their handles are
// released, then the FileSet is flushed so any cached metadata
// those closes produced reaches storage. Reversing it would flush
// before the last writes landed.
//
// For an OS-backed session this releases file descriptors, which
// is the practical reason to call it: a session over a directory
// of memo-bearing tables holds several descriptors per table, and
// nothing else returns them. For a container-backed session
// (a FAT image, say) it is also the commit point — without it,
// cached FAT and directory updates are discarded.
func (s *Session) Close() error {
	first := s.CloseAll()
	if fl, ok := s.fs.(Flusher); ok {
		if err := fl.Flush(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

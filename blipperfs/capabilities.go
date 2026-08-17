package blipperfs

import (
	"github.com/ha1tch/blipper/fatfs"
	"github.com/ha1tch/blipper/sqlitefs"
)

// Capability interfaces.
//
// A FileSet is the common denominator: open, create, exists,
// list. Backends can do more than that, and what they can do
// differs — an OS directory has file locking, a FAT image has a
// cluster geometry, a SQLite tablespace has transactions. Rather
// than widen FileSet with methods most implementations would
// have to refuse, each capability is its own small interface a
// caller can assert against by name.
//
// This is the pattern Flusher established and Compactable
// followed in blipperdb: an interface describes a property some
// implementations have, not a duty all of them owe. Asserting
// against a named interface also documents the intent at the
// call site in a way an inline literal does not:
//
//	if v, ok := fs.(FATBacked); ok {
//	    fmt.Println(v.Volume().Type())
//	}

// FATBacked is implemented by FileSets backed by a FAT disk
// image, exposing the underlying volume for FAT-specific detail:
// the variant in use, cluster size, free space, or long-name
// configuration.
type FATBacked interface {
	Volume() *fatfs.Volume
}

// SQLiteBacked is implemented by FileSets backed by a SQLite
// tablespace, exposing the underlying store for detail the
// FileSet interface does not carry — the configured chunk size,
// or the database handle for a caller running its own queries
// alongside blipper's tables.
type SQLiteBacked interface {
	Store() *sqlitefs.FS
}

// DirBacked is implemented by FileSets backed by a directory on
// disk, exposing the root path.
//
// Useful for diagnostics, for resolving a sibling file blipper
// does not manage, and for callers that need to hand a path to
// something outside this library.
type DirBacked interface {
	Root() string
}

// Root returns the directory this FileSet is rooted at,
// satisfying DirBacked.
func (f *osFileSet) Root() string { return f.root }

// Compile-time assertions that each backend implements the
// capability it claims. Without these the interfaces are
// aspirational: a rename or a signature change would break the
// assertion silently at every call site instead of here.
var (
	_ DirBacked    = (*osFileSet)(nil)
	_ FATBacked    = (*fatFileSet)(nil)
	_ Flusher      = (*fatFileSet)(nil)
	_ SQLiteBacked = (*sqliteFileSet)(nil)
	_ Flusher      = (*sqliteFileSet)(nil)
)

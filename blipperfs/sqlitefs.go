package blipperfs

import (
	"fmt"
	"io"

	"github.com/ha1tch/blipper/sqlitefs"
)

// sqliteFileSet adapts a sqlitefs.FS to the FileSet interface.
//
// As with the FAT adapter, this lives in blipperfs rather than in
// sqlitefs so the dependency points one way: sqlitefs is a
// chunked file store that knows nothing about blipper, and this
// file is the only place the two meet.
type sqliteFileSet struct {
	fs *sqlitefs.FS
}

// SQLiteTablespace opens or creates a SQLite-backed tablespace at
// the given path and returns it as a FileSet, so an entire xBase
// dataset lives in one database file:
//
//	fs, err := blipperfs.SQLiteTablespace("data.db")
//	s := blipperfs.NewSession(fs)
//	s.CreateTable("CUST", "CUSTOMERS", spec)
//	s.Close()
//
// Unlike OSDir and FAT images, this container is transactional:
// everything written between Flush calls commits together. That
// matters because blipper writes a record, its index entry, and
// its memo block as three separate stream writes with no commit
// boundary of their own — elsewhere a crash between them leaves
// an inconsistent set.
//
// Options are passed through to sqlitefs; WithChunkSize is the
// one most likely to matter.
//
// Long filenames need no option here. The name column is TEXT,
// holding arbitrary UTF-8, so long names, spaces, accented
// characters, and punctuation illegal in 8.3 all work
// unconditionally. Only FAT has the 8.3 restriction that
// fatfs.WithLongNames exists to lift.
func SQLiteTablespace(path string, opts ...sqlitefs.Option) (FileSet, error) {
	fs, err := sqlitefs.Open(path, opts...)
	if err != nil {
		return nil, fmt.Errorf("blipperfs: open SQLite tablespace: %w", err)
	}
	return &sqliteFileSet{fs: fs}, nil
}

func (s *sqliteFileSet) Open(name string) (io.ReadWriteSeeker, error) {
	return s.fs.Open(name)
}

func (s *sqliteFileSet) Create(name string) (io.ReadWriteSeeker, error) {
	return s.fs.Create(name)
}

func (s *sqliteFileSet) Exists(name string) bool { return s.fs.Exists(name) }

func (s *sqliteFileSet) List() []string { return s.fs.List() }

// Flush commits every write since the last commit.
func (s *sqliteFileSet) Flush() error { return s.fs.Flush() }

// Close commits outstanding writes and closes the database.
func (s *sqliteFileSet) Close() error { return s.fs.Close() }

// Store exposes the underlying sqlitefs.FS for callers needing
// detail the FileSet interface deliberately does not carry, such
// as the configured chunk size.
func (s *sqliteFileSet) Store() *sqlitefs.FS { return s.fs }

// Package sqlitefs stores files as chunked blobs in a SQLite
// database, giving a single-file container with atomic
// multi-file commit.
//
// The package depends on SQLite and the standard library, and
// knows nothing about its consumers. It is a chunked file store
// that happens to have blipper as its first user.
//
// Schema:
//
//	files  (id, name UNIQUE COLLATE NOCASE, size)
//	chunks (file_id -> files.id, idx, data, PK (file_id, idx))
//
// Chunks carry a file's bytes in fixed-size pieces, so a seek is
// a chunk lookup, a write touches one row, and growth appends a
// row. This sidesteps the fixed-size constraint on SQLite blob
// handles — sqlite3_blob_write cannot extend a blob, but with
// chunking nothing ever needs to — and requires only ordinary
// SELECT/INSERT/UPDATE rather than the incremental blob API.
//
// The chunks primary key is (file_id, idx) on a WITHOUT ROWID
// table, which makes the key the physical storage order: a
// file's chunks are stored adjacently in index order, so a
// sequential scan walks the B-tree in storage order and a random
// seek is a single descent.
//
// Chunk size defaults to 32 KB, measured rather than assumed;
// see bench/chunksize in the blipper repository for the numbers
// and their caveats. It is configurable because the optimum
// depends on working-set size, page-cache behaviour, and whether
// SQLite is the pure-Go or cgo build.
package sqlitefs

import (
	"database/sql"
	"errors"
	"fmt"
	"io"

	_ "modernc.org/sqlite"
)

// DefaultChunkSize is the measured default: fastest across
// header-rewrite, record-append, index-descent, and sequential-scan
// workloads on SSD-backed storage, at 1.5% storage overhead.
const DefaultChunkSize = 32 * 1024

// MinChunkSize guards against pathological configurations. Below
// roughly a SQLite page there is no benefit and considerable
// per-row overhead.
const MinChunkSize = 512

// Errors returned by this package.
var (
	// ErrNotFound is returned when a named file is absent.
	ErrNotFound = errors.New("sqlitefs: file not found")

	// ErrClosed is returned by operations on a closed FS.
	ErrClosed = errors.New("sqlitefs: filesystem is closed")
)

// Option configures an FS at construction.
type Option func(*config)

type config struct {
	chunkSize int
}

// WithChunkSize sets the chunk size in bytes. Values below
// MinChunkSize are rejected at Open.
func WithChunkSize(n int) Option {
	return func(c *config) { c.chunkSize = n }
}

// FS is a chunked file store over a SQLite database.
//
// Writes accumulate in an open transaction and become durable on
// Flush. A crash before Flush rolls back to the previous commit,
// so a set of files written together either all land or none do.
// That is the property this package exists to provide.
//
// An FS is not safe for concurrent use.
type FS struct {
	db        *sql.DB
	tx        *sql.Tx
	chunkSize int
	ownsDB    bool
	closed    bool
}

// Open opens or creates a tablespace at the given path.
func Open(path string, opts ...Option) (*FS, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlitefs: open %s: %w", path, err)
	}
	fs, err := newFS(db, true, opts...)
	if err != nil {
		db.Close()
		return nil, err
	}
	return fs, nil
}

// OpenDB wraps a caller-owned database handle. Close will not
// close it; that stays the caller's responsibility.
func OpenDB(db *sql.DB, opts ...Option) (*FS, error) {
	return newFS(db, false, opts...)
}

func newFS(db *sql.DB, ownsDB bool, opts ...Option) (*FS, error) {
	cfg := config{chunkSize: DefaultChunkSize}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.chunkSize < MinChunkSize {
		return nil, fmt.Errorf("sqlitefs: chunk size %d below minimum %d",
			cfg.chunkSize, MinChunkSize)
	}

	// WAL is what any real deployment uses; foreign keys must be
	// enabled explicitly for ON DELETE CASCADE to fire, which is
	// what makes Remove atomic.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("sqlitefs: %s: %w", pragma, err)
		}
	}

	schema := []string{
		`CREATE TABLE IF NOT EXISTS files (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE COLLATE NOCASE,
			size INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS chunks (
			file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
			idx     INTEGER NOT NULL,
			data    BLOB NOT NULL,
			PRIMARY KEY (file_id, idx)
		) WITHOUT ROWID`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			return nil, fmt.Errorf("sqlitefs: create schema: %w", err)
		}
	}

	fs := &FS{db: db, chunkSize: cfg.chunkSize, ownsDB: ownsDB}
	if err := fs.begin(); err != nil {
		return nil, err
	}
	return fs, nil
}

// ChunkSize returns the configured chunk size.
func (f *FS) ChunkSize() int { return f.chunkSize }

// begin starts the transaction that accumulates writes until the
// next Flush.
func (f *FS) begin() error {
	tx, err := f.db.Begin()
	if err != nil {
		return fmt.Errorf("sqlitefs: begin: %w", err)
	}
	f.tx = tx
	return nil
}

// Flush commits accumulated writes and opens a fresh transaction.
//
// This is the commit point: every file written since the last
// Flush becomes durable together. Blipper writes a DBF record,
// its CDX entry, and its FPT block as three separate stream
// writes with no commit boundary of their own; on an ordinary
// filesystem a crash between them leaves an inconsistent set,
// and here it does not.
func (f *FS) Flush() error {
	if f.closed {
		return ErrClosed
	}
	if f.tx == nil {
		return nil
	}
	if err := f.tx.Commit(); err != nil {
		return fmt.Errorf("sqlitefs: commit: %w", err)
	}
	f.tx = nil
	return f.begin()
}

// Close commits outstanding writes and releases the database if
// this FS owns it.
func (f *FS) Close() error {
	if f.closed {
		return nil
	}
	var first error
	if f.tx != nil {
		if err := f.tx.Commit(); err != nil {
			first = fmt.Errorf("sqlitefs: commit on close: %w", err)
		}
		f.tx = nil
	}
	f.closed = true
	if f.ownsDB {
		if err := f.db.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// fileID resolves a name to its files.id and logical size.
func (f *FS) fileID(name string) (id int64, size int64, err error) {
	err = f.tx.QueryRow("SELECT id, size FROM files WHERE name = ?", name).
		Scan(&id, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return 0, 0, err
	}
	return id, size, nil
}

// Exists reports whether a file is present.
func (f *FS) Exists(name string) bool {
	if f.closed {
		return false
	}
	_, _, err := f.fileID(name)
	return err == nil
}

// Stat returns a file's logical size.
//
// This reads files.size directly. Without that column the size
// would have to be derived as (chunk_count-1)*chunkSize plus the
// length of the last chunk, which means reading a chunk merely
// to learn a length.
func (f *FS) Stat(name string) (int64, error) {
	if f.closed {
		return 0, ErrClosed
	}
	_, size, err := f.fileID(name)
	return size, err
}

// List returns every filename, sorted.
func (f *FS) List() []string {
	if f.closed {
		return nil
	}
	rows, err := f.tx.Query("SELECT name FROM files ORDER BY name")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return out
		}
		out = append(out, n)
	}
	return out
}

// Remove deletes a file and its chunks. The chunks go via
// ON DELETE CASCADE, so the deletion is one statement rather
// than two that must both succeed.
func (f *FS) Remove(name string) error {
	if f.closed {
		return ErrClosed
	}
	id, _, err := f.fileID(name)
	if err != nil {
		return err
	}
	_, err = f.tx.Exec("DELETE FROM files WHERE id = ?", id)
	return err
}

// Open returns a handle to an existing file.
func (f *FS) Open(name string) (*File, error) {
	if f.closed {
		return nil, ErrClosed
	}
	id, size, err := f.fileID(name)
	if err != nil {
		return nil, err
	}
	return &File{fs: f, id: id, name: name, size: size}, nil
}

// Create returns a handle to a new empty file, truncating any
// existing file of the same name.
func (f *FS) Create(name string) (*File, error) {
	if f.closed {
		return nil, ErrClosed
	}
	if id, _, err := f.fileID(name); err == nil {
		// Truncate in place, keeping the id so any other handle
		// on this file observes the truncation rather than
		// silently addressing a stale row.
		if _, err := f.tx.Exec("DELETE FROM chunks WHERE file_id = ?", id); err != nil {
			return nil, err
		}
		if _, err := f.tx.Exec("UPDATE files SET size = 0 WHERE id = ?", id); err != nil {
			return nil, err
		}
		return &File{fs: f, id: id, name: name, size: 0}, nil
	}
	res, err := f.tx.Exec("INSERT INTO files(name, size) VALUES(?, 0)", name)
	if err != nil {
		return nil, fmt.Errorf("sqlitefs: create %s: %w", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &File{fs: f, id: id, name: name, size: 0}, nil
}

// File is an open file, satisfying io.ReadWriteSeeker.
type File struct {
	fs   *FS
	id   int64
	name string
	size int64
	pos  int64
}

// Name returns the file's name.
func (fl *File) Name() string { return fl.name }

// Size returns the file's current length.
func (fl *File) Size() int64 { return fl.size }

// Seek implements io.Seeker. Seeking past the end is allowed;
// the gap materialises only if a write follows.
func (fl *File) Seek(off int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = off
	case io.SeekCurrent:
		abs = fl.pos + off
	case io.SeekEnd:
		abs = fl.size + off
	default:
		return 0, fmt.Errorf("sqlitefs: invalid whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("sqlitefs: negative seek position %d", abs)
	}
	fl.pos = abs
	return abs, nil
}

// readChunk returns one chunk's bytes, or a zero-filled chunk
// when the row is absent — a file seeked past and written beyond
// leaves holes, and reading a hole yields zeroes rather than an
// error.
func (fl *File) readChunk(idx int64) ([]byte, error) {
	var data []byte
	err := fl.fs.tx.QueryRow(
		"SELECT data FROM chunks WHERE file_id = ? AND idx = ?", fl.id, idx).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return make([]byte, fl.fs.chunkSize), nil
	}
	if err != nil {
		return nil, err
	}
	// Short chunks are padded so callers can index uniformly;
	// the file's logical size bounds what is actually returned.
	if len(data) < fl.fs.chunkSize {
		padded := make([]byte, fl.fs.chunkSize)
		copy(padded, data)
		return padded, nil
	}
	return data, nil
}

// Read implements io.Reader, stopping at the file's logical size
// rather than the end of its last chunk.
func (fl *File) Read(p []byte) (int, error) {
	if fl.fs.closed {
		return 0, ErrClosed
	}
	if fl.pos >= fl.size {
		return 0, io.EOF
	}
	remaining := fl.size - fl.pos
	want := int64(len(p))
	if want > remaining {
		want = remaining
	}

	cs := int64(fl.fs.chunkSize)
	read := int64(0)
	for read < want {
		idx := (fl.pos + read) / cs
		chunk, err := fl.readChunk(idx)
		if err != nil {
			return int(read), err
		}
		inChunk := (fl.pos + read) % cs
		n := cs - inChunk
		if n > want-read {
			n = want - read
		}
		copy(p[read:read+n], chunk[inChunk:inChunk+n])
		read += n
	}
	fl.pos += read
	return int(read), nil
}

// Write implements io.Writer, read-modify-writing each chunk the
// range touches and extending the file's recorded size when the
// write passes the previous end.
func (fl *File) Write(p []byte) (int, error) {
	if fl.fs.closed {
		return 0, ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}

	cs := int64(fl.fs.chunkSize)
	written := int64(0)
	for written < int64(len(p)) {
		idx := (fl.pos + written) / cs
		inChunk := (fl.pos + written) % cs
		n := cs - inChunk
		if n > int64(len(p))-written {
			n = int64(len(p)) - written
		}

		var chunk []byte
		if inChunk == 0 && n == cs {
			// Whole-chunk overwrite: no need to read first.
			chunk = make([]byte, cs)
		} else {
			var err error
			chunk, err = fl.readChunk(idx)
			if err != nil {
				return int(written), err
			}
		}
		copy(chunk[inChunk:inChunk+n], p[written:written+n])

		if _, err := fl.fs.tx.Exec(
			"INSERT OR REPLACE INTO chunks(file_id, idx, data) VALUES(?,?,?)",
			fl.id, idx, chunk); err != nil {
			return int(written), err
		}
		written += n
	}

	fl.pos += written
	if fl.pos > fl.size {
		fl.size = fl.pos
		if _, err := fl.fs.tx.Exec("UPDATE files SET size = ? WHERE id = ?",
			fl.size, fl.id); err != nil {
			return int(written), err
		}
	}
	return int(written), nil
}

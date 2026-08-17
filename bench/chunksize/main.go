//go:build chunkbench

// Command chunkbench measures the cost of storing xBase files as
// chunked blobs in SQLite, across a range of chunk sizes.
//
// Behind a build tag so it does not run in the ordinary test
// sweep; see bench/chunksize/README.md for how to run it.
//
// The workloads mirror what blipper actually does rather than
// generic I/O:
//
//	header rewrite  32 bytes at offset 0, the Flush pattern
//	record append   ~80 bytes at the end, the Append pattern
//	index descent   4 scattered 512-byte reads, the CDX pattern
//	table scan      sequential read of the whole file
//
// Reported per chunk size: wall time for each workload, bytes on
// disk after a VACUUM, and the read amplification the index
// descent suffers.
package main

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// chunkSizes under test, in bytes.
var chunkSizes = []int{
	8 * 1024,
	16 * 1024,
	32 * 1024,
	48 * 1024,
	64 * 1024,
	128 * 1024,
	256 * 1024,
	512 * 1024,
}

// fileSize is the size of the simulated table. 20 MB is a large
// but not unreasonable xBase table: roughly 100k records of 200
// bytes.
const fileSize = 20 * 1024 * 1024

// store is a chunked-blob file store over SQLite.
type store struct {
	db        *sql.DB
	chunkSize int
	ids       map[string]int64
}

// fileID resolves a name to its files.id, creating the row on
// first use. Cached, as the real implementation caches it in the
// file handle.
func (s *store) fileID(name string) (int64, error) {
	if id, ok := s.ids[name]; ok {
		return id, nil
	}
	var id int64
	err := s.db.QueryRow("SELECT id FROM files WHERE name=?", name).Scan(&id)
	if err == sql.ErrNoRows {
		res, err := s.db.Exec("INSERT INTO files(name,size) VALUES(?,0)", name)
		if err != nil {
			return 0, err
		}
		if id, err = res.LastInsertId(); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	s.ids[name] = id
	return id, nil
}

func newStore(path string, chunkSize int) (*store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// WAL is what any real deployment would use; measuring
	// rollback-journal numbers would not reflect practice.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	for _, stmt := range []string{
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
	} {
		if _, err := db.Exec(stmt); err != nil {
			return nil, err
		}
	}
	return &store{db: db, chunkSize: chunkSize, ids: map[string]int64{}}, nil
}

func (s *store) Close() error { return s.db.Close() }

// writeAll stores a whole file as chunks in one transaction.
func (s *store) writeAll(name string, data []byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := s.fileID(name)
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare("INSERT OR REPLACE INTO chunks(file_id, idx, data) VALUES(?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for idx, off := 0, 0; off < len(data); idx, off = idx+1, off+s.chunkSize {
		end := off + s.chunkSize
		if end > len(data) {
			end = len(data)
		}
		if _, err := stmt.Exec(id, idx, data[off:end]); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("UPDATE files SET size=? WHERE id=?", len(data), id); err != nil {
		return err
	}
	return tx.Commit()
}

// readAt reads n bytes from a byte offset, touching only the
// chunks the range covers. Returns the bytes actually pulled from
// storage so callers can measure amplification.
func (s *store) readAt(name string, off, n int) (result []byte, pulled int, err error) {
	first := off / s.chunkSize
	last := (off + n - 1) / s.chunkSize
	id, err := s.fileID(name)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(
		"SELECT idx, data FROM chunks WHERE file_id=? AND idx BETWEEN ? AND ? ORDER BY idx",
		id, first, last)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	buf := make([]byte, 0, n)
	for rows.Next() {
		var idx int
		var data []byte
		if err := rows.Scan(&idx, &data); err != nil {
			return nil, 0, err
		}
		pulled += len(data)
		chunkStart := idx * s.chunkSize
		lo := 0
		if off > chunkStart {
			lo = off - chunkStart
		}
		hi := len(data)
		if off+n < chunkStart+len(data) {
			hi = off + n - chunkStart
		}
		if lo < hi {
			buf = append(buf, data[lo:hi]...)
		}
	}
	return buf, pulled, rows.Err()
}

// writeAt overwrites a byte range, read-modify-writing only the
// chunks the range touches.
func (s *store) writeAt(name string, off int, p []byte) (pulled int, err error) {
	first := off / s.chunkSize
	last := (off + len(p) - 1) / s.chunkSize

	id, err := s.fileID(name)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	for idx := first; idx <= last; idx++ {
		var data []byte
		err := tx.QueryRow("SELECT data FROM chunks WHERE file_id=? AND idx=?", id, idx).Scan(&data)
		if err == sql.ErrNoRows {
			data = make([]byte, s.chunkSize)
		} else if err != nil {
			return pulled, err
		}
		pulled += len(data)

		chunkStart := idx * s.chunkSize
		lo := 0
		if off > chunkStart {
			lo = off - chunkStart
		}
		srcStart := chunkStart + lo - off
		n := len(data) - lo
		if n > len(p)-srcStart {
			n = len(p) - srcStart
		}
		copy(data[lo:lo+n], p[srcStart:srcStart+n])

		if _, err := tx.Exec("INSERT OR REPLACE INTO chunks(file_id, idx, data) VALUES(?,?,?)",
			id, idx, data); err != nil {
			return pulled, err
		}
	}
	return pulled, tx.Commit()
}

type result struct {
	chunkSize    int
	writeAll     time.Duration
	headerWrite  time.Duration
	recordAppend time.Duration
	indexDescent time.Duration
	tableScan    time.Duration
	dbBytes      int64
	descentPull  int
	headerPull   int
}

func bench(dir string, chunkSize int, payload []byte) (result, error) {
	res := result{chunkSize: chunkSize}
	path := filepath.Join(dir, fmt.Sprintf("bench-%d.db", chunkSize))
	os.Remove(path)

	s, err := newStore(path, chunkSize)
	if err != nil {
		return res, err
	}
	defer s.Close()

	// Initial population. Four files interleaved, mirroring what
	// blipperfs.CreateTable does: DBF, FPT, DBC, CDX written in
	// sequence into the same chunk table. A single-file benchmark
	// cannot show whether that interleaving fragments storage.
	start := time.Now()
	if err := s.writeAll("CUSTOMERS.DBF", payload); err != nil {
		return res, err
	}
	quarter := len(payload) / 4
	for _, sib := range []struct {
		name string
		n    int
	}{
		{"CUSTOMERS.FPT", quarter},
		{"CUSTOMERS.CDX", quarter / 2},
		{"CUSTOMERS.DBC", 8192},
	} {
		if err := s.writeAll(sib.name, payload[:sib.n]); err != nil {
			return res, err
		}
	}
	res.writeAll = time.Since(start)

	// Header rewrite: 32 bytes at offset 0, repeated. This is
	// what Flush does after every append.
	hdr := make([]byte, 32)
	start = time.Now()
	for i := 0; i < 100; i++ {
		pulled, err := s.writeAt("CUSTOMERS.DBF", 0, hdr)
		if err != nil {
			return res, err
		}
		res.headerPull = pulled
	}
	res.headerWrite = time.Since(start) / 100

	// Record append: 80 bytes at the end of the file.
	rec := make([]byte, 80)
	start = time.Now()
	for i := 0; i < 100; i++ {
		off := fileSize - 80 - i*80
		if _, err := s.writeAt("CUSTOMERS.DBF", off, rec); err != nil {
			return res, err
		}
	}
	res.recordAppend = time.Since(start) / 100

	// Index descent: 4 scattered 512-byte reads, the CDX
	// B-tree pattern.
	//
	// Warm up first: the first pass populates SQLite's page
	// cache, and measuring a cold cache once per chunk size
	// produces noise that swamps the effect being measured.
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 200; i++ {
		for node := 0; node < 4; node++ {
			off := rng.Intn(fileSize-512) &^ 511
			if _, _, err := s.readAt("CUSTOMERS.DBF", off, 512); err != nil {
				return res, err
			}
		}
	}
	rng = rand.New(rand.NewSource(42))
	start = time.Now()
	iterations := 2000
	for i := 0; i < iterations; i++ {
		total := 0
		for node := 0; node < 4; node++ {
			off := rng.Intn(fileSize-512) &^ 511
			_, pulled, err := s.readAt("CUSTOMERS.DBF", off, 512)
			if err != nil {
				return res, err
			}
			total += pulled
		}
		res.descentPull = total
	}
	res.indexDescent = time.Since(start) / time.Duration(iterations)

	// Sequential scan of the whole file.
	start = time.Now()
	got, _, err := s.readAt("CUSTOMERS.DBF", 0, fileSize)
	if err != nil {
		return res, err
	}
	res.tableScan = time.Since(start)
	if len(got) != fileSize {
		return res, fmt.Errorf("scan returned %d bytes, want %d", len(got), fileSize)
	}

	// Compact before measuring size, so the numbers reflect
	// steady state rather than WAL and free-page noise.
	if _, err := s.db.Exec("VACUUM"); err != nil {
		return res, err
	}
	s.Close()
	fi, err := os.Stat(path)
	if err != nil {
		return res, err
	}
	res.dbBytes = fi.Size()
	return res, nil
}

func main() {
	dir, err := os.MkdirTemp("", "chunkbench")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	payload := make([]byte, fileSize)
	rand.New(rand.NewSource(7)).Read(payload)

	fmt.Printf("payload: %d MB, SSD-backed ext4, SQLite WAL mode\n\n", fileSize/1024/1024)
	fmt.Printf("%-8s %10s %10s %10s %10s %10s %12s %8s\n",
		"chunk", "writeAll", "hdrWrite", "recAppnd", "descent", "scan", "dbSize", "amplif")
	fmt.Println("---------------------------------------------------------------------------------------------")

	for _, cs := range chunkSizes {
		r, err := bench(dir, cs, payload)
		if err != nil {
			fmt.Printf("%-8s ERROR: %v\n", fmtSize(cs), err)
			continue
		}
		// Amplification on the index descent: bytes pulled from
		// storage per byte of index node actually wanted.
		amp := float64(r.descentPull) / float64(4*512)
		fmt.Printf("%-8s %10s %10s %10s %10s %10s %12s %7.0fx\n",
			fmtSize(r.chunkSize),
			fmtDur(r.writeAll),
			fmtDur(r.headerWrite),
			fmtDur(r.recordAppend),
			fmtDur(r.indexDescent),
			fmtDur(r.tableScan),
			fmtBytes(r.dbBytes),
			amp)
	}
}

func fmtSize(b int) string {
	if b >= 1024*1024 {
		return fmt.Sprintf("%dM", b/1024/1024)
	}
	return fmt.Sprintf("%dK", b/1024)
}

func fmtBytes(b int64) string {
	return fmt.Sprintf("%.1f MB", float64(b)/1024/1024)
}

func fmtDur(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fus", float64(d.Nanoseconds())/1000)
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Nanoseconds())/1e6)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

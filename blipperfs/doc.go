// Package blipperfs is the path-aware session layer over
// blipper's stream-based packages.
//
// # Four levels, each usable on its own
//
// The design intent is autopilot for the common case and manual
// control wherever it is wanted. Nothing here is a wrapper you
// must go through: each level below is a complete entry point,
// and none is a strictly weaker interface than the one beneath
// it.
//
// Level 1 — a directory as a database. One call opens every
// table with its siblings resolved:
//
//	s, err := blipperfs.OpenDir("/data")
//	area, err := s.Select("CUSTOMERS")
//
// Level 2 — a custom backend, with the same automatic
// resolution. This is how a FAT image, a SQLite tablespace, or a
// backend of your own plugs in:
//
//	fs, err := blipperfs.FATImageRW(image, fatfs.WithLongNames(true))
//	s := blipperfs.NewSession(fs)
//	area, err := s.Use("CUST", "CUSTOMERS.DBF")
//
// Level 3 — your own BlipperDB as well, when the session type
// is not what you want:
//
//	db := blipperdb.New()
//	area, err := blipperfs.Use(db, fs, "CUST", "CUSTOMERS.DBF")
//
// Level 4 — no blipperfs at all. Every format package works on a
// bare io.ReadWriteSeeker and knows nothing about filenames:
//
//	tbl, err := dbf.Open(rw)
//	memo, err := dbf.OpenFPT(fptStream)
//	idx, err := cdx.Open(cdxStream)
//
// # What is automatic and what is not
//
// Sibling resolution follows FoxPro's USE: automatic wherever
// the file itself declares the answer, explicit wherever the
// user must choose.
//
//	automatic   memo file       version byte says DBT (0x83) or FPT (0xF5)
//	automatic   DBC catalogue   table-flags bit 2 plus the backlink
//	automatic   structural CDX  conventional stem.CDX when present
//	explicit    free NTX        nothing in the DBF names them
//
// A declared sibling that is missing is corruption and returns
// ErrMissingSibling. An undeclared one that is missing simply is
// not there: no .CDX means no structural index, not an error.
//
// Free NTX indexes are never attached automatically because
// nothing in the DBF names them, and globbing *.NTX would attach
// indexes the caller never asked for. Use blipperdb.Area.SetIndex.
//
// # Backends
//
// Three storage backends implement FileSet, plus MemFileSet for
// tests:
//
//	OSDir(path)                     a directory on the host
//	SQLiteTablespace(path, opts...) chunked blobs in one .db file
//	FATImage / FATImageRW(img)      a FAT16 or FAT32 disk image
//
// They differ in what they can do beyond FileSet, and those
// differences are reachable through named capability interfaces
// rather than by widening FileSet with methods most backends
// would refuse:
//
//	Flusher       commits buffered writes (SQLite, FAT; not OSDir,
//	              where the operating system already provides it)
//	FATBacked     the underlying fatfs.Volume
//	SQLiteBacked  the underlying sqlitefs.FS
//	DirBacked     the root path
//
//	if fl, ok := fs.(blipperfs.Flusher); ok {
//	    err := fl.Flush()
//	}
//
// # Long filenames
//
// Worth stating plainly, because the situation is asymmetric and
// "long names are optional" would misdescribe it:
//
//	OSDir              native, no option — the host filesystem decides
//	SQLiteTablespace   native, no option — names are TEXT, arbitrary UTF-8
//	FATImage           fatfs.WithLongNames(true), off by default
//
// Only FAT carries the 8.3 restriction that long-name support
// exists to lift. In the other two there is no restriction to
// make optional. On FAT it is off by default because xBase
// filenames are 8.3 by construction and enabling it costs about
// 20% on directory load.
//
// # Concurrency
//
// A Session is not safe for concurrent use. For coordination
// between processes, open areas in shared mode and take locks:
// see blipperdb.UseMode, Area.FLock and Area.RLock. Locking
// requires a backend that supports it — OSDir handles implement
// blipperdb.Locker via POSIX record locks; a FAT image has no
// locking mechanism and says so.
package blipperfs

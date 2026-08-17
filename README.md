# blipper

Blipper aims to implement an xBase language written in Go, inspired
by Clipper 5.x, oracle-verified against Clipper 5.2e. It supports a
wide range of DBF file versions and index formats from the major
historical vendors — Ashton-Tate, Borland, Fox Software, Nantucket —
and adds its own DBF tablespace-bundles based on virtual filesystems
(FAT16, FAT32, SQLite).

The language is forthcoming. What exists today is the storage
foundation: the on-disk formats, the work-area session layer a
Clipper-like runtime needs, and three interchangeable places for a
dataset to live.

## What "oracle-verified" means

Every format marked verified below was checked byte-for-byte against
files produced by the original tooling — Clipper 5.2e running under
DOSBox, `mtools` and `mkfs.vfat` for FAT images, the `sqlite3` CLI
for tablespaces. No format is implemented from a specification
alone.

The rule exists because it keeps catching things. The FPT memo
implementation passed every round-trip test while its block
numbering was wrong, because encode and decode were wrong in the
same direction; only comparison against a real Clipper-written file
exposed it. **A round-trip test is not a correctness test.**

## Supported formats

| Format | Support |
| ------ | ------- |
| `.DBF` `0x03`, `0x83` | read/write, verified |
| `.DBF` `0xF5` (FoxPro 2) | read/write, verified |
| `.DBF` `0x30` (Visual FoxPro) | read-only |
| `.DBT` dBASE memo | read/write, verified |
| `.FPT` FoxPro memo | read/write, verified |
| `.NTX` Clipper index | read/write, verified |
| `.NDX` dBASE III+ index | read/write, verified |
| `.CDX` FoxPro compound index | read/write, MACHINE collation |
| `.DBC` VFP catalogue | subset, for long field names |

Fourteen of sixteen field types — `C N L D M F I B Y G T W V
Q` — including the Visual FoxPro binary types `Integer`, `Double`,
`Currency`, `General`, `DateTime`, `Blob`, `Varchar` and
`Varbinary`. Currency keeps its precision as a scaled `int64`
rather than a float, because the documented range needs 63 bits
and a `float64` mantissa carries 53. DateTime's epoch (Julian day
since 4713 BC) and the full `_NullFlags` bit algorithm governing
`Varchar`/`Varbinary` were both confirmed against real or
byte-exact worked VFP 9 data — see `docs/RESEARCH_NOTES.md`.
`Varchar`/`Varbinary` decode is a documented approximation for
content with significant trailing spaces; see T-36.

Note that `B` and `G` mean different things in different lineages:
in Visual FoxPro they are an IEEE double and a binary block
pointer, in dBASE 5.0 both are 10-digit ASCII memo pointers.
blipper implements the VFP meaning and dispatches on the version
byte.

Code pages resolved from header byte 29: all 26 identifiers
Microsoft documents are recognised, 19 of them mapped to an
encoding — including Shift-JIS, GBK, EUC-KR and Big5. The
remaining three have no table in `golang.org/x/text` and are
named but deliberately unmapped, so a file declaring one reports
meaningfully rather than being decoded with a near neighbour.

Not yet implemented: `.IDX` and `.MDX` indexes, dBASE IV and 5.0
table variants, `DateTime`, and `_NullFlags`. See `docs/ROADMAP.md`
for what each needs and why the order is what it is.

## Packages

| Package | Purpose |
| ------- | ------- |
| `dbf` | tables, memo files, field codecs, code pages, PACK |
| `ntx` | Clipper `.NTX` indexes |
| `ndx` | dBASE III+ `.NDX` indexes |
| `cdx` | FoxPro compound indexes |
| `dbc` | Visual FoxPro catalogue subset |
| `blipperdb` | work areas, aliases, shared access, PACK coordination |
| `blipperfs` | the session layer, sibling resolution, storage backends |
| `fatfs` | standalone FAT16/FAT32 driver |
| `sqlitefs` | standalone SQLite tablespace |

`fatfs` and `sqlitefs` import nothing from the rest of the
repository. Their adapters live in `blipperfs`, so either can be
used on its own.

## Four ways in

Each level is a complete entry point. None is a wrapper you must go
through.

```go
// 1. A directory as a database.
s, _ := blipperfs.OpenDir("/data")
area, _ := s.Select("CUSTOMERS")

// 2. Your own backend, same automatic sibling resolution.
fs, _ := blipperfs.FATImageRW(image, fatfs.WithLongNames(true))
s := blipperfs.NewSession(fs)
area, _ := s.Use("CUST", "CUSTOMERS.DBF")

// 3. Your own BlipperDB as well.
area, _ := blipperfs.Use(db, fs, "CUST", "CUSTOMERS.DBF")

// 4. No blipperfs at all — the formats work on bare streams.
tbl, _ := dbf.Open(rw)
```

Sibling resolution follows FoxPro's `USE`: automatic wherever the
file declares the answer, explicit wherever the user must choose. A
memo file is found from the version byte, a catalogue from the table
flags and backlink, a structural `.CDX` from the conventional stem.
Free `.NTX` indexes are never attached automatically, because
nothing in the DBF names them.

## Tablespace bundles

A whole dataset — table, memo, catalogue, indexes — can live in one
container.

```go
fs, _ := blipperfs.SQLiteTablespace("data.db")
s := blipperfs.NewSession(fs)
s.CreateTable("CUST", "CUSTOMERS", spec)
s.Close()   // commits everything atomically
```

Three backends: a plain directory, a FAT16 or FAT32 disk image, or a
SQLite tablespace. Only the last is transactional, which matters
because blipper writes a record, its index entry and its memo block
as three separate stream writes with no commit boundary of their
own. Elsewhere a crash between them leaves an inconsistent set.

FAT images read and write real disk images, with optional VFAT long
filenames. SQLite stores files as chunked blobs; the 32 KB default
chunk size is measured rather than assumed — see `bench/chunksize`.

## Concurrency

Clipper's model, with its vocabulary: `USE ... EXCLUSIVE` versus
`SHARED`, `FLOCK`, `RLOCK`, `UNLOCK`. Exclusive is the default and
the zero value, so callers written before locking existed are
unaffected.

Locks on OS files are POSIX record locks and are real between
processes. One caveat stated plainly: a shared reader does not yet
observe another process's writes, because blipper caches. That is
tracked and not claimed as working.

## Format documentation

Much of what blipper needed is scattered across sources of varying
durability — a Knowledge Base preserved in one volunteer's
repository, a Help file a community mirror hosts, archived vendor
documentation carrying `NOINDEX`. Several are one server away from
disappearing.

So the facts are restated here, in blipper's own words, with
provenance recorded per source:

| Document | Covers |
| -------- | ------ |
| `docs/DBASE_FORMAT.md` | dBASE III+, IV, 5.0 DOS and Windows |
| `docs/VFP30_FORMAT.md` | Visual FoxPro 3.0, field flags, code pages |
| `docs/INDEX_FORMATS.md` | NDX, IDX, CDX, MDX, NTX |
| `docs/CLIPPER_ORACLE.md` | the harness and what it has verified |
| `docs/FAMILY_COMPATIBILITY.md` | support matrix and coverage audit |
| `docs/RESEARCH_NOTES.md` | sources tried, dead ends, open findings |

`docs/ROADMAP.md` records sequencing and what is deliberately not
planned. `STATUS.md` is the working handover.

## Divergences from Clipper

Two, both deliberate and both safer for data: numeric overflow on
encode is an error rather than an asterisk fill, and oversize
character values truncate exactly as Clipper's `REPLACE` does.

Deleted records are kept in indexes, as Clipper does. `PACK`
physically removes them and rebuilds every index that referenced
them.

## Requirements

Go 1.25 or later.

## License

Apache License 2.0. See LICENSE.

Copyright (c) 2026 haitch
h@ual.li · https://oldbytes.space/@haitchfive

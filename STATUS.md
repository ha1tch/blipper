# STATUS

Version: 0.9.25
Last reviewed: 2026-07-23

## State at a glance

| | |
| --- | --- |
| Version | 0.9.19, tag `v0.9.19` |
| HEAD | `3dde616` |
| Tests | 265 passing across 11 packages |
| Build | clean; `gofmt` and `vet` clean |
| Register | 4 open items: T-20, T-27, T-36, T-37 |
| License | Apache 2.0 (relicensed from GPL v3 in v0.9.2) |
| Go | 1.25 in `go.mod`, sandbox runs 1.26.4 |

Eleven packages: `dbf`, `blipperdb`, `blipperfs`, `ntx`, `fatfs`,
`cdx`, `ndx`, `idx`, `mdx`, `sqlitefs`, `dbc` — `idx` and `mdx`
added since this section was last accurate.

Three direct dependencies: `modernc.org/sqlite` (pure Go, no
cgo), `golang.org/x/text` (code pages), `golang.org/x/sys`
(POSIX record locks). Nine indirect.

---

## Build and test, exactly

The environment setup is not optional — a bare `go test` will
fail on the module path.

```sh
export GOROOT=/usr/local/go GOPATH=$HOME/go GONOSUMDB='*'
export PATH=$PATH:$GOROOT/bin
cd /home/claude/blipper
GOFLAGS=-mod=mod go build ./...
GOFLAGS=-mod=mod go test ./... -count=1
```

`GOFLAGS=-mod=mod` is required. Without it the build fails on the
SQLite dependency. For fetching new modules,
`GOPROXY=https://proxy.golang.org,direct` works directly — the
elaborate offline procedure in the working guidelines was not
needed at any point this session.

Test count check:

```sh
GOFLAGS=-mod=mod go test ./... -count=1 -v 2>&1 | grep -cE "^--- PASS"
```

---

## Editing discipline

`repoman` lives at `/tmp/repoman/repoman/` and **will not survive
a session boundary.** Re-clone from
`github.com/ha1tch/repoman`, run `python3 selftest.py` before
first trust, then:

```sh
python3 /tmp/repoman/repoman/ed.py mark <name>      # before any campaign
python3 /tmp/repoman/repoman/ed.py find <term>       # never hand-type an anchor
python3 /tmp/repoman/repoman/ed.py sub --expect N old new path
python3 /tmp/repoman/repoman/register.py list|check|add
python3 /tmp/repoman/repoman/syncver.py check
python3 /tmp/repoman/repoman/guards.py stale
```

Two failure modes hit repeatedly this session, both worth
expecting:

**Stale anchors after `gofmt`.** Running `gofmt -w` realigns
struct fields and constant blocks, so an anchor captured before
formatting will refuse afterwards. Re-run `find` and apply via the
returned handle.

**A refusal is information.** `ed.py` refusing an `--expect 1`
that matches twice is telling you the edit is ambiguous, not that
the tool is being difficult. Re-count or narrow the path.

---

## The rule that shapes everything

**No format is implemented without a way to verify it.**

This has caught real defects three times:

- **v0.4.0, FPT block numbering.** Round-trip tests passed
  because encode and decode were wrong in the same direction.
  Only comparison against a real Clipper-written file exposed it.
- **v0.7.0, the CDX rebuild after PACK.**
- **v0.8.3, the code-page encode path** — verified by checking
  that `Ü` lands as byte `0x9A` under CP850, not merely that it
  round-trips.

The lesson generalised: **a round-trip test is not a correctness
test.** Where a format has an external reference, check against
the bytes it produces.

Two terms used precisely throughout the docs:

- An **oracle** produces files, so it verifies what blipper
  *writes*.
- A **specimen** is a file someone else wrote, so it verifies what
  blipper *reads* and nothing more.

That distinction is roughly the difference between "a week" and
"a fortnight" on any format item, and it should be settled before
sizing one.

---

## Oracles and specimens: what exists, and where

### Everything below `/tmp` is ephemeral and must be rebuilt

| path | what | size |
| ---- | ---- | ---- |
| `/tmp/repoman` | editing toolkit | 384K |
| `/tmp/cdx2/C/CLIPPER5` | **Clipper 5.2e toolchain** | 6.9M |
| `/tmp/ndxprobe`, `/tmp/idxprobe` | harness copies | 7.2M each |
| `/tmp/vfp/cab` | VFP 3.0 extracted | 40M |
| `/tmp/vfpx` | VFPX samples — **mirrored, see below** | 32M |
| `/tmp/db5dos/ex/borland-DBASE5` | dBASE 5.0 DOS, 330 files — **data files now vendored** | 11M |

**The dBASE 5.0 DOS files are now vendored in full** at
`dbf/testdata/dbase5/full/` — 33 DBF, 15 MDX, 1 NDX, 3 DBT, 300
KB. They came from a user upload rather than a URL and cannot be
re-fetched, so the whole set is committed rather than a sample.
The eight files one level up are the working fixtures; `full/` is
an archive nothing references.

### The Clipper oracle, in full

The single most valuable piece of ephemeral state. Rebuilding it
means re-obtaining Clipper 5.2e; the invocation below is verified
and should be copied rather than re-derived.

```sh
SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy \
  timeout 180 dosbox -conf <conf> -exit
```

Two things that cost time to find:

- **The dummy SDL drivers are required.** Without them DOSBox
  hangs waiting for a display.
- **RTLink needs a response file.** `RTLINK.EXE @NAME.LNK` works;
  command-line arguments silently produce no executable.

The RDD is selected by an `RDDSYS.PRG` compiled alongside the
program — substitute the driver name to change format:

```
ANNOUNCE RDDSYS

INIT PROCEDURE RddInit
   REQUEST DBFNDX
   rddSetDefault( "DBFNDX" )
   RETURN
```

All four drivers are present in `CLIPPER5/LIB/`: `DBFNTX`,
`DBFNDX`, `DBFCDX`, `DBFMDX`. Working `.LNK` and `.PRG` examples
are in `/tmp/ndxprobe/C/WORK/`.

### Other oracles

- **`mtools` + `mkfs.vfat`** — FAT16/32 images, both directions
- **`sqlite3` CLI** — SQLite tablespaces
- **`7z`** (`p7zip-full`) — ISO and CAB extraction

---

## Package map: what to touch and what not to

```
dbf/         tables, memo files, field codecs, code pages
  types.go       FieldType, isSupportedType  <- gate for new types
  header.go      version byte acceptance     <- gate for new versions
  schema.go      field width validation
  record_codec.go  encode/decodeRecord
  vfptypes.go    Integer, Double, Currency, General, DateTime,
                 Blob, Varchar, Varbinary — VFP binary/near-binary types
  nullflags.go   _NullFlags bit algorithm, IsNull/IsFull
  dbasetypes.go  dBASE IV/5 B/G lineage sentinels + remap
  memo_dbaseiv.go  dBASE IV/5's own 8-byte-header .DBT format
  codepage.go    26 identifiers, byte 29
  pack.go        PACK + RecordMapping
  memo_compact.go  memo compaction + BlockMapping

ntx/ ndx/ cdx/ idx/ mdx/   index formats, independent of each
                            other; idx reuses cdx's compact-leaf
                            codec (cdx.WriteLeaf, exported for it)
dbc/             VFP catalogue subset (long field names)

blipperdb/       work areas, aliases, locking, PACK coordination
blipperfs/       FileSet, Session, sibling resolution, 3 backends
fatfs/           FAT16/32 driver — imports nothing from blipper
sqlitefs/        SQLite tablespace — imports nothing from blipper
```

**Two packages are deliberately blipper-free.** `fatfs` and
`sqlitefs` import nothing from the rest of the repository, and
their adapters live in `blipperfs`. This is checked by grep and
should stay true — it is what lets either be used independently.

**Four layered entry points**, all working and all tested by
`TestAllFourAccessLevelsWork`:

```go
s, _ := blipperfs.OpenDir("/data")           // 1: directory as database
s := blipperfs.NewSession(customFileSet)     // 2: your backend
area, _ := blipperfs.Use(db, fs, ...)        // 3: your BlipperDB too
tbl, _ := dbf.Open(rw)                       // 4: no blipperfs at all
```

---

## Traps that will bite

Ordered by how easily they are stepped on.

### 1. The same type letter means different things

**dBASE `B` and `G` are 10-digit ASCII memo pointers. Visual
FoxPro's `B` is an 8-byte IEEE double and its `G` a 4-byte binary
pointer.**

**Solved, v0.9.21 (T-31).** Two internal-only sentinel `FieldType`
values, `DBaseBinary`/`DBaseGeneral` (lowercase `'b'`/`'g'`, never
a real on-disk byte), which `readField` remaps to when the
table's version byte indicates the dBASE lineage
(`isDBaseLineage`, `dbf/header.go`) and `writeField` unmaps
unconditionally on the way out. See `dbf/dbasetypes.go`. Still
worth understanding if touching either type's decode/encode path:
getting the remap direction wrong silently decodes a dBASE `B`
field as a VFP float parsed from ASCII digits, or vice versa.

### 2. Byte 28 means three different things

| lineage | byte 28 |
| ------- | ------- |
| dBASE III+ | reserved |
| dBASE IV / 5 | production MDX flag |
| FoxPro / VFP | table flags (`0x01` CDX, `0x02` memo, `0x04` DBC) |

blipper already writes `0x0C` there for DBC pairing. Any code
reading byte 28 must dispatch on the version byte first.

### 3. Numeric index keys are encoded differently per format

**NDX stores plain IEEE-754 doubles. CDX transforms them** —
byte-reversed, then all bits inverted if negative or only the top
bit if not — so that byte comparison yields numeric order.

Carrying the CDX assumption into NDX corrupts **negatives only**,
and byte comparison agrees often enough to survive casual
testing. `ndx/ndx_test.go` has a test demonstrating it with −100
against 100.

### 4. Byte 29 numbering is not shared across lineages

dBASE 5.0 specimens carry `0x1B`, which is outside the VFP code
page table entirely. blipper maps byte 29 through the VFP table
only, so such a file reports as an unsupported code page rather
than being misdecoded — the safe failure, but incomplete. No
dBASE language-driver table has been found.

### 5. Memo fields have two legal widths

10-byte ASCII in dBASE and FoxPro 2; 4-byte binary in VFP.
`Schema.validate` accepts both. Not spelled out in any format
document — found by comparing specimens.

### 6. dBASE IV/5 field-descriptor byte 31 does not mean what it
looks like it means

Documented everywhere as "this field has a tag in the production
`.MDX`," and that's what every source said until 2026-07-24. A
live write-oracle test (real dBASE 5.0 for DOS) falsified it
directly: a field indexed via `INDEX ON ... TAG` after table
creation has a genuinely working tag — confirmed by decoding the
`.MDX` tag directory itself — while byte 31 stays `0x00`.
**Determine tag membership from the `.MDX` tag directory, never
from byte 31.** See `docs/DBASE_FORMAT.md`'s dBASE IV section for
the full correction and every place it propagated to.

### 7. dBASE IV/5 `.DBT` blocks have a header dBASE III+ never had

III+ memo blocks are headerless — raw text, terminated by
`0x1A 0x1A`. dBASE IV/5 blocks (found via the same write-oracle
test) carry an 8-byte header: a constant 4-byte marker, then a
4-byte little-endian length field that is **header-inclusive** (8
+ text length), not content-only like FPT's. Decoding it as a
plain content-length field — the FPT convention — is silently
wrong by exactly 8 bytes. Verified against two blocks of
different lengths, exact match both times.

---

## Open items, with what each actually needs

### T-31 — dBASE IV/5 tables — done (v0.9.21)

Full read support shipped. Version bytes `0x8B`/`0x43`/`0x63`
accepted via `isDBaseLineage` (`dbf/header.go`), the `B`/`G`
lineage trap solved via `DBaseBinary`/`DBaseGeneral` sentinels
(`dbf/dbasetypes.go`), byte 31 correctly not relied on for tag
membership, and a new `.DBT` memo reader for dBASE IV/5's own
8-byte-header format (`dbf/memo_dbaseiv.go`). Verified against all
33 vendored specimens, the live write-oracle, and real 1994
purchase-history data including a memo spanning a block boundary.
11 new tests, all passing. See `docs/RESOLVED.md`.

One thing found and fixed along the way, not originally scoped:
every dBASE 5.0 specimen carries code page byte `0x1B`, which
blipper had no entry for — `dbf.Open()` passed every structural
check and then hard-failed on the codec lookup. Added
`CodePageDBaseIVUnknown` (`dbf/codepage.go`), a narrow identity
carve-out specifically for `0x1B`, not a general "unknown = pass
through" change.

Write support was deliberately scoped out — filed as **T-37**,
fully specified, roughly half a day, not blocked on anything.

### T-27 — `.cpg` sidecar encoding · P2 · ~1.5 days

**The highest-value item on the list**, because it is a live
correctness issue rather than a missing feature.

Four independent GIS producers write byte 29 = `0x00` alongside a
`.cpg` file naming UTF-8. blipper is correct *by accident* — it
passes bytes through, and Go strings are UTF-8. The accident
breaks on the override path: `SetCodePage(CodePageIntl850)` over a
mixed corpus would mangle every GIS export.

Design decided and recorded: an `Encoding` type with four-way
resolution — explicit override, `.cpg` sidecar, header byte 29,
identity. **`CodePageUTF8` was rejected** and should not be
revisited: byte 29 cannot express UTF-8, so inventing an
identifier would make `Table.CodePage()` report something no real
file declares.

Sidecar detection belongs in `blipperfs`, not `dbf` — `dbf`
operates on bare streams and knows nothing of filenames, a
separation that has held all session.

Fixtures identified and decisive: Malta's `Ċ` (U+010A) and `Ħ`
(U+0126) exist in **no** single-byte encoding, so a wrong guess
fails loudly rather than producing plausible rubbish.

### T-20 — cache invalidation for shared access · P2 · unsized

v0.8.2 enforces the locking protocol and the locks are real
between processes. What it does **not** do is make a shared
reader observe another process's writes, because
`Table.recordCount`, `cdx` nodes, `dbc` rows and the `fatfs` FAT
are all cached with nothing invalidating them.

Unsized deliberately. Wants a survey of how many cached fields
exist before an estimate.

### T-37 — dBASE IV/5 memo write support · ~half a day

Fully specified, not blocked on anything. `CreateDBaseIVMemo`
(fresh block-0 header — table name, block size, next-free
pointer, all self-describing per `dbf/memo_dbaseiv.go`'s doc
comment) and `(*DBaseIVMemoFile).Append` (marker + header-inclusive
length + text), mirroring `dbf/memo.go`'s existing `CreateMemo`/
`Append` for dBASE III+. Multi-block continuation is verified
(T-31's `TestDBaseIVMemoRealSpecimenMultiBlock`), so `Append` can
follow the same block-spanning approach with confidence.

Also needed for a complete write path: a test confirming
`DBaseBinary`/`DBaseGeneral` field values encode correctly (the
code path already exists, reusing Memo's plain-string convention
— likely already correct, just untested in the write direction);
and writing a correct `0x8B` version byte plus a populated `.DBT`
header at table-creation time.

---

## Documentation map

| file | what it is for |
| ---- | -------------- |
| `docs/ROADMAP.md` | sequencing and rationale; also what is **not** planned, with reasons |
| `docs/TRACKING.md` | open items only |
| `docs/RESOLVED.md` | closed items, full text as at closure |
| `docs/RESEARCH_NOTES.md` | sources tried and exhausted, verified tool invocations, findings not yet acted on |
| `docs/DBASE_FORMAT.md` | dBASE III+/IV/5.0 layouts, from Borland TI838D |
| `docs/VFP30_FORMAT.md` | Visual FoxPro, five sources with durability assessments |
| `docs/INDEX_FORMATS.md` | NDX, IDX, CDX, MDX, NTX layouts |
| `docs/FAMILY_COMPATIBILITY.md` | support matrix, coverage audit, comparable libraries |
| `docs/CLIPPER_ORACLE.md` | the harness and what it has verified |

**Every format document carries a provenance section**, and
several sources are one volunteer's server away from vanishing.
That is why the facts are restated rather than linked.

---

## Release procedure

Patch increments were the standing instruction this session.

1. `ed.py mark before-release-X`
2. Close items: move detail **verbatim** to the top of
   `docs/RESOLVED.md` with version and date, delete from
   `TRACKING.md`, cross-reference the changelog
3. `echo X > VERSION`, then bump `Version:` in all seven
   release-coupled documents
4. `syncver.py check`
5. Write the CHANGELOG entry
6. Gate: no `✓` in TRACKING · RESOLVED top entry correct ·
   versions synced · `register.py check` · `guards.py stale` ·
   tests green · `gofmt`/`vet` clean · no `(pending)` refs
7. Commit, `git tag -a vX`, bundle to `/mnt/user-data/outputs/`

**Verify the test count before writing it into the CHANGELOG.**
It was wrong once this session and caught at the gate.

---

## Things not to redo

Recorded so they are not re-attempted.

**Shapefiles cannot yield a VFP specimen.** Four independent
producers sampled, all `0x03`, because the shapefile specification
mandates dBASE III+ compatibility.

**The dBASE 5.0 *Windows* media is not extractable.** Samples sit
inside Borland's `DS\0Z` container; `7z`, `lhasa`, `unar`,
`cabextract` and Auriemma's `borpak` were all tried. The last
targets a different Borland format — it looks for magic `PACK`
where this carries `DS\0Z`. Remaining route is `INSTALL.EXE` under
Windows 3.1 emulation.

**FoxBASE+ 2.10's runtime is serialised** and was not pursued. It
also blocks nothing: FoxBASE+ writes `0x03`/`0x83` tables and
`.DBT` memos, both already supported, and `.IDX` has a working
compact-format oracle in `DBFCDX.LIB` — though not for the plain
uncompressed layout, which `DBFCDX` never emits.

**FoxBASE+ writes `.DBT`, not `.FPT`.** This was asserted wrongly
mid-session and reverted. `.FPT` is FoxPro 2.x and later, and
version byte `0x83` *means* dBASE III+ with `.DBT` by definition.

**`_NullFlags` and `DateTime` cannot be verified from the VFP 3.0
media.** Byte 18 is `0x00` in all 136 files across both editions.
Specimens are located, and not committed. In the mirror at
`github.com/ha1tch/VPFX-Samples`:

- **`Northwind/`** — eight tables with `_NullFlags`, 57 nullable
  fields between them. `employees.dbf` has the most at 14. The
  bitmap width tracks the nullable count: 1 byte up to 8 columns,
  2 beyond.
- **`Solution/Europa/photos.dbf`** — the only DateTime field
  anywhere in the corpus, column `CREATED`.

**Mirrored at `github.com/ha1tch/VPFX-Samples`** (note the
spelling: the fork name transposes to `VPFX` where the upstream
organisation is `VFPX`). So availability is settled; **licensing
is not**. Upstream `github.com/VFPX/Samples` carries no LICENSE
and its tables are Microsoft's Northwind samples.

blipper's position: extract the *facts* into
`docs/VFP30_FORMAT.md` and generate any fixture against those,
rather than copying files of unclear provenance into the
repository at all. See `docs/RESEARCH_NOTES.md`.

Note: the project was GPL v3 for most of the session and was
relicensed to Apache 2.0 in v0.9.2. Some discussion recorded in
the docs predates that. The provenance argument never depended on
which licence applied.

---

## Suggested next action

**T-31 is done (v0.9.21).** The only remaining pieces of the
Ashton-Tate/Borland table lineage now open are T-37 (dBASE IV/5
memo write support, fully specified, roughly half a day) and
whatever the plain/uncompressed `.IDX` question turns into once
tried.

No single item stands out as clearly next the way T-31 did.
Reasonable candidates, roughly in order of how bounded they are:

- **T-37** — smallest, fully specified, mirrors an existing
  pattern (`dbf/memo.go`'s `CreateMemo`/`Append`) rather than
  needing new design.
- **T-27** (`.cpg` sidecar) — P2, a live correctness issue rather
  than a missing feature, fixtures already identified and decisive.
- **T-20** (cache invalidation) — P2 but deliberately unsized;
  wants a survey of cached fields before an estimate exists.
- **Plain/uncompressed `.IDX`** — not a register item on its own,
  folded into T-29's residual scope. Two named candidates now
  (FoxBASE+ 2.10, blocked on serialisation this project won't help
  circumvent; FoxPro 2.6 DOS, no known obstacle), reachable now
  that real DOSBox access has proven able to get past what headless
  automation couldn't. Depends on the user having a session free
  for it, not on anything blipper-side.

## A verification attempted and not completed

`github.com/diskfs/go-diskfs` was fetched and exercised on
2026-07-24 as a candidate for a raw-image or VMDK storage
backend. **The round-trip did not work**: a sequence of `Create`,
`Partition`, `CreateFilesystem`, write, reopen reported success
at every step and produced an empty image — no partition entries
in the MBR, no FAT boot sector at the partition offset.

**This is not evidence against the library.** The API's partition
indexing turned out to be zero-based where the first attempt
assumed one-based, which suggests unfamiliarity rather than a
defect, and there may be a commit or flush step that was missed.
The library is actively maintained and widely used.

Recorded so that a later attempt starts from a known failing case
rather than from scratch. The reproduction is at
`/tmp/dfscheck/main.go`, which will not survive the session — but
the sequence above is enough to reconstruct it. Nothing has been
filed and no dependency was added.

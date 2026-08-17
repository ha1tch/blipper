# xBase family compatibility

Updated: 2026-07-24

Cross-family reference for the DBF-lineage products, focused on what
matters when reading or writing files with blipper. This is a
decision aid, not a history.

## Legend

Every fact is marked for source and confidence:

- **V** — verified locally against a real file, either by the oracle
  (`docs/CLIPPER_ORACLE.md`) or by decoding a corpus sample.
- **D** — documented in a first-party or established source: Microsoft
  Learn's archived VFP pages, the dBASE 7 specification hosted by the
  Library of Congress, or the Hacker's Guide to Visual FoxPro.
- **?** — reported by multiple sources but not verified here.
- **—** — not applicable or not present.

Where **V** and **D** disagree, **V** wins in this document.

## Tables

Version byte, memo file, and native index format:

| Product / era | Version byte | Memo | Native index | Field name max | blipper |
| ------------- | ------------ | ---- | ------------ | -------------- | ------- |
| dBASE III / III+ (1984–88) | `0x03` V, `0x83` w/memo V | `.DBT` V | `.NDX` V | 10 D | ✓¹ |
| dBASE IV (1988) | `0x03` V, `0x8B` w/memo V | `.DBT` V | `.MDX` (multi-tag) V\* | 10 V | ✓⁶ read+write |
| dBASE 5 (1994) | `0x03`/`0x8B`/`0x43`/`0x63` V | `.DBT` V | `.MDX` V\*, `.NDX` V | 10 V | ✓⁶ read+write |
| dBASE 7 / Level 7 (1997) | `0x04` D | `.DBT` D | `.MDX` D | **32** D | ✗ |
| FoxBASE (1984) | `0x02` D, `0xFB` w/memo D | `.DBT`-shaped D | — | 10 D | ✗ |
| FoxBASE+ (1986–89) | `0x03` V, `0x83` w/memo V | `.DBT` V | `.IDX` D | 10 D | ✓² |
| FoxPro 2.x (1991–94) | `0x03` V, `0xF5` w/memo V | `.FPT` V | `.CDX` (compact, compound) V, `.IDX` compact V | 10 D | ✓³ |
| Visual FoxPro 3.0 (1995) | `0x30` V | `.FPT` D | `.CDX` D | **128** D (in DBC), 10 D (free) | partial⁴ |
| Visual FoxPro 6–9 | `0x30`/`0x31` D, `0x32` V | `.FPT` D | `.CDX` D | 128 D / 10 D | partial⁴ |
| Clipper 5.2e (1995) | writes `0x03`/`0x83` V | `.DBT` V | `.NTX` V default, also writes `.CDX` V, `.NDX` V, `.MDX` D | 10 D | ✓⁵ |

Nulls, long names, new field types:

| Product / era | Nulls | Long names in-file | New field types | blipper |
| ------------- | ----- | ------------------ | --------------- | ------- |
| dBASE III / III+ | no D | no D | — | ✓¹ |
| dBASE IV / 5 | no D | no D | Float (`F`) V, `0x8B`/`0x43`/`0x63` versions ✓ | ✓⁶ read+write |
| dBASE 7 | no D | **yes, 32 chars in field descriptor** D | Timestamp (`@`, Julian-day epoch **stated**), Long (`I`), Autoincrement (`+`), Double (`O`) D | ✗ |
| FoxBASE / FoxBASE+ | no D | no D | — | ✗ |
| FoxPro 2.x | no D | no D | as dBASE IV D | ✗ |
| Visual FoxPro 3.0+ | via hidden `_NullFlags` field **V**, full bit algorithm **V** | **no** — long names live in `.DBC` sidecar, DBF stores 10-char shortname D | Currency (`Y`) V, DateTime (`T`) V, Integer (`I`) V, Double (`B`) V, General/OLE (`G`) V, Blob (`W`) V, Varchar (`V`) V\*, Varbinary (`Q`) V\* | partial⁴ |
| Clipper 5.2e | no D | no D | — | ✓ |

## blipper support column

- **✓** — reads and writes correctly, verified end-to-end.
- **R** — reads correctly, no write path (unused so far).
- **partial** — opens the file but with known limits or defects.
- **✗** — not supported; the version byte, index, or field type is rejected on open.

Footnotes:

¹ Reads and writes `0x03` and `0x83`, verified byte-for-byte against Clipper 5.2e (T-01, T-02, T-06 at v0.2.0). The two defects that once held this at `partial` are closed: **T-07** (duplicate field names rejected on `Open`, which Clipper tolerates) and **T-08** (header year decoded as `1900+y` where Clipper writes mod 100) both shipped in v0.4.5, taking corpus coverage to 137/137 files openable. `.NDX` indexes landed in v0.9.0 (**T-28**), read and write, verified against `DBFNDX`-generated fixtures for both character and numeric keys.

² FoxBASE+ 2.x was built for compatibility with dBASE III PLUS, and wrote `0x03`/`0x83` tables and dBASE-style `.DBT` memo files accordingly. Both are implemented and verified against Clipper 5.2e via `DBFNTX.LIB`, so FoxBASE+ tables and memos are covered today.

**`.FPT` postdates FoxBASE+.** That format belongs to FoxPro 2.x and later, and is signalled by version byte `0xF5`; FoxBASE+ 2.10 shipped in 1988, FoxPro 1.0 in 1989, FoxPro 2.0 in 1991. A `0x83` file's memo sibling is a `.DBT` by definition — the version byte says so.

The one outstanding FoxBASE+-era format was `.IDX` — compact-format support shipped in v0.9.3 (**T-29**), oracle-verified via `DBFCDX.LIB`. What that oracle does *not* cover is FoxBASE+'s own native uncompressed layout, which predates the compact scheme entirely; `DBFCDX` only ever emits compact output. The FoxBASE+ runtime is serialised and will not run without branding — this project has deliberately not pursued that and will not help circumvent it — but that bears only on the uncompressed variant, not on the compact support already shipped.

³ FoxPro 2.x tables (`0x03`, `0xF5` when memo-bearing) open through `dbf.Open` and their `.FPT` memos read and write via `dbf.OpenFPT`/`dbf.CreateFPT`, oracle-verified byte-for-byte against a Clipper 5.2e-generated fixture (T-12 closed in v0.4.0). `.CDX` indexes were closed under T-09 (v0.3.0). `blipperdb.Area` does not yet integrate memo files for either FoxPro or Clipper tables — callers use the `dbf` package directly for memo access; `blipperdb` memo integration is tracked as **T-13**.

⁴ Visual FoxPro tables (`0x30`, `0x31`, `0x32`) are accepted on `Open` and readable — the version-byte gate was widened to all three 2026-07-24, when a real `0x32` specimen (`photos.dbf`) was needed to verify DateTime decoding. Fourteen of sixteen field types decode, including `V`/`Q`/`W` (Varchar, Varbinary, Blob) and `_NullFlags`, whose complete bit algorithm — including the previously-unknown interaction with non-nullable Varchar/Varbinary fields — was confirmed 2026-07-24 against a byte-exact worked example (T-35). `V`/`Q` content decode is exact, including values legitimately ending in significant trailing spaces (**T-36**, closed v0.9.23). What is not supported: **writing** `0x30`/`0x31`/`0x32` at all (a deliberate choice — see T-10), and `P` Picture. See `docs/VFP30_FORMAT.md`.

T-24 established that specimens **are** obtainable: Microsoft's own VFP 3.0 installation media on the Internet Archive yields 129 files with version byte `0x30`, and the format is now consolidated in `docs/VFP30_FORMAT.md` with provenance for each claim. Read support for `0x30` plus `Currency`, `Integer`, `Double` and `General` is tracked as **T-25**.

Two details remain gated on specimens that would have to be *created* rather than found. `_NullFlags` is documented (last field, system-flagged, one bit per nullable column) but byte 18 is `0x00` in all 129 vendor files, so nothing exercises it. `DateTime` is documented as two four-byte integers but its epoch is not, and no specimen carries the type — a failure mode of dates wrong by one day passes inspection and corrupts records quietly. **T-33** checks whether VFP's epoch matches dBASE 7's stated one (Julian day since 4713 BC), which if true would resolve this without a VFP specimen at all.

Worth recording that shapefiles were ruled out as a VFP source conclusively: four independent producers (GADM, ArcGIS, Landsat Missions, and the Clipper corpus) all write `0x03`, because the shapefile specification effectively mandates dBASE III+ compatibility.

Note also that T-10 deliberately writes `0x03` with byte 28 set rather than claiming `0x30`: reading a VFP table is safe, but *writing* that version byte is a promise to honour VFP field types and null semantics, and should not be made before they exist.

⁶ dBASE IV (`0x8B` with memo) and dBASE 5 for DOS (`0x03`, `0x8B`, `0x43` SQL table, `0x63` SQL system) tables are accepted by `dbf.Open` and read correctly as of v0.9.21 (**T-31**), oracle-verified against all 33 vendored specimens plus a live write-oracle (real dBASE 5.0 for DOS, S13) — including the `B`/`G` lineage dispatch and dBASE IV/5's own 8-byte-header `.DBT` memo format (`dbf/memo_dbaseiv.go`), verified against real 1994 data spanning a multi-block memo. **Memo file write support shipped in v0.9.23 (T-37)** — `CreateDBaseIVMemo`/`Append` mirror `dbf/memo.go`'s existing dBASE III+ pattern, verified internally including a multi-block write, though not tested against real dBASE 5.0 re-opening a blipper-written file. **Table write support shipped in v0.9.25 (T-38)** — `CreateDBaseIV(rw, schema, kind)` covers all three version bytes explicitly, for both dBASE IV 2.0 and dBASE 5.0 (the two products share this format byte-for-byte, confirmed directly in T-31), verified with a full round trip including a `B`-type field. `.NDX` and `.MDX` indexes over these tables were already separately verified (T-28, T-30).

⁵ CDX read/write shipped in v0.3.0 (T-09 closed) via the `cdx` package, oracle-verified against `DBFCDX.LIB`. `blipperdb.Area` attaches CDX tags for ordered traversal. Clipper 5.2e can also be linked via `DBFMDX.LIB` and `DBFNDX.LIB`, but those RDDs are not exercised here.

## What blipper can validate right now

Four oracles are in use, and what each covers is worth
distinguishing, because "documented" and "verified" are different
kinds of confidence.

**Clipper 5.2e under DOSBox** — the primary oracle, covering:

- Tables `0x03` and `0x83` via `DBFNTX.LIB` **V**
- Tables `0xF5` via `DBFCDX.LIB` **V**
- `.DBT` memo files, dBASE III+ shape **V**
- `.FPT` memo files via `DBFCDX.LIB`, 64-byte default block **V**
- `.NTX` indexes **V**
- `.NDX` indexes via `DBFNDX.LIB`, both character and numeric
  keys **V** (v0.9.0)
- `.CDX` indexes via `DBFCDX.LIB`, MACHINE collation only **V**
- `.IDX` — compact-format read/write via `DBFCDX.LIB` **V**
  (v0.9.3, T-29). Plain/uncompressed layout unimplemented; no
  generator has been found for it — `DBFCDX` only ever emits
  compact output.

**mtools and mkfs.vfat** — FAT16 and FAT32 images, both
directions: images blipper writes are read back by `mdir`, and
images `mcopy` wrote are read by blipper **V**

**The `sqlite3` CLI** — SQLite tablespaces, verified by querying
the schema blipper produced **V**

**Microsoft's VFP 3.0 distribution media** is a specimen source
rather than an oracle, and the distinction matters: 136
vendor-written files support *read* verification, but verifying
what blipper *writes* would need VFP itself. That is why `0x30`
is read-only and writing it stays out of scope.

**Real dBASE 5.0 for DOS, run by the user under DOSBox** — a
genuine fourth oracle, not just a specimen source, added
2026-07-24 (source S13). Generated `dbf/testdata/dbase5/oracle/`
with predictions stated and checked before looking: version byte,
byte 28, byte 29 confirmed exactly as expected; field-descriptor
byte 31 falsified directly (a real `.MDX` tag existed, byte 31
stayed `0x00`); the `.DBT` 8-byte block header found and verified
here, not documented anywhere prior. This is what made `T-31`'s
`B`/`G` dispatch, byte-31 correction, and the new memo format
oracle-verified rather than merely documented.

**Decisive-by-construction text, not a vendor oracle**, for
`.cpg` encoding resolution (T-27, v0.9.23): the trigger
investigation found real GIS shapefiles (Malta, Kosovo) containing
characters — `Ċ`, `Ħ`, `Đ`, plus Cyrillic in the same table — that
exist in *no* single-byte DOS or Windows encoding at all. A
resolution test built on that fact can't pass by accident the way
a Latin-alphabet test could; a wrong tier winning produces visibly
mangled bytes, not plausible-looking wrong text.

Stale as of the version where this section was last corrected
(2026-07-24, against v0.9.21): `DateTime` and `_NullFlags` were
listed here as documented-but-unverified. Both are now oracle-
verified (T-33 against a real VFP 9 specimen, T-35 against a
byte-exact worked example) — corrected in the same pass that adds
the dBASE 5.0 oracle above. `dBASE IV .MDX` was also listed here
and is likewise now verified (T-30, cross-checked again by T-31's
live write-oracle).

What remains genuinely documented (**D**) but not verifiable here:
dBASE 7's long-field-name format (zero specimens found anywhere
this session) and any CDX file with a collation identifier other
than MACHINE (no source found describing the collation tables at
all).

## Coverage audit

Originally audited against the code at v0.9.0, and left stale
through several closures since — corrected 2026-07-24, and
re-audited the same day against v0.9.23 after T-27/T-36/T-37
closed and T-20 partially closed. The question a reader most often
has is not "what works" but "what does not", so the gaps are
listed with what each would cost.

### Version bytes — 9 of roughly 16 named accepted

**This section originally read "Audited against the code at
v0.9.0" and was never updated through v0.9.21 — six version bytes
this table listed ✗ shipped since.** Corrected 2026-07-24.

`dbf.Open` accepts `0x03`, `0x83`, `0xF5`, `0x30`, `0x31`, `0x32`,
`0x8B`, `0x43`, `0x63`.

| byte | product | status |
| ---- | ------- | ------ |
| `0x03` | dBASE III+ / FoxPro 2 / Clipper, no memo | ✓ |
| `0x83` | dBASE III+ / FoxBASE+ with memo | ✓ |
| `0xF5` | FoxPro 2.x with `.FPT` | ✓ |
| `0x30` | Visual FoxPro | ✓ |
| `0x31` | VFP, autoincrement enabled | ✓ (v0.9.13) |
| `0x32` | VFP, Varchar/Varbinary/Blob | ✓ (v0.9.13) |
| `0x8B` | dBASE IV/5.0 with memo | ✓ (v0.9.21, T-31) |
| `0x43` | dBASE IV SQL table | ✓ (v0.9.21, T-31) |
| `0x63` | dBASE IV SQL system file | ✓ (v0.9.21, T-31) |
| `0x04` | dBASE 7 | ✗ — 31-character field names, different descriptor, zero specimens found |
| `0x05` | dBASE V, no memo | ✗ |
| `0x02`, `0xFB` | FoxBASE / dBASE II | ✗ — 16-bit record count, different header; not planned |
| `0x8E`, `0xCB` | claimed elsewhere as dBASE IV SQL variants | ✗ — no specimen or corroborating source found for these two specifically; `0x43`/`0x63` were the ones with real specimens and are now closed |

Read support for all nine now-accepted bytes is complete. Write
support for the *table itself* — `dbf.Create` choosing a version
byte — is not: writing `0x30`/`0x31`/`0x32` is a deliberate
permanent exclusion (T-10 — a promise about VFP semantics blipper
does not fully honour), and `dbf.Create` has no path to `0x8B`/
`0x43`/`0x63` either, fully specified but not attempted.

Distinct from that: dBASE IV/5's own **memo file** format is
writable as of v0.9.23 (T-37) — `CreateDBaseIVMemo`/`Append`,
standalone functions a caller can use directly, verified
internally including a multi-block write. That is not the same
thing as `dbf.Create` producing a fresh `0x8B`-versioned table;
the two are independent pieces of work, and only the memo half
has shipped.

### Field types — 14 of 16

**Also stale from the same v0.9.0 audit — corrected 2026-07-24.**
Five rows below were marked ✗ and have shipped since (T-33
through T-36).

| code | type | status |
| ---- | ---- | ------ |
| `C` | Character | ✓ |
| `N` | Numeric | ✓ |
| `L` | Logical | ✓ |
| `D` | Date | ✓ |
| `M` | Memo | ✓ 10-byte ASCII, 4-byte binary, and (v0.9.21) dBASE IV/5's own 8-byte-header `.DBT` pointer convention |
| `F` | Float | ✓ |
| `I` | Integer | ✓ |
| `B` | Double | ✓ |
| `Y` | Currency | ✓ scaled `int64`, precision preserved |
| `G` | General / OLE | ✓ block pointer; payload not interpreted |
| `T` | DateTime | ✓ (v0.9.13, T-33) — epoch confirmed against a real VFP 9 specimen |
| `V` | Varchar (VFP 9) | ✓ (v0.9.16, T-35) — exact content decode, including significant trailing spaces (**T-36**, closed v0.9.23) |
| `Q` | Varbinary (VFP 9) | ✓ (v0.9.16, T-35) — same caveat as `V` |
| `W` | Blob (VFP 9) | ✓ (v0.9.14, T-34) — `General`'s exact pointer mechanism |
| `0` | `_NullFlags` system column | ✓ (v0.9.16, T-35) — complete bit algorithm, `Record.IsNull`/`IsFull` |
| `P` | Picture | ✗ — the one genuine remaining gap in this table |

`_NullFlags`'s bit algorithm needed a correction mid-session:
`_NullFlags` bit ordering across fields, including a previously
undocumented interaction with non-nullable Varchar/Varbinary
fields, was confirmed against a byte-exact worked example rather
than inferred.

### Memo formats — complete for three lineages

`.DBT` (two physically different layouts) and `.FPT`, read and
write, with compaction — though compaction and write support
apply to the original two lineages; the third, below, is read-only.

The `M` field itself has three on-disk representations, not two:
10-byte ASCII pointer in dBASE III+ and FoxPro 2; 4-byte binary
pointer in Visual FoxPro; and dBASE IV/5.0's own variant (v0.9.21,
T-31), which reuses the same 10-byte ASCII pointer convention but
whose `.DBT` blocks carry an 8-byte header dBASE III+'s never
had — a constant marker plus a header-inclusive length field.
None of this is spelled out in any format documentation found
this session; both distinctions were found by comparing vendor
specimens, the second one via a live write-oracle.

### Indexes — 5 of 5

| format | product | status |
| ------ | ------- | ------ |
| `.NTX` | Clipper | ✓ read/write, oracle-verified |
| `.NDX` | dBASE III+ | ✓ read/write, oracle-verified |
| `.CDX` | FoxPro compound | ✓ read/write, MACHINE collation only, numeric keys |
| `.IDX` | FoxPro 2 single-key | ✓ read/write, compact format only, numeric keys — plain/uncompressed unimplemented |
| `.MDX` | dBASE IV multi-tag | ✓ read/write, Character/Date/Numeric — Numeric bounded to 4 significant digits |

Every index format blipper set out to support now has a working
implementation. The one open question left is `.IDX`'s
plain/uncompressed layout — `DBFCDX`, the only generator found,
only ever produces compact-format output. Two untried candidates
exist for it: FoxBASE+ 2.10 (blocked on serialisation this project
won't help circumvent) and FoxPro 2.6 DOS (no known obstacle),
both newly reachable now that real interactive DOSBox access has
proven able to get past what headless automation couldn't. See
`docs/INDEX_FORMATS.md`.

### What no comparable library implements

`.NTX` and `.NDX` appear in no other Go library, and only
`DbfDataReader` (.NET) reads `.CDX` — read and search, without
writing. That matters because Clipper installations are the ones
with no vendor, no migration path, and a retiring maintainer
population.

## Open register items bearing on coverage

**T-27, T-36 and T-37 all closed in v0.9.23** — a direct request
("close what can be closed with the knowledge we already have")
found all three fully specified from prior sessions, needing
implementation rather than research. See `docs/RESOLVED.md`.

- **T-20** cache invalidation for shared access — partially
  closed in the same pass. `dbf.Table.Reload` ships (v0.9.23),
  re-reading `RecordCount` after seeking to start, verified with
  two independent `*Table` instances over one stream standing in
  for two processes. Checked before implementing: no reload
  mechanism of any kind existed anywhere in the codebase, a bigger
  finding than the item's earlier "invalidate four caches" framing
  suggested. `cdx`, `dbc` and `fatfs` each cache their own state
  independently and still need their own version of the same
  pattern — see `docs/TRACKING.md`.

## Comparable libraries

First surveyed 2026-07-23, refreshed 2026-07-24 after blipper's
own IDX/MDX/numeric-codec work landed and a wider search turned up
libraries in more languages than Go and .NET. Blipper is not the
only xBase library, and it is worth being precise about where it
overlaps with the others and where it does not.

| | blipper | [go-dbase] | [go-foxpro-dbf] | [DbfDataReader] | [CodeBase] |
|---|---|---|---|---|---|
| Language | Go | Go | Go | .NET | C (DLL/FFI) |
| Read `.DBF` | ✓ | ✓ | ✓ | ✓ | ✓ |
| Write `.DBF` | ✓ | ✓ | ✗ | ✗ | ✓ |
| Read `.DBT` (dBASE memo) | ✓ | ✓ | ✗ | ✓ | ✓ |
| Write `.DBT` | ✓ both shapes — dBASE III+/FoxPro 2, and dBASE IV/5's own 8-byte-header shape (T-37, v0.9.23) | ✗ | ✗ | ✗ | ✓ |
| Read `.FPT` (FoxPro memo) | ✓ | ✓ | ✓ | ✓ | ✓ |
| Write `.FPT` | ✓ | ✓ | ✗ | ✗ | ✓ |
| `.NTX` (Clipper index) | ✓ read/write | ✗ | ✗ | ✗ | build-from-source only |
| `.NDX` (dBASE III+ index) | ✓ read/write | ✗ | ✗ | ✗ | build-from-source only |
| `.CDX` (compound index) | ✓ read/write, numeric keys | ✗ | ✗ | ✓ read/search, MACHINE only | ✓ read/write |
| `.IDX` (FoxPro 2 index) | ✓ compact, numeric keys | ✗ | ✗ | ✗ | ✓ read/write |
| `.MDX` (dBASE IV/5 index) | ✓ Character/Date/Numeric\* | ✗ | ✗ | ✗ | build-from-source only |
| `.DBC` (long names) | ✓ subset | ✓ | ✗ | ✗ | ✗ (explicitly unsupported) |
| Field types | 14 of 16 | full VFP set | — | — | full VFP set + custom |
| PACK with index rebuild | ✓ | ✗ | ✗ | ✗ | ✓ |
| Memo compaction | ✓ | ✗ | ✗ | ✗ | ✓ |
| Character encodings | 28 named, 19 mapped, plus four-way `.cpg`/override resolution (T-27) | 13+ | extensible | ✓ | ✓ |
| Struct / JSON / map conversion | ✗ | ✓ | ✓ | ✓ (LINQ/Dapper-style) | via wrapper |
| Record locking | ✓ | exclusive only | ✗ | ✗ | ✓, multi-user client/server |
| Storage backends | 3 (dir, FAT, SQLite) | filesystem | filesystem | filesystem | filesystem, 2GB/terabyte modes |
| Table relations, transactions | ✗ | ✗ | ✗ | ✗ | ✓ |
| Oracle-verified against period tooling | ✓ | ✗ | ✗ | ✗ | — (was the period tooling) |
| Maintained by | one session | active, 74+ releases | — | active | volunteers; original devs retired |
| License | Apache 2.0 | — | — | — | LGPL-3.0 |

[go-dbase]: https://github.com/valentin-kaiser/go-dbase
[go-foxpro-dbf]: https://github.com/SebastiaanKlippert/go-foxpro-dbf
[DbfDataReader]: https://github.com/yellowfeather/DbfDataReader
[CodeBase]: https://github.com/MPSystemsServices/CodeBase-for-DBF

\* MDX numeric keys are bounded to 4 significant digits — see T-32's
resolution in `docs/RESOLVED.md`.

### CodeBase deserves separate treatment

The other three are hobby-scale readers and writers. CodeBase is
neither hobby-scale nor new: it is **Sequiter Inc.'s commercial
xBase engine**, the product underneath a real generation of
production Clipper/FoxPro applications, released to open source
under LGPL-3.0 in 2018 after Sequiter's business moved on. Every
original developer has since retired; the repository is now
maintained by a third party (M-P Systems Services) working from
what Sequiter handed over, which is precisely the "vendor gone,
volunteers holding the line" pattern this whole project's research
has kept running into.

It is a genuinely different kind of tool: table relations across
files, cross-table transactions, a client/server mode with its own
admin console, and a "Large" model breaking VFP's 2GB table limit.
None of that is in scope for what blipper is.

**Where it's ahead of blipper:** full VFP field-type coverage
including custom self-incrementing integers, multi-user
client/server architecture, and — notably — its distributed DLLs
support **both CDX and IDX** for Visual FoxPro compatibility,
which is more than any Go or .NET library in this table manages
for IDX.

**Where blipper is ahead, concretely:** the DLLs distributed in the
repository do **not** include Clipper NDX or dBASE IV MDX support —
the maintainers state it requires recompiling from source with
different `#DEFINE`s, and note they don't have the expertise to do
that recompilation themselves. So NDX and MDX read/write, working
in blipper's shipped tests today, are not something you can get
from CodeBase without doing that build yourself first. And CodeBase
explicitly does not support the DBC database-container concept at
all — blipper's subset, small as it is, covers ground CodeBase
never did.

### Where blipper differs from the rest of the field

**Indexes generally.** `go-dbase` and `go-foxpro-dbf` do not read
indexes at all. `DbfDataReader` has grown real query sophistication
since last checked — SQL-style and LINQ-style queries that use a
sidecar `.cdx` automatically for equality, range, and `ORDER BY` —
but it remains CDX-only, MACHINE-collation-only, and read-only.
Nothing outside blipper and CodeBase (once rebuilt) touches NTX,
which is Clipper's own format.

**Verification method.** The other Go/.NET libraries are written
from published specifications. Blipper is checked byte-for-byte
against Clipper 5.2e running under DOSBox, `mkfs.vfat`/`mtools`
for FAT images, and the `sqlite3` CLI for tablespaces — and where
no oracle existed (MDX's numeric encoding), against ground truth
recovered directly from vendor-written specimens. That method has
repeatedly caught things a specification reading did not: the FPT
block-numbering bug in v0.4.0 passed every round-trip test because
both halves were wrong in the same direction.

**Storage abstraction.** Blipper separates the format packages
from where bytes live, so a dataset can sit in a directory, a
FAT16/FAT32 disk image, or a SQLite tablespace behind the same
API. The Go/.NET libraries read from the filesystem; CodeBase adds
its own large-table modes but is still filesystem-based.

### Where blipper is behind

**Convenience conversions.** `go-dbase` maps rows to Go structs,
JSON, and maps directly; `DbfDataReader` supports typed LINQ-style
queries. Blipper returns values and leaves the mapping to the
caller.

**Field type coverage.** `go-dbase` and CodeBase both support the
full VFP type set. Blipper has 14 of 16 field types — the dBASE
III+ set, VFP's binary types (Integer, Double, Currency, General,
DateTime, Blob), and VFP 9's Varchar/Varbinary — and reads but
does not write `0x30`/`0x31`/`0x32` VFP tables (T-10, a deliberate
permanent exclusion). dBASE IV/5 tables (`0x8B`/`0x43`/`0x63`)
write as of v0.9.25 (T-38, `CreateDBaseIV`) — the asymmetry with
VFP is real, not an oversight: VFP's write exclusion is a promise
not yet earned about null semantics blipper doesn't fully honour,
while nothing analogous ever applied to the dBASE IV/5 lineage.

**Maturity.** `go-dbase` has 74+ releases and external
contributors. CodeBase has two decades of commercial production
use behind it. Blipper is one session old.

### The shared caveat

`go-dbase`'s own README carries a disclaimer worth repeating
here, because it applies equally to blipper:

> This library is designed for working with **existing** dBase
> files and legacy system integration. While it supports creating
> new tables, it should not be used to develop new applications
> that rely on dBase as the primary database format.

The use case is migration, integration, and reading data that
already exists — not greenfield storage.

### Why any of this still matters

Enlyft counted more than 5,400 companies still running Visual
FoxPro in 2025, concentrated in small and mid-sized businesses.
Microsoft ended support in 2015; VFP now runs only on 32-bit
Windows compatibility layers with no vendor patches. FoxPro
DevCon last met in 2009, and the developers who built these
systems are retiring.

The Clipper lineage is in the same position but with less
commercial attention: Harbour and xHarbour remain actively
maintained and roughly 100% backward compatible with CA-Clipper
5.2e — the exact version this project uses as its oracle.

## Format references in this repository

| document | covers |
| -------- | ------ |
| `docs/DBASE_FORMAT.md` | dBASE III PLUS, IV, 5.0 DOS and Windows — headers, field descriptors, `.DBT` memo behaviour, and where byte 28 diverges across the family |
| `docs/VFP30_FORMAT.md` | Visual FoxPro 3.0 — header, field flags byte 18, `_NullFlags`, field types, the byte-29 code page table |
| `docs/CLIPPER_ORACLE.md` | the Clipper 5.2e harness and what it has verified byte-for-byte |
| `docs/INDEX_FORMATS.md` | NDX, IDX, CDX, MDX and NTX index layouts, with the compact-key encoding |
| `docs/RESEARCH_NOTES.md` | sources tried and exhausted, tool invocations, findings not yet acted on |
| `docs/DBASE_HISTORY.md` | the origin story — Vulcan, JPL, Ashton-Tate, 1975–1984 |

Each carries its own provenance section. Several of the upstream
sources are one volunteer's server away from disappearing, which
is why the facts are restated here rather than merely linked.

## Sources

- `docs/CLIPPER_ORACLE.md §5.1` — Microsoft Learn spec URLs, the
  Hacker's Guide chapter, the Q130461 header structure article, and
  the Clipper 5.x Drivers Guide chapter 4 (`DBFCDX.LIB` handling
  both CDX and FPT).
- Corpus at `github.com/ha1tch/clipper` for verified `0x03`, `0x83`,
  `.DBT`, `.NTX` samples.
- Committed oracle fixtures: `cdx/testdata/CDATA.{DBF,CDX}` for CDX
  (T-09) and `dbf/testdata/FDATA.{DBF,FPT}` for FPT (T-12). Provenance
  documented in the sibling `*.README.md` files.

# Research notes: sources, dead ends, and unfiled findings

Version: 0.9.25
Last reviewed: 2026-07-23

## Why this document exists

Priming a fresh session on this project is laborious. Much of what
was learned on 2026-07-23 lives in the register, the changelog, or
the two format references — but a residue does not, and that
residue is the expensive part to rediscover: which sources were
tried and failed, which tools work in the container and how, and
which findings are established but not yet acted on.

This file is that residue. It is deliberately not a design
document; nothing here constrains implementation. It exists so
that a later reader does not repeat a search that has already been
exhausted.

---

## Working environment, verified

Facts about the container that took time to establish and would
take the same time again.

### DOSBox oracle harness

Installed and working. The Clipper 5.2e toolchain is staged at
`/tmp/cdx2/C/CLIPPER5/` (ephemeral — `/tmp` does not survive) with
a proven invocation:

    SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy \
      timeout 180 dosbox -conf <conf> -exit

Two things that cost time to discover:

- **The dummy SDL drivers are required.** Without them DOSBox
  either fails or hangs waiting for a display.
- **RTLink needs a response file, not command-line arguments.**
  `RTLINK.EXE @NAME.LNK` works; passing `FI name LIB ...` on the
  command line silently produces no executable and an empty log.

The RDD is selected by an `RDDSYS.PRG` compiled alongside the
program:

    ANNOUNCE RDDSYS
    INIT PROCEDURE RddInit
       REQUEST DBFCDX
       rddSetDefault( "DBFCDX" )
       RETURN

Substituting `DBFNDX`, `DBFMDX`, or `DBFNTX` selects those
drivers. All four libraries are present in `CLIPPER5/LIB/`.

### Other tools

| tool | package | used for |
| ---- | ------- | -------- |
| `7z` | `p7zip-full` | ISO and CAB extraction |
| `mtools` | `mtools` | FAT12 floppy images (`fatfs` excludes FAT12) |
| `mkfs.vfat` | `dosfstools` | generating FAT test images |
| `dosbox` | `dosbox` | the Clipper oracle |

`bsdtar` also available via `libarchive-tools`.

### Go module fetching

`GOPROXY=https://proxy.golang.org,direct` with `GOFLAGS=-mod=mod`
works directly for `go get`; the elaborate offline procedure in
the working guidelines was not needed this session.

---

## Sources: what was tried, and what came of it

### Productive

| source | what it gave |
| ------ | ------------ |
| `archive.org/download/ms-vfp30/` Standard ISO | 129 VFP `0x30` specimens, `30DBC.DBF` |
| the same, Professional ISO | sample DBCs with byte 28 = `0x07`; confirmed no nullable/DateTime anywhere |
| `vfphelp.com/help/_5wn12pc0x.htm` | field-descriptor byte 18 in full — the decisive find |
| `vfphelp.com/help/_5WN12P5S3.htm` | the authoritative 26-entry code page table |
| `learn.microsoft.com/.../ww305zh2(v=vs.80)` | field type sizes and ranges |
| `raw.githubusercontent.com/jeffpar/kbarchive/` | KB Q130461, the header structure |
| Borland TI838D | the dBASE III+/IV/5.0 layouts — see `DBASE_FORMAT.md` |
| Bachmann, *Xbase File Format Description* | NDX, IDX, CDX, MDX and NTX layouts — see `INDEX_FORMATS.md` |
| GADM, USGS, Landsat shapefiles | real-world DBF at scale; `.cpg` sidecar evidence |

### Exhausted — do not retry

**Shapefiles as a VFP source.** Four independent producers were
sampled — GADM, ArcGIS via the USGS geothermal group, Landsat
Missions 2018, and the Clipper corpus — and every one writes
`0x03`. The reason is structural: the shapefile specification
effectively mandates dBASE III+ compatibility, so no conforming
writer emits `0x30` whatever software produced it. Sampling more
countries or agencies will not change this.

**The Clipper corpus for anything but Clipper.** All 120 files
scanned: 117 × `0x03`, 3 × `0x83`. Nothing else.

**Running the FoxBASE+ 2.10 runtime.** The four floppy images at
`archive.org/download/fox-base-v-2.10-runtime-dbms` are genuine
and extractable (FAT12; use `mtools`), and `FOXPLUS.EXE` runs
under DOSBox. But it refuses with "This program has not been
properly installed" because `BRAND.EXE` must write a serial into
the executable first, and `INSTALL.BAT` aborts to `BADSERIAL` if
that step fails. The install *is* the copy protection; they are
not separable. **Not pursued.**

**This blocks nothing.** FoxBASE+ was built for dBASE III PLUS
compatibility, so it writes `0x03`/`0x83` tables and `.DBT`
memos, both of which blipper already reads, writes, and has
verified against Clipper 5.2e. The only FoxBASE+-era format
outstanding is `.IDX`, and `DBFCDX.LIB` produces those — see
finding 2. The disks were worth fetching for the reconnaissance,
not for an oracle that turned out to be already in hand.

**A correction worth recording**, because it was made and then
reverted within the hour. The compatibility table briefly claimed
FoxBASE+ wrote `.FPT`, on a misreading of "Fox-style memos" as
meaning the FoxPro format. It does not: `.FPT` belongs to FoxPro
2.x and later and is signalled by version byte `0xF5`. FoxBASE+
2.10 shipped in 1988, FoxPro 1.0 in 1989, FoxPro 2.0 in 1991 — so
`.FPT` postdates FoxBASE+ entirely. The version byte settles it
without needing a specimen: `0x83` *means* dBASE III+ with
`.DBT`.

The FoxBASE+ distribution examined carries no memo file at all,
so it offers no local evidence either way. The reasoning above is
from the version-byte semantics and the product chronology.

**GitHub API for bulk listing.** Rate-limited to 60
requests/hour unauthenticated and exhausted quickly. Use
`codeload.github.com/<owner>/<repo>/tar.gz/refs/heads/master`
instead, which does not consume the API quota. `raw.githubusercontent.com`
also works for individual files where the HTML interface is
robots-blocked.

### Unresolved licensing

**`github.com/VFPX/Samples`**, part of the VFPX organisation at
`github.com/VFPX`. Contains 301 VFP files including
7 × `0x31`, 1 × `0x32`, **57 nullable fields across 8 tables with
real `_NullFlags` columns**, and one DateTime field
(`photos.dbf`/`CREATED`). These are Northwind sample tables — a
*later* Microsoft product than the VFP 3.0 media, so genuinely
different files.

No LICENSE file at the repository root; a 92-byte README pointing
at "the Samples topic in VFP help" — Microsoft's documentation for
Microsoft's bundled samples. The tables are Northwind
(`employees`, `suppliers`, `orders`, `customers`), which is a
*later* Microsoft product than the VFP 3.0 media, so these are
genuinely different files rather than a duplicate of what the
installation ISO already provides.

VFPX as an organisation hosts genuinely open-source VFP tools,
most carrying explicit licences. This repository does not, which
is the finding rather than an oversight to work around.

**Position taken:** do not vendor into blipper, whose fixtures
should have clean provenance regardless of licence. Forking
under a separate namespace as a research mirror is a different
act and was judged acceptable, with the caveat that hosting a
Microsoft-derived archive on Microsoft infrastructure has its own
risk — an independent copy alongside is cheap insurance.

**Mirror created 2026-07-23: `github.com/ha1tch/VPFX-Samples`.**
Note the spelling — the fork name transposes to `VPFX` where the
upstream organisation is `VFPX`. Recorded exactly as it exists so
it can be found; searching for `VFPX-Samples` under that
namespace will not resolve.

**Where the specimens actually are**, verified 2026-07-23. They
sit in two directories, not one:

`Northwind/` — eight tables carrying `_NullFlags`, all with the
system column last, field type byte `0x00`, byte 18 `0x05`:

| file | version | nullable fields | `_NullFlags` width |
| ---- | ------- | --------------- | ------------------ |
| `employees.dbf` | `0x31` | 14 | 2 |
| `orders.dbf` | `0x31` | 13 | 2 |
| `suppliers.dbf` | `0x31` | 10 | 2 |
| `customers.dbf` | `0x30` | 9 | 2 |
| `products.dbf` | `0x31` | 7 | 1 |
| `categories.dbf` | `0x31` | 2 | 1 |
| `shippers.dbf` | `0x31` | 1 | 1 |
| `customerdemographics.dbf` | `0x30` | 1 | 1 |

The width tracking the nullable count — 1 byte up to 8 columns, 2
beyond — is directly visible across that set, which is the
clearest evidence available for how the bitmap is sized.

`Solution/Europa/photos.dbf` — the **only** DateTime field in the
entire corpus, column `CREATED`. Earlier notes in this document
implied it was among the Northwind tables; it is not.

So the specimens are no longer at risk of disappearing with the
upstream repository. What remains unresolved is the licensing,
not the availability, and blipper's position is unchanged: the
*facts* extracted from those files belong in
`docs/VFP30_FORMAT.md`, and any fixture blipper needs should be
generated against those documented facts rather than copied.

**The durable move regardless:** extract the *facts* from those
files and document them. Byte layouts are not copyrightable. That
extraction has not yet been done — see below.

---

## Findings established but not yet acted on

### 0. MDX numeric key encoding — cracked and shipped (T-32, v0.9.11)

Not pending — recorded here because the method is reusable. dBASE
stores `Numeric` DBF fields as plain ASCII text, so a field's
ground-truth value is directly readable next to any index built
over it. Cross-referencing every numeric-tagged `.MDX` specimen
(`CODES.MDX`/`AREACODE`, 39 records; `ACCT_REC.MDX`/`OLDBALANCE`,
5 records including negative and zero) against its paired `.DBF`
gave 44 known (value, key-bytes) pairs — enough to derive the
encoding by inspection rather than guesswork. Full derivation in
`mdx/numeric.go`; the encoding is a third, distinct scheme from
both NDX's plain IEEE double and CDX/IDX's transformed one.

The same method — find a specimen where the human-readable
ground truth sits next to the encoded bytes — is worth trying
first on any future undocumented encoding before assuming a
specimen search or a document search is the only path.

### 1. `_NullFlags` observed in real files

From the VFPX Samples scan. Eight tables carry a `_NullFlags`
column with these properties:

- **field type byte is `0x00`** — not any documented letter
- **byte 18 = `0x05`** — system (`0x01`) plus binary (`0x04`)
- **width 1 or 2 bytes**, scaling with the nullable column count
- appears as the **last field**, consistent with the Help's
  description

One field, `employees.dbf`/`PHOTO`, carries **byte 18 = `0x06`** —
nullable plus binary — which is the combination the VFP 9 Help
calls out explicitly and which no other observed file exercises.

**Not yet established:** bit ordering within the bitmap, and
behaviour when columns are added or dropped. Those need either the
files in hand or a VFP installation.

### 2. IDX is reachable through Clipper — for compact format only

**Verified 2026-07-23, then corrected the same day** (see below) —
recorded with both stages intact because the initial overclaim is
exactly the kind of mistake worth being visible about. `DBFCDX.LIB`
emits `.IDX` files for `INDEX ON <field> TO <name>`, but — as
established later in this same entry — only ever in *compact*
format. Whether Clipper (or anything else found this session) can
produce the plain/uncompressed layout remains open; see T-29 and
`docs/ROADMAP.md`, corrected 2026-07-24 after this same overclaim
was found copied forward there without the correction attached.
A probe produced a 2048-byte `BYCODE.IDX` from a four-record table.

Header observations from that file, decoded but **not fully
established**:

| offset | value seen | probable meaning |
| ------ | ---------- | ---------------- |
| 0–3 | 1024 | root page pointer |
| 4–7 | 0 | free page pointer |
| 12–13 | 10 | key length (matched the `C(10)` field) |
| 14 | `0x20` | index options |

Pages are 512 bytes. **Keys are stored packed backward from the
page end** — in the probe, `DELTA` at 1514, `CHARLIE` at 1519,
`BRAVO` at 1526, `ALPHA` at 1531, i.e. descending byte order for
ascending key order. That is the compact-index pattern, the same
shape as CDX, and unsurprising given both are FoxPro-lineage.

**The key expression field appeared empty**, which differs from
NTX where it is stored as text. Worth confirming.

The node layout was **not** established — an initial guess at
`recno`/`page`/`key` triples decoded to nonsense.

**Resolved later the same day.** The `0x20` in the index-options
byte means *compact index format*, so the file is laid out the
CDX way rather than the plain-IDX way: keys grow backward from
the page end with duplicates and trailing blanks elided, and the
record number, duplicate count and trailing count are bit-packed
at the start of the entry area. The backward key order was the
format working correctly, not a decode error. See
`docs/INDEX_FORMATS.md`; the open question is now whether
`DBFCDX` can produce an *uncompressed* IDX at all.

### 3. Memo fields have two legal widths

Acted on in v0.8.6, recorded here because it is the kind of thing
a specification does not spell out: dBASE III+ and FoxPro 2 store
the memo block number as a 10-byte right-aligned ASCII string;
Visual FoxPro stores a 4-byte little-endian integer. Confirmed
across the vendor specimens.

### 4. The same type letter means different things across lineages

dBASE 5.0's `B` (Binary) and `G` (General/OLE) are 10-digit ASCII
`.DBT` pointers. VFP's `B` is an 8-byte IEEE double and its `G` is
a 4-byte binary block pointer. **Dispatching on the type code
alone is wrong**; the version byte must be consulted. blipper
does this correctly but nothing documented why until now.

### 5. `.cpg` sidecars are the real encoding channel for GIS data

Four independent producers write `0x03` with header byte 29 =
`0x00` and a `.cpg` file containing `UTF-8`. blipper reads byte 29
and knows nothing of `.cpg`, so it applies the identity codec —
correct by accident, since Go strings are UTF-8, but wrong in
principle. A caller applying `SetCodePage(CodePageIntl850)` across
a mixed corpus would mangle these files.

Specimens confirming this, with the useful property that their
text is **unrepresentable in any single-byte encoding**:

- Malta: `Ċentrali`, `Ħal Balzan` — `Ċ` (U+010A) and `Ħ` (U+0126)
  exist in no DOS or Windows code page
- Kosovo: `Đakovica`, `Dečani`, plus Cyrillic `Ораховац` in the
  same table

Those make a decisive test rather than a suggestive one: a wrong
guess fails loudly rather than producing plausible rubbish.

**Not filed.** The design discussed was an `Encoding` type with
four-way resolution — explicit override, `.cpg` sidecar, header
byte 29, identity — with UTF-8 arriving as a sidecar value rather
than a fabricated code-page byte. `CodePageUTF8` was rejected:
byte 29 cannot express UTF-8, so inventing an identifier would
make `Table.CodePage()` report something no real file declares.
Sidecar detection belongs in `blipperfs` alongside memo/DBC/CDX
resolution, not in `dbf`, which knows nothing of filenames.

### 6. T-08's year pivot is now evidence-backed above byte 99

The Y2K pivot shipped in v0.4.5 was tested against Clipper files
with year bytes 91–98 and *reasoned* about the rest. Four modern
files exercise the ≥100 range: Landsat WRS-1 (byte 118 → 2018),
USGS faults and two GADM files (byte 122 → 2022). All decode
correctly against their known origin dates.

### 7. Production-scale read evidence

Before today the largest fixture was 1,017 bytes. blipper now has
been run against:

| file | records | result |
| ---- | ------- | ------ |
| USGS Great Basin faults | 257,521 | 0 errors, 168k rec/s |
| Landsat WRS-1 ascending | 31,509 | 0 errors, 233k rec/s |

289,030 records total, zero errors. Neither file is committed —
152 MB and 8 MB respectively — but both are re-fetchable.

---

### 8. dBASE 5.0 for DOS specimens obtained

Borland dBASE 5.0 for DOS (1994), plain files with no container.
33 DBF across four version bytes, 15 MDX, 1 NDX, 3 DBT. Eight
staged at `dbf/testdata/dbase5/`.

This unblocked two register items in one go — T-30 (MDX) had a
documented layout and no evidence, and T-31 (dBASE IV/5 tables)
did not exist as an item because the versions looked
unverifiable.

**The Windows 5.0 media is a dead end by contrast.** Its samples
sit inside Borland's `DS\0Z` installer container. The member
directory is readable — 50 names with uncompressed sizes,
including 8 DBF, 6 MDX and 2 DBT — but the payloads are
LZ-compressed at about 4.6:1 and no available extractor opens
them: `7z`, `lhasa`, `unar`, `cabextract`, and Luigi Auriemma's
`borpak` were all tried. The last of those targets a different
Borland format entirely; it looks for magic `PACK` where this file
carries `DS\0Z`. The remaining route is `INSTALL.EXE` under
emulation, which needs Windows 3.1 rather than plain DOSBox.

Recorded so the decompressor search is not repeated.

### 9. Byte 29 numbering is not shared across lineages

The dBASE 5.0 DOS specimens carry `0x1B` in byte 29, which is
outside the Visual FoxPro code page table entirely — that runs
`0x01` to `0xCB` with no `0x1B`. So the dBASE IV lineage numbers
its language drivers differently, and byte 29 cannot be read with
one table family-wide.

blipper maps byte 29 through the VFP table only, so such a file
is reported as an unsupported code page rather than misdecoded.
That is the safe failure, but the mapping is incomplete and no
dBASE language-driver table has been found.

`docs/DBASE_FORMAT.md` previously implied the numbering was
consistent; corrected.

### 10. Version byte 0x63 has no primary-source description

Eleven files in the dBASE 5.0 DOS distribution carry it. Borland's
TI838D does not mention it at all. The only account found is
Bachmann's, listing it as dBASE IV SQL system files.

Specimens exist, so the format is establishable by observation —
but a version byte whose only description is secondary is worth
flagging rather than treating as settled.

## Index formats: current position

All three remaining index formats have a working oracle. None is
blocked.

| format | driver | status |
| ------ | ------ | ------ |
| `.NDX` | `DBFNDX.LIB` | available, unused |
| `.MDX` | `DBFMDX.LIB` | available, unused |
| `.IDX` | `DBFCDX.LIB` | **verified working** — see finding 2 |

`.NDX` is the simplest and the most directly useful for Clipper
corpora. An independent third-party description of the NDX layout
was also found to corroborate blipper's existing NTX
implementation — 1024-byte pages, key expression in the header,
unique flag — which is worth noting as independent agreement with
an oracle-derived implementation.

---

## Method notes worth preserving

**Round-trip tests are not correctness tests.** The FPT
block-numbering bug in v0.4.0 passed every round-trip because
encode and decode were wrong in the same direction. Only
comparison against a real Clipper-written file exposed it. This
recurred as a principle three times today: the CDX rebuild, the
code page encode path (verified by checking `Ü` lands as `0x9A`
under CP850, not merely that it round-trips), and the VFP Currency
decoder (verified against `CUSTOMER.DBF`, not merely round-tripped).

**A specimen is not an oracle.** A found file proves what a real
writer produced and supports read verification. Verifying what
blipper *writes* needs the producing software. That distinction
was the difference between "a week" and "a fortnight" in the VFP
scoping, and it should be settled before any format item is sized.

**Documentation runs out before the format does.** VFP is well
documented up to `_NullFlags` and DateTime, at which point every
source goes quiet simultaneously. That pattern — thorough on the
common path, silent on the awkward corner — is worth expecting
rather than being surprised by.

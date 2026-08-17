# Visual FoxPro 3.0 format reference

Version: 0.9.25
Last reviewed: 2026-07-23

## Why this document exists

The information below is scattered across four sources of varying
durability: a Microsoft Knowledge Base article preserved only in a
community archive, a Help file that Microsoft placed under Creative
Commons and that a volunteer project now hosts, an archived MSDN page
carrying `NOINDEX,NOFOLLOW`, and a 1995 installation ISO on the
Internet Archive.

None of those is guaranteed to outlive the format. The KB archive is
one person's GitHub repository. `vfphelp.com` is a community mirror
that rate-limits and could stop being paid for. Microsoft's archived
documentation is explicitly marked `is_archived: true` and has already
migrated hosts at least twice. This file consolidates what was found,
with enough provenance that a reader can judge each claim and, if a
source has vanished, know what it said.

Assembled 2026-07-23 while investigating T-24 (whether VFP 3.0
support is verifiable). Nothing here is implemented in blipper yet;
this is the reference such an implementation would be built against.

---

## Sources, with provenance

### S10 — "What's New in VFP 9", Hentzenwerke Publishing, Chapter
9 "New Data and Index Types" (retrieved from a foxcentral.net
mirror, 2026-07-24)

- A full book chapter, not a reference page — includes a
  self-contained worked example: a synthetic table (`Field1 C(1)`,
  `Field2 V(1)` not null, `Field3 C(1)` null, `Field4 V(1)` null),
  seven inserted records, with the exact raw record bytes and the
  resulting `_NullFlags` value given for every one. This is the
  source that fully solved the bit-allocation algorithm — see
  `dbf/nullflags.go` and T-35's resolution in `docs/RESOLVED.md`.
  Verified by reproducing all seven records independently and
  matching every byte, not merely trusted.
- **Corrects S9.** S9's `SET EXACT` discussion states VarBinary
  values are "padded with CHR(0)" for comparison purposes. The
  previous pass through this document (v0.9.15) read that as a
  storage-layout fact and stated the on-disk padding byte was
  `0x00`. It is not. S10 states directly, and its worked hex bytes
  confirm: *"Varchar and Varbinary fields actually are padded with
  spaces in the DBF file."* S9's `CHR(0)` statement describes
  runtime comparison semantics — how a shorter expression is
  conceptually zero-padded when compared against a longer one —
  not physical storage. Both facts are true; the earlier pass
  applied one of them to the wrong layer.
- **Gives the complete `_NullFlags` bit algorithm**, stated
  directly in prose and confirmed by the worked example: every
  Varchar/Varbinary field allocates a "full" bit regardless of
  nullability, every nullable field (any type) allocates a "null"
  bit, and a field with both gets both, full-bit first, adjacent.
  A null value sets both bits. This is not new information layered
  onto S7 — S7 only ever described the two-bit case for a field
  that is both nullable and Varchar/Varbinary; S10 is the first
  source establishing that a *non-nullable* Varchar/Varbinary
  field also consumes a bit, which shifts the position of every
  field declared after it.
- Also confirms independently: version byte `0x32` for a table
  containing any of the three VFP 9 types (matching what blipper
  already reads via `dbf/testdata/vfp/PHOTOS.DBF`); the 254/255
  character length limit for Varchar in a table; and that Blob
  storage in the FPT "doesn't affect the DBF structure... using
  the same organization as normal Memo fields" — a third
  independent confirmation of the General/Blob mechanism, after
  the direct FPT decode and S9's design-intent statement.

### S9 — CODE Magazine, "What's New with Data in Visual FoxPro
9?" by David Anderson, CODE Focus Magazine 2004 Vol. 2 Issue 1,
published 2004-09-23

- Contemporary trade press, published alongside VFP 9's actual
  release — not archived Microsoft documentation but a working
  developer's account written at the time. Durability: fair; a
  commercial magazine site rather than an archive, but the
  publisher (EPS Software / CODE Magazine) is still operating and
  the article carries no dead-link risk found so far.
- **Fills a specific gap S7 left open.** S7 established that a
  nullable `V`/`Q` field's actual content length is stored in the
  field's own last byte when the varlength bit is set, but never
  stated what fills the unused tail bytes. S9, describing `SET
  EXACT` comparison behaviour, states directly: *"with VarBinary
  types whose values are padded with CHR(0), the trailing bytes
  are ignored for the comparison."* **The padding byte is `0x00`
  (NUL), not `0x20` (space).** This distinguishes `V`/`Q` from
  `C`/`M`, which pad with spaces — consistent with S9's separate
  statement that VarChar values are "not padded with spaces to
  the length of the field," which otherwise reads ambiguously
  about whether the field is padded with *anything*.
- **`V` and `Q` share one storage mechanism**, differing only in
  a decode-layer concern rather than a byte-layout one: *"The
  only real difference between the two is that FoxPro does not
  perform any code page translation for VarBinary types."* So an
  eventual implementation can share one codec for both, gated by
  a code-page-translation flag rather than needing two byte
  layouts.
- **Independent confirmation that `W` (Blob) is `General`'s
  storage mechanism**, matching what decoding `photos.fpt`
  already proved directly: *"BLOBs have the same limitations and
  issues as Memo fields... The BLOB datatype is an ideal candidate
  to replace the legacy General field."* This is design intent
  from a contemporary source agreeing with the byte-level finding,
  not new information on its own — recorded because corroboration
  from an independent source is worth more than either alone.
- **The long-name/single-letter table matches blipper's constants
  exactly**: `V` Varchar, `Q` Varbinary, `W` Blob, `T` Datetime,
  `I` Integer, `B` Double, `Y` Currency, `G` General. No
  corrections needed there.
- **A genuinely new, unimplemented CDX variant surfaced**: `INDEX
  ON <expr> TAG <name> BINARY`. A specialised bitmap-style index
  restricted to `NOT NULL` logical expressions (its own worked
  example is `INDEX ON DELETED() TAG DELETED BINARY`), roughly 30×
  smaller than an ordinary index over the same expression, but
  unusable with `SEEK` and excluded from `SET ORDER TO`. Not
  investigated further this session — single-purpose, VFP 9-only,
  and no specimen or oracle exists for it. Noted here so it is not
  mistaken for CDX's standard compact format if encountered later.

### S8 — Microsoft Learn archive, VFP 7.0-era (`v=vs.71`),
"Table File Structure" and "Table Structures of Table Files"

- `learn.microsoft.com/.../st4a0s68(v=vs.71)` and
  `.../72es52cd(v=vs.71)`. Same `is_archived: true`,
  `NOINDEX,NOFOLLOW` status as S3. `v=vs.71` corresponds to
  Visual Studio .NET 2003, i.e. **VFP 7.0**, two major versions
  before S7's VFP 9 SP2.
- **Gives a version boundary S7 alone could not.** S8's byte-0
  file-type list has no `0x31` or `0x32`; its field-type list has
  no `V`, `Q`, `W`; its field-flags list stops at `0x04` with no
  `0x06` combination and no `0x0C` autoincrement, and bytes 19–32
  are undifferentiated reserved space rather than holding
  autoincrement next/step values. **Autoincrement, Varchar,
  Varbinary and Blob were introduced somewhere between VFP 7 and
  VFP 9 SP2**, not present "in Visual FoxPro" generally as earlier
  notes in this document had been loose about.
- The companion page confirms the `FILESPEC` naming convention
  behind the ISO specimens: `26SPEC.pjx` documents FoxPro 2.6,
  `60SPEC.pjx` documents 5.0/6.0/7.0 together. The `30DBC.DBF` /
  `30PJX.DBF` specimens in `dbf/testdata/vfp/` are VFP 3.0's own
  version-named instance, predating the later consolidation into
  `60SPEC` — not a discrepancy, just an earlier naming generation.

### S7 — VFP 9.0 SP2, "Table File Structure (.dbc, .dbf, .frx,
.lbx, .mnx, .pjx, .scx, .vcx)"

- Supplied as document text, no URL given; the "environ 4 minutes
  pour lire" reading-time marker and page structure match
  Microsoft Learn's documentation platform, French locale. Content
  is a superset of S2 — same header and field-subrecord tables,
  plus a "Remarks" section S2 does not carry and a more precise
  statement of the `_NullFlags` bit layout for `V`/`Q` fields.
- Not yet independently confirmed as still live; treat with the
  same durability caution as S2/S5 until a URL is checked.

### S1 — KB Q130461, "New Header Structure for Tables in Visual FoxPro"

- Retrieved from `raw.githubusercontent.com/jeffpar/kbarchive/master/kb/130/Q130461/README.md`
- Article dated 21-AUG-1999, applies to Visual FoxPro for Windows 3.0
- The `kbarchive` project is an unofficial preservation of the
  Microsoft Knowledge Base, which Microsoft retired. GitHub's HTML
  interface disallows automated access; the `raw.githubusercontent.com`
  path works.
- **Durability: poor.** A single volunteer repository.

### S2 — VFP 9 SP2 Help, "Table File Structure"

- `https://www.vfphelp.com/help/_5wn12pc0x.htm`
- Microsoft Visual FoxPro 9 SP2 Help file, VFPX Edition v1.08,
  "2009-2017 Placed under Creative Commons licensing by Microsoft
  Corporation"
- **This is the single most complete source.** It is the only one
  found that documents field-descriptor byte 18 in full, which is the
  detail the null-value question turns on.
- **Caveat on version:** this is the VFP **9** Help, not 3.0. VFP 9
  added `Varchar`, `Varbinary`, `Blob`, and autoincrement, so parts of
  the field-type list post-date 3.0. The header and field-descriptor
  *structure* is unchanged across the 0x30 family, but a reader should
  not assume every type listed exists in 3.0.
- **Durability: poor.** Community-hosted; returns HTTP 503 under
  even light repeated access.

### S3 — Microsoft Learn archive, "Visual FoxPro Data and Field Types"

- `https://learn.microsoft.com/en-us/previous-versions/visualstudio/foxpro/ww305zh2(v=vs.80)`
- Page metadata: `is_archived: true`, `ms.topic: archived`,
  `ROBOTS: NOINDEX,NOFOLLOW`, last updated 2007-07-09
- Authoritative for storage sizes and value ranges per type.
- **Durability: moderate.** Microsoft-hosted but explicitly archived
  and de-indexed, which is usually a step before removal.

### S5 — VFP 9 SP2 Help, "Code Pages Supported by Visual FoxPro"

- `https://www.vfphelp.com/help/_5WN12P5S3.htm`
- Same provenance and durability as S2.
- Authoritative for the byte-29 identifier table, reproduced below.

### S4 — VFP 3.0 installation media

- `https://archive.org/download/ms-vfp30/VFP30US%20%28Standard%29.ISO`
- ISO 9660, volume label `VFP30US`, 21,161,984 bytes, files dated
  1995-06-21. Contents are in 13 spanning CAB archives; extracting
  `VFP1.CAB` yields all 700 files.
- **This is the only primary source among the four**, and the only one
  that yields specimens rather than descriptions.
- **Durability: good** while the Internet Archive persists.

---

## What the ISO contains

Extracting `VFP1.CAB` gives 700 files, of which 134 are structured
(DBF-format) files:

| version byte | count | meaning |
| ------------ | ----- | ------- |
| `0x30` | 129 | Visual FoxPro 3.0 |
| `0xF5` | 5 | FoxPro 2.x with FPT |

The `0x30` files are VFP's own metadata tables — `.SCX` forms, `.FRX`
reports, `.PJX` projects, `.MNX` menus — plus the `Filespec` projects
that Microsoft shipped as the field-by-field format documentation
(KB Q136586 points at these).

Field types observed across all 129 files:

| type | count | name |
| ---- | ----- | ---- |
| `M` | 1628 | Memo |
| `N` | 1109 | Numeric |
| `L` | 688 | Logical |
| `C` | 523 | Character |
| `Y` | 13 | **Currency** |
| `D` | 9 | Date |
| `I` | 4 | **Integer** |
| `G` | 3 | **General** |
| `B` | 3 | **Double** |

The four in bold are types blipper does not implement, present here in
real vendor-written files.

### The Professional edition adds nothing on the two gaps

`VFP30US (Professional).ISO` (101,916,672 bytes, 17 CABs) was also
checked, since it carries the sample databases the Standard edition
lacks — `TASTRADE.DBC` and `TESTDATA.DBC` with 23 business tables
(customers, orders, products, employees).

Those confirm one thing and settle another. **Both DBCs carry byte
28 = `0x07`**, exactly as KB Q130461 predicts for a database
container (`0x01` CDX + `0x02` memo + `0x04` DBC). And across all
136 VFP files in the Professional edition — including every sample
table — there are still **zero nullable fields, zero system fields,
and zero DateTime fields.**

So the gap is not an artifact of the Standard edition being a
cut-down disc. Microsoft's sample data simply does not use nullable
columns or DateTime, and neither do its metadata tables.

**No field in any of the 129 files has byte 18 set.** Zero nullable
columns, zero system columns. These are Microsoft's own metadata
tables and there was no reason to make their columns nullable, so the
distribution does not exercise `_NullFlags`. That remains the one
detail with documentation but no specimen.

`DateTime` also does not appear, so the epoch convention below is
documented but unverified against data.

### Specimens staged in this repository

`dbf/testdata/vfp/` carries three of the smallest, chosen for what
they demonstrate rather than size alone:

| file | bytes | why |
| ---- | ----- | --- |
| `30DBC.DBF` | 1529 | Microsoft's own specification of the DBC schema |
| `30PJX.DBF` | 8489 | VFP 3.0 project-file spec |
| `26PJX.DBF` | 6574 | FoxPro 2.6 counterpart, for comparison |

---

## Table header, 32 bytes

From S2, corroborated by S1 and confirmed against the S4 specimens.

| offset | size | description |
| ------ | ---- | ----------- |
| 0 | 1 | File type (see below) |
| 1–3 | 3 | Last update, YYMMDD |
| 4–7 | 4 | Number of records |
| 8–9 | 2 | Position of first data record |
| 10–11 | 2 | Length of one record, including the delete flag |
| 12–27 | 16 | Reserved |
| 28 | 1 | Table flags |
| 29 | 1 | Code page mark |
| 30–31 | 2 | Reserved, contains 0x00 |
| 32–n | | Field subrecords, 32 bytes each |
| n+1 | 1 | Header terminator, `0x0D` |
| n+2 … n+264 | 263 | Backlink |

### File type byte

| value | meaning |
| ----- | ------- |
| `0x02` | FoxBASE / dBase II |
| `0x03` | FoxBASE+ / FoxPro / dBase III PLUS / dBase IV, no memo |
| `0x30` | **Visual FoxPro** |
| `0x31` | Visual FoxPro, autoincrement enabled |
| `0x32` | Visual FoxPro, Varchar/Varbinary/Blob enabled |
| `0x43` | dBASE IV SQL table files, no memo |
| `0x63` | dBASE IV SQL system files, no memo |
| `0x83` | FoxBASE+/dBASE III PLUS, with memo |
| `0x8B` | dBASE IV with memo |
| `0xCB` | dBASE IV SQL table files, with memo |
| `0xF5` | FoxPro 2.x (or earlier) with memo |
| `0xFB` | FoxBASE |

### When VFP rewrites a FoxPro 2.x header — S7's "Remarks"

Directly relevant to blipper's T-10 decision to keep writing
`0x03` for the DBC sidecar rather than `0x30`: S7 states VFP does
**not** modify a file already saved in FoxPro 2.x format unless
one of four features is added —

1. Null value support
2. `DateTime`, `Currency`, or `Double` fields
3. a `CHAR` or `MEMO` field marked binary
4. the table is added to a `.dbc`

That is a positive confirmation of the inference T-10 already
made from the opposite direction: writing `0x03` with byte 28 set
is not merely *safe*, it is what VFP's own migration rule treats
as the boundary — a `0x03` file with none of the four features
above stays `0x03` even inside VFP itself. blipper's sidecar
currently sets byte 28 (DBC-owned) but adds none of the other
three triggers, which is consistent with staying at `0x03` rather
than a gap to close.

### Table flags, byte 28

| bit | meaning |
| --- | ------- |
| `0x01` | File has a structural `.cdx` |
| `0x02` | File has a Memo field |
| `0x04` | File is a database (`.dbc`) |

The byte holds the sum. S1 notes that a DBC has both memos and an
index, so a `.dbc` file's own header carries `0x07`.

blipper's T-10 sidecar deliberately writes version `0x03` with byte 28
= `0x0C` (`0x04` DBC-owned plus a blipper-reserved `0x08` provenance
bit) rather than claiming `0x30`. See `docs/RESOLVED.md`, T-10, for
why: writing `0x30` is a promise to honour field types and null
semantics that blipper does not yet implement.

### Backlink

263 bytes immediately after the header terminator, holding the
relative path of the associated `.dbc`. Per S2: if the first byte is
`0x00` the file is not associated with a database, and **database
files themselves therefore always contain `0x00`**.

S2 gives a formula for field count that folds the backlink in:
`(x - 296) / 32`, where `x` is the position of the first record and
296 = 263 backlink + 1 terminator + 32 for the first subrecord.

---

## Field subrecord, 32 bytes

From S2. **This is the part no other source found documents in full**,
and byte 18 is the reason this document exists.

| offset | size | description |
| ------ | ---- | ----------- |
| 0–10 | 11 | Field name, NUL-padded |
| 11 | 1 | Field type |
| 12–15 | 4 | **Displacement of field in record** |
| 16 | 1 | Length in bytes |
| 17 | 1 | Decimal places |
| 18 | 1 | **Field flags** |
| 19–22 | 4 | Autoincrement next value |
| 23 | 1 | Autoincrement step value |
| 24–31 | 8 | Reserved |

### Field flags, byte 18

| bit | meaning |
| --- | ------- |
| `0x01` | System column, not visible to the user |
| `0x02` | Column can store null values |
| `0x04` | Binary column (CHAR and MEMO only) |
| `0x06` | Null **and** binary (Integer, Currency, Character/Memo) |
| `0x0C` | Column is autoincrementing |

### Null storage

S2, in a note attached to the field-type list:

> For each Varchar and Varbinary field, one bit, or "varlength" bit,
> is allocated in the last system field, which is a hidden field and
> stores the null status for all fields that can be null.

So the mechanism is: **the last field in the table is a system field**
(byte 18 bit `0x01`) holding a bitmap, one bit per nullable column.
That answers the position question, which KB Q130461 does not address
and which no article in the 3,879-title FoxPro KB index mentions —
`_NullFlags` appears zero times across the whole archive.

**Bit ordering, partially resolved by S7** — "Table File Structure
(.dbc, .dbf, .frx, .lbx, .mnx, .pjx, .scx, .vcx)", the VFP 9.0 SP2
online documentation, a different page from S2 with an equivalent
note stated more precisely:

> For each Varchar and Varbinary field, one bit, or "varlength"
> bit, is allocated in the last system field... If the Varchar or
> Varbinary field can be null, the null bit follows the
> "varlength" bit.

So for a `V`/`Q` field specifically, the bitmap carries **two**
bits, adjacent, varlength before null — not one. This settles the
ordering *within one field's pair of bits*, and confirms the
per-field allocation is variable (`C`/`M`/nullable-only fields get
one bit each; nullable `V`/`Q` fields get two). What remains
unverified is the ordering *across* fields — whether the bitmap
walks the field descriptor array in order, and what happens when
columns are added or dropped after creation.

S7 also gives the encoding for the length itself: **if the
varlength bit is 1, the field's actual content length is stored in
the field's own last byte**; if 0, the length equals the declared
field size. That is new — S2 established that `V`/`Q` fields carry
a length somewhere but not where.

**Specimens do exist elsewhere.** Microsoft's later Northwind
sample database carries them, mirrored at
`github.com/ha1tch/VPFX-Samples` under `Northwind/` — eight tables
with 57 nullable fields between them, every `_NullFlags` column
sitting last with field type byte `0x30` (ASCII `'0'`, corrected
2026-07-24 from an earlier `0x00` claim carried from secondary
documentation — confirmed against real field descriptor bytes)
and byte 18 `0x05`. The
bitmap width tracks the nullable count: 1 byte for up to 8
columns, 2 beyond, visible across that set.

The single `DateTime` specimen in the corpus is
`Solution/Europa/photos.dbf`, column `CREATED`.

Referenced rather than copied. blipper's approach is to establish
the facts from those files and then **generate** its own fixtures:
a synthesised table can exercise one nullable column, then two,
then a `0x06` nullable-and-binary field, which isolates decoder
paths better than whatever Northwind happens to contain.

### Displacement, bytes 12–15

Worth noting because blipper does not currently read it: the field
descriptor **states** each field's offset within the record rather
than requiring the reader to sum preceding widths. For well-formed
files the two agree. A reader that uses the stated displacement is
more robust against a file where they do not.

---

## Field types and storage

From S3, with sizes as stored in a table rather than in memory.

| code | type | stored size | notes |
| ---- | ---- | ----------- | ----- |
| `C` | Character | 1 byte/char, ≤ 254 | |
| `C` | Character (binary) | as Character | byte 18 bit `0x04` |
| `N` | Numeric | 1–20 bytes, ASCII | range ±.9999999999E+19/20 |
| `F` | Float | as Numeric | "Same as Numeric" per S3 |
| `L` | Logical | 1 byte | |
| `D` | Date | 8 bytes | `{^0001-01-01}` to `{^9999-12-31}` |
| `T` | DateTime | 8 bytes | two 4-byte LE integers: Julian day (since 4713 BC), then ms since midnight — **confirmed** 2026-07-24 against real VFP 9 data, see below |
| `I` | Integer | 4 bytes | −2147483647 to 2147483647 |
| `I` | Integer (autoinc) | 4 bytes | byte 18 bit `0x0C` |
| `Y` | Currency | 8 bytes | ±922337203685477.5807 |
| `B` | Double | 8 bytes | ±4.94065645841247E-324 to ±8.9884656743115E307 |
| `M` | Memo | 4 bytes in table | pointer into `.fpt` |
| `M` | Memo (binary) | 4 bytes in table | byte 18 bit `0x04` |
| `G` | General | 4 bytes in table | OLE object reference |
| `P` | Picture | 4 bytes in table | |
| `V` | Varchar | 254 (S10) | fixed-width slot, **space**-padded (S10, corrected from an earlier `0x00` claim), actual length in the field's last byte when not full (S7, S10) |
| `Q` | Varbinary | ≤ 255 | identical on-disk storage to `V`, space-padded (S10); differs only in that no code page translation is applied to the content |
| `W` | Blob | 4 bytes in table | VFP 9; `General`'s exact pointer mechanism — confirmed three independent ways: decoding a real specimen's FPT payload, S9's design-intent statement, and S10's "same organization as normal Memo fields" |

S2 adds: **"Integers in table files are stored with the least
significant byte first"** — little-endian, consistent with the rest of
the DBF family.

### The Currency range implies the scale

S3 gives the range as ±922337203685477.5807. That is exactly
2⁶³−1 divided by 10⁴ (9223372036854775807 / 10000), which confirms
Currency is **a 64-bit signed integer scaled by 10,000** — four
implied decimal places. Neither source states the scale directly; it
falls out of the range.

### DateTime: documented, and now confirmed

S2 and the DateTime type page both say eight bytes as two four-byte
integers. Neither this document's original sources, nor any VFP 3.0
distribution specimen, stated the epoch.

**Settled 2026-07-24**, by a route neither S2 nor the VFP 3.0 media
could offer: dBASE 7's own documentation states its `@` Timestamp
epoch explicitly — Julian day number since 1 January 4713 BC, plus
milliseconds since midnight (`docs/DBASE_FORMAT.md`, source S6). That
gave a concrete, checkable claim rather than a guess, and it was
checked against real data rather than assumed: `photos.dbf` in the
`ha1tch/VPFX-Samples` mirror (`Solution/Europa/photos.dbf`, column
`CREATED`) decodes under that exact formula to

    2004-10-12 14:03:30
    2004-10-12 14:20:06
    2004-10-12 14:22:05

Three photos taken minutes apart on one afternoon — the pattern real
sample-data timestamps make, not the pattern a coincidence makes.
VFP 9 shipped in 2004.

This confirms VFP's `T` DateTime and dBASE 7's `@` Timestamp share an
encoding, despite the two products being a decade and a distinct
lineage apart. Whether that is because VFP's own format was already
settled by the time dBASE 7 documented it, or because both trace to
a common earlier convention, is not established — only that the
bytes agree.

Implemented in `dbf/vfptypes.go`: `decodeJulianDateTime` /
`encodeJulianDateTime`, the standard Fliegel & Van Flandern
Julian-day/Gregorian-calendar conversion, oracle-verified against
the specimen above rather than through `dbf.Open` — `photos.dbf`
also carries a `W` (Blob) field this package does not implement,
which is a separate, unrelated gap and was not routed around.

The **day boundary's timezone** (local vs UTC) remains unconfirmed;
the three timestamps above are internally consistent regardless of
which convention applies, so they do not distinguish between them.

---

## DBC schema, from Microsoft's own specification file

`30DBC.DBF` in the distribution is Microsoft's field-by-field
specification of the database container. Its eight records:

| FIELDNAME | TYPE | WIDTH |
| --------- | ---- | ----- |
| `OBJECTID` | I | 4 |
| `PARENTID` | I | 4 |
| `OBJECTTYPE` | C | 10 |
| `OBJECTNAME` | C | 128 |
| `PROPERTY` | M | 4 |
| `CODE` | M | 4 |
| `RIINFO` | C | 6 |
| `USER` | M | 4 |

This **confirms the schema blipper implemented in T-10**, which was
reconstructed from secondary sources before this file was found. Names,
types, and widths all match.

---

## What a VFP 3.0 implementation still needs

| piece | documented | specimen |
| ----- | ---------- | -------- |
| Version byte `0x30` | S1, S2 | 129 files |
| Table flags byte 28 | S1, S2 | yes |
| 263-byte backlink | S1, S2 | yes |
| Field flags byte 18 | S2 | **no** — all zero in the distribution |
| Field displacement 12–15 | S2 | yes |
| Currency, Integer, Double, General | S3 | yes, 23 fields total |
| DateTime encoding | S2, S3, S6, VPFX Northwind specimen | **yes** — confirmed 2026-07-24 |
| `_NullFlags` position | S2, S7 | yes (last field, system-flagged) |
| `_NullFlags` bit ordering across fields | **yes** | **yes** — confirmed 2026-07-24, see T-34's resolution |
| DBC schema | S4 (`30DBC.DBF`) | yes |

Read-only support is now documented, verified, and implemented for
every row above. The one remaining gap in this area is untested
interaction between `_NullFlags` and `V`/`Q` (Varchar/Varbinary)
fields specifically — see T-35.

---

## Code pages, byte 29

From S5 — VFP 9 SP2 Help, "Code Pages Supported by Visual FoxPro",
`https://www.vfphelp.com/help/_5WN12P5S3.htm`. This is the
authoritative list; blipper's table was assembled from secondary
sources in T-21 and was missing nine of these until T-26.

| identifier | code page | platform |
| ---------- | --------- | -------- |
| `0x01` | 437 | U.S. MS-DOS |
| `0x02` | 850 | International MS-DOS |
| `0x03` | 1252 | Windows ANSI |
| `0x04` | 10000 | Standard Macintosh |
| `0x64` | 852 | Eastern European MS-DOS |
| `0x65` | 866 | Russian MS-DOS |
| `0x66` | 865 | Nordic MS-DOS |
| `0x67` | 861 | Icelandic MS-DOS |
| `0x68` | 895 \* | Kamenicky (Czech) MS-DOS |
| `0x69` | 620 \* | Mazovia (Polish) MS-DOS |
| `0x6A` | 737 \* | Greek MS-DOS (437G) |
| `0x6B` | 857 | Turkish MS-DOS |
| `0x78` | 950 | Traditional Chinese Windows |
| `0x79` | 949 | Korean Windows |
| `0x7A` | 936 | Chinese Simplified Windows |
| `0x7B` | 932 | Japanese Windows |
| `0x7C` | 874 | Thai Windows |
| `0x7D` | 1255 | Hebrew Windows |
| `0x7E` | 1256 | Arabic Windows |
| `0x96` | 10007 \* | Russian Macintosh |
| `0x97` | 10029 | Macintosh EE |
| `0x98` | 10006 | Greek Macintosh |
| `0xC8` | 1250 | Eastern European Windows |
| `0xC9` | 1251 | Russian Windows |
| `0xCA` | 1254 | Turkish Windows |
| `0xCB` | 1253 | Greek Windows |

\* Not detected when `CODEPAGE=AUTO` is set in the configuration
file. Worth knowing: it explains how a file can carry a code page
its own tooling would not have inferred.

Three have no `charmap` table in `golang.org/x/text` — CP861,
CP857, CP737 — and blipper names them without mapping them. A near
neighbour would decode most of a file correctly, which is the kind
of nearly-right that hides a problem.

---

## Reproducing this

    # KB article
    wget https://raw.githubusercontent.com/jeffpar/kbarchive/master/kb/130/Q130461/README.md

    # Help page (rate-limits aggressively; one request at a time)
    wget https://www.vfphelp.com/help/_5wn12pc0x.htm

    # Installation media, and the specimens within it
    wget https://archive.org/download/ms-vfp30/VFP30US%20%28Standard%29.ISO
    7z x -oiso 'VFP30US (Standard).ISO'
    7z x -ocab iso/VFP1.CAB      # spanning set; VFP1 contains all 700 files

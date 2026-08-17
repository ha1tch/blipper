# xBase index file formats

Version: 0.9.25
Last reviewed: 2026-07-23

## Provenance

Restated from **"Xbase File Format Description"** by Erik Bachmann,
Clickety Click Software, Roskilde, Denmark. Created 1998-03-26,
document revision dated 1999-03-08, with a change history running
back to 1996-10-03. Retrieved 2026-07-23 from a copy vendored in
the `dbffile` npm package at
`unpkg.com/dbffile@1.4.0/doc/xbase-file-format-description.html`;
the author's own URL was `e-bachmann.dk/docs/xbase.html`.

The document is a compilation. Its own reference list credits
around 28 sources including Borland technical documents, Microsoft
Knowledge Base articles, the CA-Clipper file-structure
documentation, and several 1980s programming references. Two
sections carry named third-party attribution within the document:

- the **CDX** structure, credited to David Kuechler
- the **record-locking offsets** section, credited to Phil Barnett

The author notes the document's own position on rights: that
Ashton-Tate (and later Borland) held copyright on the *name*
dBASE but not on the file structure, which is why "xBase" became
the generic term.

This file restates byte layouts only. Layouts are facts; the

**Second source, added 2026-07-24: Microsoft, VFP 7.0 archived
documentation** (`learn.microsoft.com/.../foxpro/`, `v=vs.71`,
`is_archived: true`, `NOINDEX,NOFOLLOW` — same status as
`docs/VFP30_FORMAT.md`'s S3/S8). Four pages: "Compact Index File
Structure (.idx)", "Index File Structure (.idx)" — the
uncompressed layout, not found anywhere else this session —
"Compound Index File Structure (.cdx)", and "Memo File Structure
(.FPT)". This is the vendor's own account of formats Bachmann's
document reconstructed independently, and it **confirms** rather
than corrects: every field this session had already derived from
the DBFCDX oracle matches. Cited inline below as **[MS]**.
wording, organisation, and commentary here are blipper's. The
original document's narrative, ASCII diagrams, and 1990s link
list are not reproduced.

**Durability: poor.** The author's original site is long gone;
this copy survives incidentally inside an npm package. That is
precisely the fragility this document exists to guard against.

---

## Why this matters to blipper

Three index formats remain unimplemented — NDX, IDX, MDX — and all
three are described here. The document also describes NTX and CDX,
which blipper *has* implemented from oracle evidence, so those
sections serve as **independent corroboration** of work that was
otherwise verified only against Clipper's own output.

Where this document and blipper's oracle-derived implementations
agree, confidence is higher than either source alone would
justify. Where they disagree, the oracle wins — a real file beats
a reconstruction.

---

## NDX — dBASE III+ single-key index

A paged B-tree. The document notes it is commonly described as a
B+ tree but is more precisely a paged B-tree. **Page size is 512
bytes.** Page 0 is the header, called the anchor node.

### Header, page 0

| offset | size | contents |
| ------ | ---- | -------- |
| 0–3 | 4 | Starting (root) page number |
| 4–7 | 4 | Total page count |
| 8–11 | 4 | Reserved |
| 12–13 | 2 | Key length |
| 14–15 | 2 | Keys per page |
| 16–17 | 2 | Key type: `0` character, `1` numeric |
| 18–21 | 4 | Size of a key record |
| 22 | 1 | Reserved |
| 23 | 1 | Unique flag |
| 24–511 | — | Key expression string |

**Root page offset is `page number × 512`.**

**Key record size is a multiple of four**, computed as 4 (pointer
to next page) + 4 (record number) + the key size rounded up to a
multiple of four. The document gives the worked example: a
10-byte key yields a 20-byte record, being 4 + 4 + 12.

### Page structure

Each page opens with a 4-byte count of valid entries, followed by
an array of key entries. Each entry:

| offset | size | contents |
| ------ | ---- | -------- |
| 0–3 | 4 | Pointer to the lower-level page |
| 4–7 | 4 | Record number in the data file |
| 8– | key length | Key data |

**Numeric keys are stored as IEEE doubles.**

### Search

The anchor node stays resident while the file is open and selects
which root node to enter. The root is read and scanned
sequentially until a key ≥ the target is found; the process
repeats at each level until the lower-level pointer is zero, at
which point a matching leaf key yields its record number.

---

## IDX — FoxPro 2 uncompressed index

512-byte pages. Page 0 is the header.

### Header

| offset | size | contents |
| ------ | ---- | -------- |
| 0–3 | 4 | Pointer to the root node |
| 4–7 | 4 | Pointer to the free list; `-1` if empty (FoxPro) or `0` (FoxBASE) |
| 8–11 | 4 | Pointer to end of file |
| 12–13 | 2 | Key length |
| 14 | 1 | Index options |
| 15 | 1 | Index signature |
| 16–235 | 220 | Key expression |
| 236–455 | 220 | FOR expression |
| 456–511 | 56 | Reserved |

**Index options are a sum of bit values:**

| value | meaning |
| ----- | ------- |
| `0x01` | Unique index |
| `0x08` | FOR clause present |
| `0x10` | Bit vector (SoftC) |
| `0x20` | Compact index format (FoxPro) |
| `0x40` | Compound index header (FoxPro) |
| `0x80` | Structural index (FoxPro) |

**Key lengths:** number and date keys are 8 bytes; character keys
are ≤ 100 bytes and **not** NUL-terminated.

### Node structure

| offset | size | contents |
| ------ | ---- | -------- |
| 0–1 | 2 | Node attributes |
| 2–3 | 2 | Number of keys |
| 4–7 | 4 | Pointer to left sibling; `-1` if none |
| 8–11 | 4 | Pointer to right sibling; `-1` if none |
| 12– | — | Array of key entries |

**Node attributes are a sum:** `0` index node, `1` start page,
`2` end page.

Within a non-leaf entry: key data, then the record number in the
data file **high-order byte first**.

**Confirmed by [MS] independently**, with two details Bachmann's
account did not give for this format specifically:

- **The numeric-key transform matches CDX's, not NDX's.** [MS]:
  convert to IEEE double, swap Intel byte order to left-to-right,
  then invert all 64 bits if negative or only the leftmost bit if
  positive. This is a genuine finding for T-29's future scope: the
  *uncompressed* IDX format uses the same transformed encoding as
  CDX, so an implementation covering both compact and uncompressed
  IDX can share one numeric codec rather than needing NDX's plain
  encoding as a third variant.
- **The key's type is not stored in the index at all** — [MS]
  states this explicitly. A reader must already know the key
  expression's type from elsewhere (the table schema) to interpret
  the bytes; nothing in the `.IDX` file itself distinguishes a
  character key from a transformed numeric one.

### Cross-check against blipper's oracle probe

A probe run 2026-07-23 used `DBFCDX.LIB` to generate a real
`.IDX` and decoded its header. **The observations agree with this
document** on every field checked:

| field | document | probe |
| ----- | -------- | ----- |
| page size | 512 | 512 (2048-byte file = 4 pages) |
| offset 0–3 | root pointer | 1024 — page 2, plausible for a small index |
| offset 4–7 | free list | 0 |
| offset 12–13 | key length | 10, matching the indexed `C(10)` field |
| offset 14 | index options | `0x20` — compact index format |

The `0x20` is worth noting: it says the file `DBFCDX` produced is
in **compact** format, which per the CDX section below means the
key storage follows the compact scheme rather than the plain one
described above. That reconciles an observation from the probe
that had looked anomalous — keys packed backward from the page
end, at offsets 1514–1531 in descending order for ascending keys.
That is the compact layout, not the uncompressed one.

**Implication for T-29:** a `DBFCDX`-generated `.IDX` may not
exercise the uncompressed format at all. Verifying plain IDX may
need a different generator, or the compact path may simply be
what matters in practice. Worth settling before implementation.

**Two concrete candidates identified 2026-07-24, not yet tried.**
FoxBASE+ 2.10 predates CDX entirely — historically it only ever
wrote `.IDX`, and it should be the uncompressed variant specifically,
since the compact scheme was a FoxPro 2.x innovation. FoxPro 2.6
DOS is a second candidate: FoxPro 2.x supported writing `.IDX` for
FoxBASE+ backward compatibility alongside its native `.CDX`, and
whether that compatibility path produces the plain or the compact
layout is an open, testable question.

Neither has been run. FoxBASE+ 2.10's DOSBox runtime requires
`BRAND.EXE` serialisation blipper's own work has deliberately not
pursued (see `docs/RESEARCH_NOTES.md`). FoxPro 2.6 DOS has no known
protection obstacle and is the more promising near-term path, now
that real interactive DOSBox access (not headless automation) has
proven able to get past blockers headless scripting could not — see
`STATUS.md` for the dBASE 5.0 precedent this follows.

---

## CDX — FoxPro compound index

Credited within the source document to David Kuechler.

A CDX is a compact IDX. The initial IDX holds one key per tag: the
key is a 10-byte character string naming the tag, and the record
number stored with it is the offset of that tag's root page.

The source notes plainly that **no description of the compression
algorithm was available to it.**

**[MS] resolves this gap directly**, and states the architecture
more precisely than "a CDX is a compact IDX":

> All compound indexes are compact indexes... One file structure
> exists to track all the tags in the .cdx file. This structure is
> identical to the compact index file structure with one
> exception — the leaf nodes at the lowest level of this structure
> point to one of the tags in the compound index. All tags in the
> index have their own complete structure that is identical to the
> compact index structure for an .idx file.

So a CDX is **two layers of the same compact-index structure**:
one compact-index tree serving as the tag directory, whose leaves
point at tag roots instead of DBF records, and one complete
compact-index tree per tag, identical in shape to a compact
`.IDX`. That is precisely the shape blipper's code already takes
— `idx` reuses `cdx`'s leaf codec (`cdx.WriteLeaf`) because both
packages are encoding the same structure — and this is the primary
source confirming that reuse was the correct design rather than a
convenient shortcut.

### Header

| offset | size | contents |
| ------ | ---- | -------- |
| 0–3 | 4 | Pointer to root node |
| 4–7 | 4 | Pointer to free list; `-1` if empty |
| 8–11 | 4 | Version; page count in FoxBASE and FoxPro 1.x, reserved in FoxPro 2.x |
| 12–13 | 2 | Key length |
| 14 | 1 | Index options (same bit values as IDX) |
| 15 | 1 | Index signature |
| 16–501 | — | Reserved, NUL-filled |
| 502–503 | 2 | Sort order: `0` ascending, `1` descending |
| 504–505 | 2 | Total expression length (FoxPro 2) |
| 506–507 | 2 | FOR expression length |
| 508–509 | 2 | Reserved |
| 510–511 | 2 | Key expression length |
| 512–1023 | 512 | Key expression, then FOR expression |

The key expression comes first with a NUL terminator, then the
FOR expression.

### Node attributes

A sum: `0` interior node, `1` root page, `2` leaf page.

### Non-leaf page

| offset | size | contents |
| ------ | ---- | -------- |
| 0–1 | 2 | Node attributes |
| 2–3 | 2 | Number of keys |
| 4–7 | 4 | Left sibling; `-1` if none |
| 8–11 | 4 | Right sibling; `-1` if none |
| 12– | — | Key entries |

Each entry holds key data, then the record number **high-order
byte first**, then a pointer to the child page.

### Leaf page

| offset | size | contents |
| ------ | ---- | -------- |
| 0–1 | 2 | Node attributes |
| 2–3 | 2 | Number of keys |
| 4–7 | 4 | Left sibling |
| 8–11 | 4 | Right sibling |
| 12–13 | 2 | Free space available in page |
| 14–17 | 4 | Record number mask |
| 18 | 1 | Duplicate count mask |
| 19 | 1 | Trailing byte count mask |
| 20 | 1 | Bits used for the record number |
| 21 | 1 | Bits used for the duplicate count |
| 22 | 1 | Bits used for the trailing count |
| 23 | 1 | Bytes holding all three of the above |
| 24– | — | Key entries |

**The compact layout.** Record number, duplicate count and
trailing count are bit-packed at the start of the entry area,
each entry taking the byte count given at offset 23. The **key
values are placed at the end of the area and grow backwards**,
stored with duplicates against the previous key eliminated and
trailing blanks removed. The masks at 14–19 must be ANDed with
the packed values to recover the originals.

This backward key growth is what the blipper IDX probe observed,
confirming the file `DBFCDX` wrote is in compact format.

### Key encoding

**Dates** are stored as Julian dates converted to numbers.

**Numbers** are stored as IEEE doubles with a transformation:
convert to an 8-byte IEEE double, reverse the byte order, then
invert all bits if the value was negative or only the highest bit
if it was not. The purpose is that keys can then be compared
directly with a byte comparison — the transformation makes
lexicographic byte order match numeric order.

That last point is worth flagging: it is the sort of detail that
a reader who assumed plain IEEE storage would get subtly wrong on
negative numbers only.

---

## MDX — dBASE IV multiple index

A tag file: one physical file holding several named indexes.

### File header

| offset | size | contents |
| ------ | ---- | -------- |
| 0 | 1 | Version |
| 1–3 | 3 | Creation date, YYMMDD |
| 4–19 | 16 | Data file name, no extension |
| 20–21 | 2 | Block size |
| 22–23 | 2 | Block size adder |
| 24 | 1 | Production index flag |
| 25 | 1 | Entries in the tag table; maximum 48 |
| 26 | 1 | Length of each tag table entry; maximum 32 |
| 27 | 1 | Reserved |
| 28–29 | 2 | Tags in use |
| 30–31 | 2 | Reserved |
| 32–35 | 4 | Pages in the tag file |
| 36–39 | 4 | Pointer to the first free page |
| 40–43 | 4 | Blocks available |
| 44–46 | 3 | Last update, YYMMDD |
| 47 | 1 | Reserved |
| 48–543 | — | Unused |
| 544– | — | Tag table entries |

### Tag table entry

| offset | size | contents |
| ------ | ---- | -------- |
| 0–3 | 4 | Tag header page number |
| 4–14 | 11 | Tag name |
| 15 | 1 | Key format: `0x00` calculated, `0x10` data field |
| 16 | 1 | Forward tag thread, less-than |
| 17 | 1 | Forward tag thread, greater-than |
| 18 | 1 | Backward tag thread (previous tag) |
| 19 | 1 | Reserved |
| 20 | 1 | Key type: `C`, `N`, or `D` |
| 21–31 | 11 | Reserved |

### Tag header

| offset | size | contents |
| ------ | ---- | -------- |
| 0–3 | 4 | Pointer to root page |
| 4–7 | 4 | File size in pages |
| 8 | 1 | Key format |
| 9 | 1 | Key type: `C`, `N`, or `D` |
| 10–11 | 2 | Reserved |
| 12–13 | 2 | Index key length |
| 14–15 | 2 | Maximum keys per page |
| 16–17 | 2 | Secondary key type |
| 18–19 | 2 | Index key item length |
| 20–22 | 3 | Reserved |
| 23 | 1 | Unique flag |

**Key format is a sum:** `0x00` right/left/dtoc, `0x08` descending
order, `0x10` fields/string, `0x40` unique keys.

**Key lengths by type:** numeric 12, date 8, character ≤ 100 and
**not** NUL-terminated.

**Secondary key type:** `0` is character or numeric in dBASE IV
and character in dBASE III; `1` is date in dBASE IV and numeric or
date in dBASE III.

---

## NTX — Clipper index

blipper implements this already, verified against Clipper 5.2e.
Included because independent agreement is worth recording.

A modified B+ tree with **1024-byte pages.** Page 0 is the header.

| offset | size | contents |
| ------ | ---- | -------- |
| 0–1 | 2 | Signature: `0x0003` Clipper 87, `0x0006` Clipper 5.x |
| 2–3 | 2 | Indexing version (compiler version) |
| 4–7 | 4 | Offset of the first index page (root) |
| 8–11 | 4 | Offset of an unused next key page |
| 12–13 | 2 | Key size + 8 |
| 14–15 | 2 | Key size |
| 16–17 | 2 | Decimals in key |
| 18–19 | 2 | Maximum items per page |
| 20–21 | 2 | Half page |
| 22–277 | 256 | Key expression, NUL-terminated |
| 278 | 1 | Unique flag: `1` unique, `0` not |
| 279–1023 | — | Unused |

**"Half page" is the minimum key count per page** — maximum
divided by two — which is the load factor a B-tree must maintain.
The root page is exempt and may hold as few as one entry.

### Page structure

A used page opens with a count of entries, then an array of
unsigned longs whose length is the maximum keys per page plus
one. A value of `0x00` means no record; other values are record
offsets from the start of the page. The index entries follow.

Each entry:

| offset | size | contents |
| ------ | ---- | -------- |
| 0–3 | 4 | Address of the left page in the tree |
| 4–7 | 4 | Record number in the DBF |
| 8– | key size | Key field |

Empty pages form a linked list: bytes 0–3 hold the address of the
next empty page, and `0x00000000` marks the last in the chain.

### Agreement with blipper

blipper's `ntx` package was written from Clipper oracle output,
independently of this document. The points that can be compared —
1024-byte pages, key expression stored as text in the header,
unique flag, the entry triple of left pointer, record number and
key — **agree**. Two independent derivations reaching the same
layout is stronger evidence than either alone.

---

## Notes carried across from the source

Two items worth preserving because they bear on implementation
rather than merely describing bytes.

**Reserved areas may contain live data.** The source warns that
regions labelled reserved, unused, or garbage can hold fragments
of whatever previously occupied that disk space, and advises
zeroing them on write. blipper does zero its reserved regions.
This also explains stale bytes observed in the Clipper corpus.

**Record-lock offsets differ between products**, which matters for
interoperability. Credited within the source to Phil Barnett: an
xBase runtime places a record lock at a large arbitrary offset
plus the record's file offset, so that reads at the real offset
pass through the lock. Clipper originally used 1,000,000,000;
FoxPro used a different and larger base. Clipper later adopted
FoxPro's base for its CDX and IDX drivers but **not** for NDX,
which is why Clipper and dBASE locking historically did not
interoperate on `.NDX` files even though both could create them.

That last point is directly relevant to blipper's T-19 locking
work, which chose its own lock-region constants. Those constants
coordinate blipper with itself; matching a specific product's base
offset would be required to coordinate with that product, and is
not currently attempted.

---

## A CDX variant not covered above: the VFP 9 Binary Index

`INDEX ON <expr> TAG <name> BINARY` (source S9,
`docs/VFP30_FORMAT.md`) is a specialised bitmap-style index
introduced in VFP 9, restricted to `NOT NULL` logical expressions
— its own documented use case is `INDEX ON DELETED() TAG DELETED
BINARY`. Reported around 30× smaller than an ordinary index over
the same expression, but excluded from `SEEK` and `SET ORDER TO`
entirely, which suggests it is a Rushmore-optimisation aid rather
than a general-purpose index a caller would traverse directly.

Not investigated further this session: no specimen or oracle
exists for it, and its restricted purpose makes it a poor
candidate to prioritise even once one is found. Recorded here so
a future encounter with an unfamiliar CDX tag doesn't get
mistaken for a malformed compact index — it may be this instead.

---

## FPT memo confirmation

**[MS]**'s "Memo File Structure (.FPT)" page independently confirms
blipper's oracle-verified implementation (T-12, closed v0.4.0):
header pointer-to-next-free-block and block-size fields **stored
big-endian** ("most significant byte first" — the one place in the
xBase family that departs from the little-endian convention
everywhere else), block signature `0` picture / `1` text, and a
4-byte length field. No corrections; recorded because independent
confirmation from the vendor is worth more than the oracle alone,
and this is the first primary Microsoft source found for FPT
specifically.

---

## Cross-references

- `docs/DBASE_FORMAT.md` — dBASE table and memo formats
- `docs/VFP30_FORMAT.md` — the Visual FoxPro lineage
- `docs/CLIPPER_ORACLE.md` — what has been verified against
  Clipper 5.2e output
- `docs/RESEARCH_NOTES.md` — the IDX probe, and which drivers
  generate which index formats

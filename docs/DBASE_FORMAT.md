# dBASE table and memo formats

Version: 0.9.25
Last reviewed: 2026-07-23

## Provenance

Restated from Borland Technical Information document **TI838D**,
"dBASE .DBF File Structure" (Category: Database Programming;
Platform: All; Product: Delphi All), dated 1998-07-16. The
document itself credits four vendor manuals as its sources:

| section | credited to |
| ------- | ----------- |
| dBASE III PLUS 1.1 | *Using dBASE III PLUS*, Appendix C |
| dBASE IV 2.0 | *dBASE IV Language Reference*, Appendix D |
| dBASE 5.0 for DOS | *dBASE for DOS Language Reference*, Appendix C |
| dBASE 5.0 for Windows | *dBASE for Windows Language Reference*, Appendix C |
| dBASE 7 (via S6) | dbase.com Knowledge Base, `db7_file_fmt.htm` — original 404s; mirrored at `autopark.ru/ASBProgrammerGuide/dbf7stru.htm` |

**Source S13 — a live write-oracle**: real dBASE 5.0 for DOS, run
under DOSBox on the user's own machine 2026-07-24 (headless
automation in the sandbox never succeeded — dBASE 5 is a Turbo
Vision IDE with no dot-prompt, unlike III+/IV, and genuinely needs
interactive keyboard input). Output vendored at
`dbf/testdata/dbase5/oracle/`. The strongest form of evidence this
project uses — not a document, not a specimen found in the wild,
but bytes generated on demand by the real product, with predictions
stated and checked against them *before* looking.

Confirmed exactly as predicted: version byte `0x8B`, byte 28
`0x01`, byte 29 `0x1B` (independently matching the 1994 vendor
specimens). **Falsified a documented assumption**: field-descriptor
byte 31 does not track current `.MDX` tag membership — see the
correction in the dBASE IV section below. **Found something new**:
dBASE IV/5's `.DBT` blocks carry an 8-byte header dBASE III PLUS's
never had, with a header-inclusive length field — see the same
section.

**Source S12 — Borland's own internal manuscript**, `AC_LREF.DWD`,
recovered 2026-07-24 from a FrameMaker source file embedded in a
German shareware compilation CD (`archive.org/download/dbase`,
`dbase.iso`, path `DBASE/BUCH/AC_LREF.DWD`). The file itself
carries the header **"BORLAND CONFIDENTIAL, Copyright © 1994,
Borland International. All rights reserved"**, dated 27 October
1994, with internal path `e:\dbase5\buch\ac_lref.dwd` — Appendix C,
"Dateistrukturen" (File Structures), of the *dBASE for Windows
5.0* Language Reference, in German. This is very likely the actual
document TI838D's own "dBASE 5.0 for Windows" section cites as its
source; the wording matches closely enough to be the same original
text, independently translated back rather than paraphrased.

Format note for whoever revisits this: FrameMaker's binary format
interleaves plain paragraph text with structural markup, so
`strings -n 6` on the raw `.DWD` file recovers the prose directly
— no FrameMaker license or converter needed.

**This settles T-31's central open risk.** Stated in Borland's own
words, unambiguously, for both letters:

> **B (Binär)** — Alle in den OEM-Code-Seiten enthaltenen Zeichen.
> (Interne Speicherung als 10 Ziffern, die die Nummer eines
> .DBT-Blocks angeben.)
>
> **G (OLE)** — Alle in den OEM-Code-Seiten enthaltenen Zeichen.
> (Interne Speicherung als 10 Ziffern, die die Nummer eines
> .DBT-Blocks angeben.)

Both `B` and `G` are **10-digit ASCII `.DBT` block pointers** in
the dBASE-for-Windows/5 lineage — matching TI838D exactly, and
directly contradicting S11's unexercised `G`→`Double` mapping.
Field type list given is `B, C, D, F, G, L, M, N` — no `T`,
confirming DateTime is not part of this lineage at all, consistent
with everything else found this session.

Two further findings not previously recorded anywhere:

- **Byte 14 (incomplete-transaction flag) is not used by dBASE
  for Windows at all.** Stated directly: dBASE IV sets it via
  `BEGIN TRANSACTION`/clears it via `END TRANSACTION`/`ROLLBACK`;
  dBASE for Windows doesn't touch it. A dBASE-for-Windows-written
  file should always carry `0x00` here, though a table inherited
  from dBASE IV could still carry a stale `0x01`.
- **Byte 15 (encryption) is not supported by dBASE for Windows.**
  `0x01` there is meaningful only for dBASE IV tables, where it
  indicates the table is encrypted.

And one independent confirmation of something T-30/T-31 already
believed from TI838D: the memo block-reuse behaviour. Stated
directly — *"dBASE für Windows [reuses], like dBASE IV, freed
space for new data"*, contrasted explicitly with dBASE III PLUS,
which *"always appended changed text to the end of the .DBT
file."* Same fact, now from the primary manuscript rather than a
secondary restatement of it.

**Source S11 — `github.com/artnum/BizCuit-DBase`**, MIT-licensed C
source (`dbase.c`, `dbase.h`, `memo.c`), read 2026-07-24. Not a
specification — a real, independent implementation reading actual
dBASE5 files for Winbiz, a currently-operating Swiss accounting
product (`bizcuit.ch`). Two findings, of very different reliability:

- **Useful and load-bearing for T-31.** The version byte is
  decoded as `version = byte0 & 0x07`, `memo_file = byte0 >> 7` —
  nothing else. The SQL-table bits (4–6) that distinguish `0x43`
  and `0x63` from `0x03` are never inspected anywhere in the code.
  This is real, shipped, exercised parsing logic against real
  production files, and it says plainly that field-descriptor and
  record layout do not need to special-case `0x43`/`0x63` at all
  — they parse exactly like `0x03`/`0x83` once the version bits
  are read correctly. Directly simplifies T-31, and specifically
  de-risks `0x63`, the version byte with no primary-source
  description anywhere else.
- **Checked and set aside: the `G` field-type mapping.** S11's
  type table maps `G` to an internal `DOUBLE` code, which would
  contradict TI838D's own `General`/OLE-pointer meaning for the
  dBASE lineage. Traced through the source before trusting it:
  the per-record decode switch has explicit cases for `MEMO`,
  `DATE`, `INTEGER`, `FLOAT`, `CHAR` — **no case for `DOUBLE` at
  all**. The mapping is declared and referenced in two coercion
  helpers, but nothing ever reads bytes into it. Unexercised code,
  not evidence from a tested file. Checked against blipper's own
  33 vendored dBASE 5.0 specimens for a `G` or `B` field to settle
  it independently either way — none exist in any of them. The
  `B`/`G` lineage question T-31 already named as its central risk
  remains exactly as open as it was.

**Corporate lineage.** dBASE originated at Ashton-Tate, which
Borland acquired in 1991. Borland became Inprise and then Borland
again; its developer tools passed to Embarcadero Technologies in
2008, which is why a Borland TI number is now Embarcadero
material. dBASE itself was divested separately and continues
under dBASE LLC. **dBASE 5.0 was the first Windows release** —
everything before it was DOS.

This file is blipper's own restatement of the byte layouts, not a
copy of the source document. Layouts are facts; the wording here
is ours. Where the source is ambiguous or where we have verified
something against real files, that is marked.

**Durability: moderate.** Borland TI documents survive on
mirrors of varying quality. The underlying manuals are long out
of print.

---

## Why this matters to blipper

blipper implements dBASE III PLUS (`0x03`, `0x83`) and reads
FoxPro/VFP variants. The dBASE IV and 5.0 lineages diverge from
III PLUS in ways that affect what blipper can safely claim:

- **Byte 28 means something different.** In III PLUS it is
  reserved; from dBASE IV it is the production `.MDX` flag. VFP
  later reused the same byte for its own flag set. A reader that
  assumes one meaning across the family will misread files.
- **Field descriptor byte 31 does not mean what it looks like it
  means.** Documented and long assumed as "this field has a tag
  in the production MDX." A live write-oracle test (real dBASE
  5.0 for DOS, 2026-07-24) falsifies that: a field indexed via a
  standalone `INDEX ON ... TAG` command after table creation has
  a genuinely working tag in the production `.MDX` — confirmed by
  decoding the tag directory directly — while its byte 31 stays
  `0x00`. The flag appears to reflect something set at table
  *creation* (plausibly the structure designer's own "Index"
  column) rather than current tag membership. blipper must not
  use byte 31 to decide whether a field is indexed; the `.MDX`
  tag directory is the only reliable source, which is what
  blipper's own MDX-reading code already treats as authoritative.
- **Memo block size stopped being fixed.** III PLUS memo blocks
  are always 512 bytes; from dBASE IV, `SET BLOCKSIZE` controls
  it, which is the same problem FPT solved differently.

---

## dBASE III PLUS 1.1

### Header

| offset | size | contents |
| ------ | ---- | -------- |
| 0 | 1 | Version: `0x03` without a memo file, `0x83` with a `.DBT` |
| 1–3 | 3 | Last update, YYMMDD |
| 4–7 | 4 | Record count (32-bit) |
| 8–9 | 2 | Header length in bytes (16-bit) |
| 10–11 | 2 | Record length in bytes (16-bit) |
| 12–14 | 3 | Reserved |
| 15–27 | 13 | Reserved for LAN use |
| 28–31 | 4 | Reserved |
| 32–n | 32 each | Field descriptor array |
| n+1 | 1 | Field terminator, `0x0D` |

Note that **bytes 28–31 are wholly reserved here.** The later
meanings of byte 28 (production MDX in dBASE IV, table flags in
VFP) do not apply to a genuine III PLUS file.

### Field descriptor, 32 bytes

| offset | size | contents |
| ------ | ---- | -------- |
| 0–10 | 11 | Field name, ASCII, zero-filled |
| 11 | 1 | Field type: `C`, `D`, `L`, `M`, `N` |
| 12–15 | 4 | Field data address |
| 16 | 1 | Field length |
| 17 | 1 | Decimal count |
| 18–19 | 2 | Reserved for LAN use |
| 20 | 1 | Work area ID |
| 21–22 | 2 | Reserved for LAN use |
| 23 | 1 | SET FIELDS flag |
| 24–31 | 8 | Reserved |

**On bytes 12–15.** The source describes this as a data address
set in memory and explicitly notes it is *not useful on disk*.
That is worth contrasting with the VFP lineage, where the same
bytes hold the field's displacement within the record and are
meaningful — see `docs/VFP30_FORMAT.md`. blipper computes offsets
by summing widths, which is correct for both.

This also explains an observation from the Clipper corpus: field
descriptor bytes 12–15 carry apparently random values (`99190000`
in `UM.DBF`) rather than zeros. Those are stale in-memory
addresses, exactly as the source describes, not corruption.

### Records

A record is preceded by one byte: `0x20` if live, `0x2A` if
deleted. Fields are packed with no separators and no record
terminator. The file ends with a single `0x1A`.

### Types

| code | type | accepted input |
| ---- | ---- | -------------- |
| `C` | Character | any OEM code page characters |
| `D` | Date | stored as 8 digits, `YYYYMMDD` |
| `N` | Numeric | digits, `-`, `.` |
| `L` | Logical | `? Y y N n T t F f`; `?` when uninitialised |
| `M` | Memo | stored as 10 digits, a `.DBT` block number |

### `.DBT` memo files

Blocks are numbered from zero and are **fixed at 512 bytes** in
III PLUS. Block 0 is the file header. A record's memo field holds
the block number in OEM code page digits; an empty field holds
spaces (`0x20`) rather than a number.

**III PLUS never reclaims space.** The source is explicit: it
always appends new text to the end of the file, so a `.DBT` grows
whenever text is added even if other text was deleted. That is
the behaviour blipper's memo compaction (T-18, v0.8.1) exists to
undo.

---

## dBASE IV 2.0

### Header — what changed from III PLUS

| offset | size | contents |
| ------ | ---- | -------- |
| 0 | 1 | Version, now **bit-encoded** (see below) |
| 12–13 | 2 | Reserved, zero-filled |
| **14** | 1 | **Incomplete transaction flag** |
| **15** | 1 | **Encryption flag** |
| 16–27 | 12 | Reserved for multi-user |
| **28** | 1 | **Production `.MDX` flag**: `0x01` if an MDX exists |
| **29** | 1 | **Language driver ID** |
| 30–31 | 2 | Reserved, zero-filled |

Bytes 1–11 and the field-terminator convention are unchanged.

**The version byte becomes a bit field.** Bits 0–2 are the
version number, bit 3 indicates a dBASE IV memo file, bits 4–6 an
SQL table, and bit 7 any memo file at all (III PLUS or IV). This
is why `0x8B` means "dBASE IV with memo" — bit 7 set for the memo
plus the IV version in the low bits.

**Byte 29 is the language driver**, the same byte blipper reads as
the code page mark (T-21, v0.8.3).

**But the identifier numbering is not shared with FoxPro**, which
this document previously implied. Specimens from Borland's dBASE
5.0 for DOS distribution carry `0x1B` in byte 29, and that value
is outside the Visual FoxPro code page table entirely — that table
runs from `0x01` to `0xCB` with no `0x1B` in it. So the dBASE IV
lineage numbers its language drivers differently, and byte 29
cannot be interpreted with a single table across the whole family.

blipper currently maps byte 29 through the VFP table only. A
dBASE IV-lineage file declaring `0x1B` is therefore reported as an
unsupported code page rather than misdecoded, which is the safe
failure, but the mapping is incomplete. No dBASE language-driver
table has been found; see `dbf/testdata/dbase5/`.

### Field descriptor — what changed

| offset | size | contents |
| ------ | ---- | -------- |
| 12–15 | 4 | **Reserved** — no longer a data address |
| 21–30 | 10 | Reserved |
| **31** | 1 | **Set at table creation, meaning not fully understood — see the correction below the field table.** Documented as "`0x01` if this field has an index tag in the production MDX," but oracle-falsified 2026-07-24: does not track tags added after creation via `INDEX ON ... TAG`. |

Byte 20 remains the work area ID. The `SET FIELDS` flag at byte
23 in III PLUS is absorbed into the reserved range.

### Types

`F` (floating-point binary numeric) joins the III PLUS set. The
source distinguishes it from `N`, which it describes as binary
coded decimal — both accept the same input characters.

### Memo files

Block size is **no longer fixed**: `SET BLOCKSIZE` controls it.
Block 0 remains the header.

**"Everything else follows III PLUS" is wrong — corrected
2026-07-24 against a live write-oracle (real dBASE 5.0 for DOS).**
III PLUS memo blocks have no per-block header at all: raw text,
terminated by `0x1A 0x1A`. dBASE IV/5 blocks carry one, present
even at the default block size (not only when `SET BLOCKSIZE` is
customised):

| offset | size | contents |
| ------ | ---- | -------- |
| 0–3 | 4 | Constant marker, observed as bytes `FF FF 08 00` in every block checked |
| 4–7 | 4 | Length, little-endian — **not the text length: 8 (this header) plus the text length** |
| 8– | — | Text, NUL-padded to fill the rest of the block |

Confirmed against two real blocks: a 15-byte memo gave length
field `23` (`8+15`); a 6-byte memo gave `14` (`8+6`). Decoding
this as a plain content-length field, the FPT convention, would
be silently wrong by exactly 8 bytes in either direction depending
on which side of the mistake the code lands on.

**Field-level byte 31 is separately unreliable — see the table
above.**

---

## dBASE 5.0 for DOS

The first release to carry the "5.0" numbering; still DOS.

Header and field descriptor are **identical to dBASE IV 2.0**,
including byte 28 as the production MDX flag. Byte 31 exists in
the same position but its meaning is unreliable — see the
dBASE IV section's correction, which applies here unchanged since
this is confirmed the same field layout. The version byte keeps
the same bit encoding, though the source describes it in terms of
"dBASE for Windows" naming even in the DOS section — an artifact
of the two products sharing a lineage.

### What changed: memo space reuse

The one substantive difference the source draws out:

> Unlike dBASE III PLUS, if you delete text in a memo field,
> dBASE 5.0 for DOS may reuse the space from the deleted text
> when you input new text.

So a 5.0 `.DBT` does not grow monotonically the way a III PLUS
one does. For blipper this matters to memo compaction: a file
written by 5.0 may already contain reused blocks, so orphan
counts are not a reliable measure of how much a compaction will
recover.

### Types

`B` (binary) and `G` (general/OLE) join the set alongside `F`.
Both are stored as 10-digit `.DBT` block numbers, the same
representation as `M`.

**Contrast with VFP.** Visual FoxPro also has `B` and `G`, but
means something different by them: VFP's `B` is an 8-byte IEEE
double and its `G` is a 4-byte binary block pointer. The dBASE
`B` is a memo-style 10-digit ASCII pointer. **The same letter
means different things in the two lineages**, which a reader
dispatching on type code alone would get wrong. blipper
distinguishes them by version byte.

---

## dBASE 5.0 for Windows

**The first Windows release of dBASE.**

Header and field descriptor are the same as 5.0 for DOS. The
source's byte-0 description names dBASE-for-Windows memo files
explicitly in the bit meanings, but the layout is unchanged.

### Types

Same set as 5.0 for DOS: `B`, `C`, `D`, `F`, `G`, `L`, `M`, `N`.
The source gives `B` (Binary) and `G` (General or OLE) their own
entries in the input table, both stored as 10-digit `.DBT` block
numbers.

### Memo files

Space reuse as in 5.0 for DOS, extended to binary and OLE fields.

**The 8-byte per-block header found via S13's live write-oracle
(dBASE 5.0 for DOS — see the dBASE IV section above) has not been
directly verified for the Windows variant**, since no Windows
`.DBT` specimen exists. Given S12 states the two products share an
identical byte-for-byte format, inheriting the same block header
is the reasonable default assumption — but it is an inference from
a DOS-side test, not something checked against Windows output.
The source notes this applies "unlike dBASE IV" as well as unlike
III PLUS — so dBASE IV appended monotonically like III PLUS, and
only the 5.0 generation reclaims.

---

## dBASE 7

**Source S6** — dbase.com's own Knowledge Base article
`db7_file_fmt.htm`. The original is 404 as of 2026-07-24; this
section is restated from a mirror at
`autopark.ru/ASBProgrammerGuide/dbf7stru.htm`, itself a copy of
the vendor page rather than an independent description. Durability
assessment: **poor** — one unofficial mirror is now the only known
copy of the *primary* source, since the vendor's own page is gone.

This is a materially different format from III PLUS through 5.0,
not an incremental extension. Three structural changes:

### Header — extended

Same base layout through byte 31, then:

| offset | size | contents |
| ------ | ---- | -------- |
| 32–63 | 32 | Language driver **name** (not just the ID at byte 29) |
| 64–67 | 4 | Reserved |
| 68–n | 48 each | Field descriptor array — **48 bytes, not 32** |
| n+1 | 1 | `0x0D` terminator, as elsewhere |
| n+2– | — | **Field Properties Structure** (new) |

### Field descriptor — 48 bytes, not 32

| offset | size | contents |
| ------ | ---- | -------- |
| 0–31 | 32 | Field name — **32 bytes, not 11** |
| 32 | 1 | Field type: `B C D N L M @ I + F O G` |
| 33 | 1 | Length |
| 34 | 1 | Decimals |
| 35–36 | 2 | Reserved |
| 37 | 1 | Production MDX field flag (was byte 31 in earlier versions) — **inherit with caution**: the earlier-version byte this succeeds was oracle-falsified 2026-07-24 as not reliably tracking current tag membership. Unverified whether dBASE 7 fixed this or carried the same behaviour forward. |
| 38–39 | 2 | Reserved |
| 40–43 | 4 | Next autoincrement value (`+` type only) |
| 44–47 | 4 | Reserved |

### Field Properties Structure — new in dBASE 7

A header plus three variable-length descriptor arrays, immediately
following the field-descriptor terminator: **Standard Properties**
(required/min/max/default/database-constraint, keyed by field
number), **Custom Properties** (arbitrary name/value pairs), and
**Referential Integrity Properties** (parent/child table links,
cascade behaviour, linking keys). None of blipper's `dbc` work
touches this — RI in dBASE 7 is stored in the table itself, where
VFP stores it in the `.dbc` container.

### Two new field types, with a real epoch

| code | type | encoding |
| ---- | ---- | -------- |
| `@` | **Timestamp** | 8 bytes, two 32-bit longs. First is the **date as days since 1 January 4713 BC** (Julian day number, stated explicitly). Second is **milliseconds since midnight**: `hours×3600000 + minutes×60000 + seconds×1000`. |
| `I` | Long | 4 bytes, sign in the leftmost bit |
| `+` | Autoincrement | same encoding as `I` |
| `O` | Double | 8 bytes, stored directly as a double — no conversion |

**This is the first primary-source epoch value found for any
dBASE-lineage datetime field**, dBASE 7's `@` rather than VFP's
`T`. They are not necessarily the same encoding — different
products, a decade apart — but a stated Julian-day epoch is
concrete enough to be worth checking against VFP's `T` before
assuming they differ. If they match, T-25's DateTime gap (blocked
all session on VFP's undocumented epoch) may be resolvable without
a VFP specimen at all.

### Storage note also worth carrying forward

S6 states plainly: *all types initialise to binary zero, except
autoincrement fields, and any field with a default property takes
that default.* Not stated in TI838D for the earlier versions.

---

## Comparison across the family

Byte 28, which blipper cares about because VFP reuses it:

| product | byte 28 |
| ------- | ------- |
| dBASE III PLUS | reserved |
| dBASE IV | production MDX flag |
| dBASE 5.0 DOS/Windows | production MDX flag |
| FoxPro 2 / VFP | table flags: `0x01` CDX, `0x02` memo, `0x04` DBC |
| blipper's DBC pairing | `0x0C` — VFP DBC bit plus a reserved provenance bit (T-10) |

Memo block size:

| product | block size |
| ------- | ---------- |
| dBASE III PLUS | fixed 512 |
| dBASE IV onward | `SET BLOCKSIZE` |
| FoxPro `.FPT` | stored in the FPT header, big-endian |

Memo space reuse:

| product | on delete |
| ------- | --------- |
| dBASE III PLUS | never reclaims; file grows monotonically |
| dBASE IV | as III PLUS |
| dBASE 5.0 DOS/Windows | may reuse deleted space |
| FoxPro `.FPT` | appends; orphans accumulate |

---

## Specimens

Borland's dBASE 5.0 for DOS distribution (1994) provides the first
dBASE-lineage specimens this project has held. Eight are staged at
`dbf/testdata/dbase5/` with provenance and per-file notes; the
full distribution carried 33 DBF files across four version bytes,
15 MDX indexes, 1 NDX, and 3 DBT memos.

Three claims in this document are now backed by files rather than
by the source document alone:

- **Byte 28 as the production MDX flag** — `0x01` on every table
  with an MDX sibling, `0x00` on every table without.
- **Byte 31 as the per-field tag flag** — in `ACCT_REC.DBF` the
  three flagged fields are exactly the three tags in
  `ACCT_REC.MDX`, so the descriptor and the index agree.
- **The MDX layout itself**, which decoded correctly on first
  attempt against `docs/INDEX_FORMATS.md`.

Two findings the document did not previously record are noted
above: the byte-29 numbering divergence, and version byte `0x63`,
which eleven files carry and which TI838D does not mention at all.

## What blipper does and does not implement

| product | blipper | specimens |
| ------- | ------- | --------- |
| dBASE III PLUS 1.1 | ✓ read and write, oracle-verified against Clipper 5.2e | 137-file corpus |
| dBASE IV 2.0 | ✓ table read+write (read: v0.9.21, T-31; write: v0.9.25, T-38 — dBASE IV 2.0 and dBASE 5.0 for DOS share this format byte-for-byte, so `CreateDBaseIV` covers both); `.NDX`/`.MDX` ✓ (T-28/T-30, Character/Date verified, Numeric bounded) | ✓ dBASE 5.0 DOS media, plus a live write-oracle (S13, real dBASE 5.0 output) |
| dBASE 5.0 DOS | ✓ table read+write, all three version bytes `0x8B`/`0x43`/`0x63` (read: v0.9.21, T-31; write: v0.9.25, T-38, `CreateDBaseIV`) — oracle-verified against 33 real specimens plus a live write-oracle; `B`/`G` lineage dispatch, the 8-byte `.DBT` memo header, and byte-31 non-reliance all verified against real data, not just documented. `.DBT` memo write shipped v0.9.23 (**T-37**). `.NDX`/`.MDX` ✓ as above | ✓ 33 tables, 15 MDX, plus S13's live oracle output |
| dBASE 5.0 Windows | ✗ — **but the format itself is fully known**, not just undocumented. S12 is literally titled for the Windows-version manuscript and states the header/field descriptor/type layout is byte-identical to 5.0 DOS, confirmed now against real 5.0 DOS read support. What remains blocked is specimen *access*: samples sit inside Borland's `DS\0Z` installer container, which five extractors (`7z`, `lhasa`, `unar`, `cabextract`, `borpak`) all failed against. A DOS-DOS format problem, not a format-knowledge problem. | container not extractable |
| dBASE 7 | ✗ — 32-byte field names, 48-byte descriptor, RI properties | S6, zero specimens |

**On `0x63` specifically**, which had no primary-source description for most of this session: S11 (a real, shipped C reader, tested against a currently-operating product's production files) decodes the version byte as `version = byte0 & 0x07`, `memo = byte0 >> 7` only — the SQL bits (4–6) distinguishing `0x43`/`0x63` from `0x03` play no role in field-descriptor or record parsing. Field layout for `0x63` needs no separate handling from `0x03`/`0x83`, independent of whatever `0x63`'s SQL-system-table semantics actually are at the application level.

Note that the dBASE 5.0 DOS media supplies specimens for the
dBASE IV lineage as well, since `0x8B` and the SQL variants are
shared between them. What blocks these versions is now
implementation rather than evidence.

The gap is `.MDX`. blipper reads no dBASE IV-lineage index, and
byte 28's production-MDX flag is not interpreted. `DBFMDX.LIB`
exists in the Clipper toolchain and would serve as an oracle; see
the register.

---

## Cross-references

- `docs/VFP30_FORMAT.md` — the FoxPro/VFP lineage, where byte 28
  and the `B`/`G` type codes diverge from dBASE
- `docs/CLIPPER_ORACLE.md` — the Clipper 5.2e harness and what it
  has verified
- `docs/DBASE_HISTORY.md` — the origin story: Vulcan, JPL,
  Ashton-Tate
- `docs/FAMILY_COMPATIBILITY.md` — the support matrix across both
  lineages

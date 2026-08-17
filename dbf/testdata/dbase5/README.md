# dBASE 5.0 for DOS specimens

Borland dBASE 5.0 for DOS, 1994. Sample data and system tables
from the product distribution, obtained 2026-07-23.

Unlike the Windows 5.0 media — where the samples sit inside
Borland's `DS\0Z` installer container that no available extractor
opens — these files are plain on disk.

## Why these eight

Chosen to cover every distinct thing the distribution
demonstrates, at the smallest size that does so. 44 KB total.

| file | bytes | version | demonstrates |
|------|-------|---------|--------------|
| `ACCT_REC.DBF` | 1347 | `0x03` | byte 28 = `0x01` production MDX flag; byte 31 per-field tag flags |
| `ACCT_REC.MDX` | 8192 | — | **three tags in one file**: character, numeric, character |
| `CLIENT.DBF` | 1730 | `0x8B` | dBASE IV/5 with memo — a version byte blipper does not accept |
| `CLIENT.DBT` | 4096 | — | dBASE IV-lineage memo, `SET BLOCKSIZE` rather than fixed 512 |
| `CLIENT.MDX` | 6144 | — | two tags |
| `ASSEMBLY.DBF` | 355 | `0x43` | dBASE IV SQL table — byte 28 and 29 both `0x00` |
| `STAFF.DBF` | 1143 | `0x43` | second SQL-variant sample, with a Date field |
| `CUS_NAME.NDX` | 1024 | — | standalone NDX from the dBASE lineage, for cross-checking the `ndx` package against a non-Clipper writer |

## What the full distribution contained

33 DBF files across four version bytes, 15 MDX, 1 NDX, 3 DBT.

| version | count | product |
|---------|-------|---------|
| `0x03` | 13 | dBASE III+ compatible, no memo |
| `0x43` | 6 | dBASE IV SQL table |
| `0x63` | 11 | dBASE IV SQL system |
| `0x8B` | 3 | dBASE IV / 5 with memo |

## Findings

**MDX decoded correctly on first attempt** against the layout in
`docs/INDEX_FORMATS.md`. Every field landed where documented: the
data file name at offset 4, block size at 20, production flag at
24, the 48-entry tag table with 32-byte entries beginning at 544.
`ACCT_REC.MDX` yields three tags — `CUST_ID` (C), `OLDBALANCE`
(N), `INVOICE_NO` (C) — each with its own root page. That is the
multi-tag structure T-30 needs, observed rather than inferred.

**Byte 28 behaves exactly as documented.** `0x01` on the tables
that have an MDX sibling, `0x00` on those that do not. Directly
usable as a test rather than a claim.

**Byte 31 flags precisely the indexed fields.** In `ACCT_REC.DBF`
the fields with a `1` are `CUST_ID`, `INVOICE_NO`, and
`OLDBALANCE` — which are exactly the three tags in the MDX. The
per-field flag and the tag directory agree, which is a
cross-check neither alone would give.

**Byte 29 = `0x1B` across the dBASE-lineage files.** That is
outside the Visual FoxPro code page table entirely, which runs to
`0xCB`. So the dBASE IV lineage numbers its language drivers
differently from FoxPro, and byte 29 cannot be read with one
table family-wide. `docs/DBASE_FORMAT.md` previously implied
otherwise.

**`0x63` is not in the Borland TI838D document.** Eleven files
carry it. The only description found is Bachmann's, which lists it
as dBASE IV SQL system files. A version byte with specimens in
hand and no primary-source description.

**`_DBASELOCK` appears as the last field** in both `ACCT_REC` and
`CLIENT` — the record-lock field described in
`docs/INDEX_FORMATS.md`, holding a change counter, lock time, lock
date, and user name. This is the first specimen of it.

## The `full/` subdirectory

The eight files above are the working fixture set — the smallest
selection that covers each distinct thing the distribution
demonstrates.

`full/` holds the complete data set: **33 DBF, 15 MDX, 1 NDX, and
3 DBT, 300 KB in total.**

It is vendored rather than referenced because **it cannot be
re-fetched.** These files arrived as a user upload, not from a
URL, and no equivalent source has been found — the dBASE 5.0 for
*Windows* media on the Internet Archive keeps its samples inside
Borland's `DS\0Z` installer container, which five extractors were
tried against without success (see `docs/RESEARCH_NOTES.md`).

Losing them would re-block two register items. T-30 (MDX) had a
documented layout and no evidence at all until these arrived, and
T-31 (dBASE IV/5 tables) did not exist as an item because those
versions looked unverifiable.

Version bytes in the full set:

| byte | count | product |
|------|-------|---------|
| `0x03` | 13 | dBASE III+ compatible, no memo |
| `0x43` | 6 | dBASE IV SQL table |
| `0x63` | 11 | dBASE IV SQL system |
| `0x8B` | 3 | dBASE IV / 5 with memo |

Nothing in `full/` is referenced by any test. It is an archive,
and the working fixtures sit alongside it deliberately so that a
reader can tell the two apart.

## Licensing

Fragments of Borland's 1994 distribution, retained as format
specimens under the same reasoning as the VFP 3.0 material in
`dbf/testdata/vfp/`: these are sample and system tables
demonstrating file structure, not application code. If that
judgement is wrong they should be removed; the findings above are
recorded in `docs/DBASE_FORMAT.md` and survive without them.

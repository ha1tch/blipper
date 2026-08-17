# NDX oracle fixtures

Generated 2026-07-23 by Clipper 5.2e's `DBFNDX` driver under the
DOSBox harness described in `docs/CLIPPER_ORACLE.md`. The same
harness produced the CDX and FPT fixtures.

The generating program created a two-field table — `CODE C(10)`
and `NUM N(8,2)` — appended five records in deliberately unsorted
order, then built two indexes:

    INDEX ON CODE TO BYCODE
    INDEX ON NUM  TO BYNUM

| record | CODE | NUM |
|--------|---------|----|
| 1 | DELTA | 40 |
| 2 | ALPHA | 10 |
| 3 | CHARLIE | 30 |
| 4 | BRAVO | 20 |
| 5 | ECHO | 50 |

Insertion order is not sorted order, so an index that merely
preserved append order would pass a naive test and fail these.

## What they demonstrate

`BYCODE.NDX` is a character index: key length 10, record size 20,
25 keys per page. `BYNUM.NDX` is numeric: key length 8, record
size 16, 31 keys per page. Both are 1024 bytes — a header page
plus a single root page holding all five entries.

The pair matters because the two key types are compared
differently. Character keys are unsigned byte comparisons;
numeric keys are IEEE-754 doubles that must be decoded before
comparison. A single character fixture would not catch an
implementation that compared numeric keys as bytes.

## Header values, decoded

| field | BYCODE | BYNUM |
|-------|--------|-------|
| root page | 1 | 1 |
| total pages | 2 | 2 |
| key length | 10 | 8 |
| keys per page | 25 | 31 |
| key type | 0 (character) | 1 (numeric) |
| key record size | 20 | 16 |
| unique flag | 0 | 0 |
| key expression | `CODE` | `NUM` |

Both record sizes match the documented rule — 4 bytes of
lower-level pointer, 4 of record number, and the key rounded up
to a multiple of four.

`TESTGEN.DBF` / `TESTGEN.MDX` / `TESTGEN.DBT`, generated
2026-07-24 by real dBASE 5.0 for DOS running under a locally-run
DOSBox instance — a genuine write-oracle, not a specimen found in
the wild. Table created interactively via the structure designer
(`CREATE testgen`, fields `ID N(4,0)`, `NAME C(10)`, `NOTES M`),
then populated and indexed via `DO GENTEST.PRG`:

```
SET TALK OFF
USE testgen
APPEND BLANK
REPLACE ID WITH 1, NAME WITH "ALPHA", NOTES WITH "first memo note"
APPEND BLANK
REPLACE ID WITH 2, NAME WITH "BRAVO", NOTES WITH "second"
INDEX ON NAME TAG NAMETAG
CLOSE ALL
QUIT
```

Vendored because it cannot be regenerated without a working
headless dBASE 5.0 setup, which this session never achieved —
dBASE 5 for DOS is a Turbo Vision IDE with no dot-prompt, unlike
III+/IV, and needed real interactive keyboard input.

## What this specimen settled

**Confirmed exactly as predicted, before looking**: version byte
`0x8B`, byte 28 `0x01`, byte 29 `0x1B` (matching the original 1994
vendor specimens independently — same value, different dBASE 5
installation, decades apart).

**Falsified a documented assumption.** `NAME`'s field-descriptor
byte 31 is `0x00` despite `NAMETAG` being a real, working tag on
that field — confirmed by decoding the `.MDX` tag directory
directly (both records present, correct keys and record numbers).
Byte 31 does not reliably indicate current tag membership; it
appears to reflect something fixed at table creation rather than
updating when `INDEX ON ... TAG` adds a tag afterward. See
`docs/DBASE_FORMAT.md`'s dBASE IV section for the full correction.

**Found something new, not just confirmed something old.** dBASE
IV/5's `.DBT` memo blocks carry an 8-byte header — a constant
4-byte marker (`FF FF 08 00`) plus a 4-byte little-endian length
field — that dBASE III PLUS's headerless format never had. The
length field is header-inclusive (8 + text length), not
content-only: verified against two blocks (15-byte text → length
23; 6-byte text → length 14). Present at the default block size,
not only under a customised `SET BLOCKSIZE`.

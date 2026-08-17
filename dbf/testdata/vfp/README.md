# Visual FoxPro 3.0 specimens

Extracted from Microsoft's own VFP 3.0 installation media, 2026-07-23:

    https://archive.org/download/ms-vfp30/VFP30US%20%28Standard%29.ISO
    7z x -oiso 'VFP30US (Standard).ISO'
    7z x -ocab iso/VFP1.CAB

The ISO is 21,161,984 bytes, ISO 9660, volume label `VFP30US`, files
dated 1995-06-21. The 13 CAB archives are a spanning set — extracting
`VFP1.CAB` alone yields all 700 files.

Of those, 134 are DBF-format: 129 with version byte `0x30` (Visual
FoxPro) and 5 with `0xF5` (FoxPro 2.x with FPT). These three are the
smallest that demonstrate something specific.

| file | bytes | version | records | what it is |
|------|-------|---------|---------|------------|
| `30DBC.DBF` | 1529 | `0x30` | 8 | Microsoft's field-by-field spec of the DBC schema |
| `30PJX.DBF` | 8489 | `0x30` | 26 | VFP 3.0 project-file spec |
| `26PJX.DBF` | 6574 | `0x30` | 31 | FoxPro 2.6 counterpart, for comparison |

`30DBC.DBF` is the useful one: its eight records *are* the DBC column
definitions, and they confirm the schema blipper implemented in T-10
from secondary sources — `OBJECTID I(4)`, `PARENTID I(4)`,
`OBJECTTYPE C(10)`, `OBJECTNAME C(128)`, `PROPERTY M`, `CODE M`,
`RIINFO C(6)`, `USER M`.

## What these do not demonstrate

**No nullable or system columns.** Field-descriptor byte 18 is `0x00`
in every field of all 129 VFP files in the distribution. These are
VFP's own metadata tables and Microsoft had no reason to make their
columns nullable, so the distribution does not exercise `_NullFlags`
at all.

**No DateTime fields.** The type appears in the documentation and in
no file here, so the epoch convention cannot be checked against data.

Both gaps are recorded in `docs/VFP30_FORMAT.md`. Closing them needs a
table *created* by running VFP, not one found by reading its
distribution.

## Licensing

These are fragments of Microsoft's 1995 installation media, retained
as format specimens under fair-use/research reasoning. They are
metadata tables describing file structures, not application code or
user data. If that judgement is wrong they should be removed and the
`docs/VFP30_FORMAT.md` reproduction steps used instead — everything
here can be re-derived from the ISO in two commands.

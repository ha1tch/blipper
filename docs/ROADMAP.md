# Roadmap

Version: 0.9.25
Last reviewed: 2026-07-23

## What this is for

`docs/TRACKING.md` records what is open. This records what order
and why, which is a different question — several items are cheap
and low-value, others expensive and load-bearing, and the register
deliberately does not express that.

Nothing here is a commitment. It is the current reading of what
would be worth doing next and what each thing costs.

If you are resuming work after a break, read `STATUS.md` at the
repository root first. It carries the build invocation, the
ephemeral state that does not survive a session boundary, the
traps worth knowing before touching anything, and a recommended
next action.

## Where the project stands

| | |
| --- | --- |
| Version bytes accepted | 4 of 16 |
| Field types | 14 of 16 |
| Memo formats | complete (DBT, FPT) |
| Index formats | 3 of 5 (NTX, NDX, CDX) |
| Storage backends | 3 (directory, FAT16/32 image, SQLite tablespace) |
| Tests | 226 across 9 packages |

The four accepted version bytes cover every file in the 137-file
Clipper corpus, every shapefile sidecar sampled from four
independent producers, and all 136 files in Microsoft's VFP 3.0
distribution. Coverage is narrow by count and broad by incidence.

## The constraint that shapes everything

**No format is implemented without a way to verify it.** That rule
has caught real defects three times: the FPT block-numbering bug
in v0.4.0, which passed its round-trip tests because both halves
were wrong identically; the CDX rebuild in v0.7.0; and the
code-page encode path in v0.8.3.

So an item's cost is dominated not by the code but by whether an
oracle or specimen exists. That is why the ordering below does not
track difficulty.

Two distinctions worth keeping:

- **An oracle** produces files, so it can verify what blipper
  *writes*. Clipper 5.2e, mtools, and the `sqlite3` CLI are
  oracles.
- **A specimen** is a file someone else wrote, so it verifies what
  blipper *reads* and nothing more. The VFP 3.0 media and the
  dBASE 5.0 DOS media are specimen sources.

Read support needs specimens. Write support needs an oracle. That
difference is the gap between "about a week" and "about a
fortnight" on most format items.

## Near term

**T-27 — `.cpg` sidecar encoding.** P2, roughly 1.5 days.

The highest-value open item, because it is a live correctness
issue rather than a missing feature. Four independent GIS
producers write header byte 29 as `0x00` alongside a `.cpg` file
naming UTF-8. blipper reads byte 29, finds nothing, and passes
bytes through — correct by accident, since Go strings are UTF-8.
The accident breaks on the override path: a caller with a mixed
corpus calling `SetCodePage(CodePageIntl850)` would mangle every
GIS export among their files.

Fixtures are already identified and are decisive rather than
suggestive: Malta's `Ċ` and `Ħ` exist in no single-byte encoding
at all, so a wrong guess fails loudly instead of producing
plausible rubbish.

**Note, 2026-07-24:** this section was written early in the
session and left stale through several closures since. T-30 (MDX)
closed at v0.9.0. T-31 (dBASE IV/5 tables) closed at v0.9.21 —
below.

**T-31 — dBASE IV/5 tables — done.** Full read support shipped:
version bytes `0x8B`/`0x43`/`0x63` accepted, the `B`/`G` lineage
trap solved via two internal sentinel `FieldType` values remapped
at read time, byte 31 correctly not relied on for tag membership,
and a new `.DBT` memo reader for dBASE IV/5's own 8-byte-header
format (`dbf/memo_dbaseiv.go`) — whose block-0 header turned out
to be self-describing (table name, block size), a detail found
empirically while building it and cross-checked against three
real 1994 specimens plus the write-oracle. Verified against all
33 vendored specimens, the live write-oracle, and real 1994
purchase-history data including a memo spanning a block boundary.
See `docs/DBASE_FORMAT.md` and `docs/RESOLVED.md`.

Write support (a fresh `0x8B` table, or a new memo appended to an
existing dBASE-IV-format `.DBT`) was deliberately scoped out and
filed as **T-37** — fully specified, roughly half a day, not
blocked on anything.

**T-29 — IDX indexes.** P3, roughly 4 days, scope question now
has two concrete paths rather than none.

`DBFCDX.LIB` only ever produces *compact*-format `.IDX` (index
options byte `0x20`), so whether an uncompressed `.IDX` can be
generated for verification at all was unsettled for most of the
session — see `docs/INDEX_FORMATS.md` for the full account.

**This is no longer a dead end**, though neither path has been
tried yet. FoxBASE+ 2.10 predates the compact scheme entirely and
should write the uncompressed layout natively — but its DOSBox
runtime needs `BRAND.EXE` serialisation, which this project has
deliberately not pursued and will not help circumvent regardless
of who's running it. FoxPro 2.6 DOS is the more promising
near-term candidate: it wrote `.IDX` for FoxBASE+ backward
compatibility alongside its native `.CDX`, whether that
compatibility path is plain or compact is untested, and — unlike
FoxBASE+ — there is no known protection obstacle in the way. Real
interactive DOSBox access, proven capable of getting past what
headless automation couldn't (dBASE 5.0's Turbo Vision interface),
makes both paths genuinely reachable now rather than theoretical.

**T-27 — `.cpg` sidecar encoding.** P2, roughly 1.5 days.

A live correctness issue rather than a missing feature. Four
independent GIS producers write header byte 29 as `0x00` alongside
a `.cpg` file naming UTF-8. blipper reads byte 29, finds nothing,
and passes bytes through — correct by accident, since Go strings
are UTF-8. The accident breaks on the override path: a caller with
a mixed corpus calling `SetCodePage(CodePageIntl850)` would mangle
every GIS export among their files.

Fixtures are already identified and are decisive rather than
suggestive: Malta's `Ċ` and `Ħ` exist in no single-byte encoding
at all, so a wrong guess fails loudly instead of producing
plausible rubbish.

## Medium term

**Later FoxPro table variants — done.** This section originally
listed four items (`0x31`/`0x32` acceptance, `_NullFlags`, `T`
DateTime, `V`/`Q`/`W`) as future work. All four shipped since:
`0x31`/`0x32` accepted alongside the DateTime work (v0.9.13);
DateTime's epoch confirmed against real VFP 9 data via dBASE 7's
documented formula (T-33, v0.9.13); the complete `_NullFlags` bit
algorithm — including the Varchar/Varbinary interaction no source
had previously described — solved against a byte-exact worked
example (T-35, v0.9.16); `V`, `Q`, `W` all implemented in the same
pass. Field types are now 14 of 16.

One residual, not part of the original list because it wasn't
known to exist until the `_NullFlags` work surfaced it: **T-36**,
exact `Varchar`/`Varbinary` content decode when a stored value
legitimately ends in significant spaces. The algorithm is fully
specified (`IsFull` already exists); what's missing is threading
`_NullFlags`'s bits through `decodeRecord` before it reaches
`Varchar`/`Varbinary`, deferred as a `decodeRecord` restructuring
worth doing carefully rather than rushed alongside the bit-algorithm
fix.

**T-20 — cache invalidation for shared access.** P2, unsized.

The outstanding half of the concurrency work. v0.8.2 enforces the
locking protocol and the locks are real between processes, but a
shared *reader* still will not observe another process's writes,
because `Table.recordCount`, `cdx` nodes, `dbc` rows and the
`fatfs` FAT are all cached with nothing invalidating them.

Unsized deliberately: the interface is small, and the open
question is how many cached fields exist and whether each can be
re-read cheaply. Wants a survey pass before an estimate.

## Not planned

Recorded so the reasoning is not relitigated.

**dBASE 7.** 31-character field names in a different descriptor
layout, no specimens, and no oracle. A distinct format rather than
an extension of what exists.

**dBASE II and FoxBASE `0x02`/`0xFB`.** 16-bit record counts, a
520-byte header, a 32-field maximum — structurally a different
format that would not share code with the rest.

**Writing version byte `0x30`.** Reading a Visual FoxPro table is
safe; writing that byte is a promise to honour field types and
null semantics blipper does not implement. T-10's DBC sidecar
deliberately writes `0x03` with byte 28 set for the same reason,
and that decision should hold until the VFP exclusions close.

**CDX collations other than MACHINE.** The collation tables are
not documented in any source found, and guessing at collation
order corrupts sort order silently.

**Running the FoxBASE+ 2.10 runtime.** `FOXPLUS.EXE` refuses
without `BRAND.EXE` serialisation, and the install is the
protection rather than a step that gates it. Not pursued, and
this project will not help circumvent that check regardless of
who is asking or what machine it runs on.

This bears on almost nothing. **FoxBASE+ writes formats blipper
already supports**: it was built for dBASE III PLUS compatibility,
so its tables are `0x03`/`0x83` and its memo files are `.DBT`,
both read and written and oracle-verified against Clipper 5.2e.

**One format is a genuine exception, corrected 2026-07-24**: the
claim once made here, that `DBFCDX.LIB` closes the `.IDX` gap, was
wrong. `DBFCDX` only ever produces *compact*-format `.IDX`
(index-options byte `0x20`) — FoxBASE+'s own, older, uncompressed
layout has no confirmed generator anywhere this session. FoxBASE+
remains the most plausible native source for it, unreached for the
reason above; FoxPro 2.6 DOS is an alternative path with no known
protection obstacle. See T-29.

## The language

The project's stated direction is an xBase language in Go,
inspired by Clipper 5.x. Nothing of it exists yet, and the file
format work is the foundation rather than a detour from it.

The nearest thing to a prerequisite is an **expression
evaluator**. Its absence is already felt: a CDX tag stores
`UPPER(NAME)` as text and blipper cannot compute it, which is why
`Area.Pack` remaps CDX entries rather than recomputing them and
why callers must supply a Go `KeyFunc`. An evaluator would close
that gap and is the first piece of a language runtime either way.

Not scheduled. Recorded because it is the one item that serves
both the current library and the stated direction.

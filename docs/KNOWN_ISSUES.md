# blipper — Known Issues and Limits

Version: 0.9.25
Last reviewed: 2026-07-23

Intentional limits and invariant boundaries. Actionable gaps live in
docs/TRACKING.md.

## Limits

- Single writer, no concurrency: dbf, ntx and blipperdb objects are
  not safe for concurrent use, by design (a Clipper session model).
- No crash safety: a failed write can leave a table or index
  inconsistent; there is no journal.
- Durability stops at io.ReadWriteSeeker: callers own fsync policy.
- Deletion is logical only (register item T-03); deleted records
  remain visible and indexed, as in Clipper with SET DELETED OFF.
- Memo files are appended, never compacted: rewriting a memo
  allocates new blocks and the old ones are not reused. Existing
  pointers stay valid; callers needing compaction rebuild the file.

## Dormant guards

### G-01. Clipper 5.2e oracle under headless DOSBox

- **Gating condition:** manual; not part of `go test`. Requires the
  `dosbox` package and the `CLIPPER5/` toolchain staged into a mount
  point — neither is vendored, so this cannot run in CI as configured.
- **Requirements:** `apt-get install -y dosbox`; Clipper 5.2e
  toolchain from github.com/ha1tch/clipper (`CLIPPER.EXE`,
  `RTLINK.EXE`, `CLIPPER.LIB`, `EXTEND.LIB`, `DBFNTX.LIB`,
  `TERMINAL.LIB`).
- **Invocation:** `setsid env -u DISPLAY SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy dosbox -conf CONF.conf -exit </dev/null >host.log 2>&1 &`
- **Procedure:** docs/CLIPPER_ORACLE.md §10.
- **Last exercised:** 2026-07-23 env:container (DOSBox 0.74-3, Clipper 5.2e, RTLink 3.14B) — DBFCDX RDD generated FDATA.DBF (version 0xF5) and FDATA.FPT (block size 64, big-endian header) with a two-record memo field; oracle test surfaced a block-numbering bug in blipper's FPTFile (block 1 vs Clipper's block 8 for first memo) which was fixed under this exercise. Previous: 2026-07-23 — DBFCDX RDD linked via ANNOUNCE RDDSYS; CDX with two tags generated. Earlier: 2026-07-23 — SET DATE ANSI and DTOS/DTOC index ordering verified
  5.2e, RTLink 3.14B); compiled, linked and ran a generator producing
  a `.DBF`/`.NTX` pair, verified twice from clean directories.

Scope note: exercising this guard means running the §10 checklist to
a generated `.DBF`/`.NTX` pair. It does **not** imply that a
compatibility corpus is wired into the Go test suites; no such corpus
exists yet.

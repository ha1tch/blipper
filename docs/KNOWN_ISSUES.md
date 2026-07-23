# blipper — Known Issues and Limits

Version: 0.1.0
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
- Memo (.DBT) content is not interpreted (register item T-02).
- NTX numeric keys accept non-negative values only (register item
  T-01).

## Dormant guards

None. No build-tagged, environment-gated, or hardware-dependent
verifications exist in this repository yet. When the first one is
written it must be registered here in the same session (working
agreement Part 3 §8).

# blipper — Live Register

Version: 0.9.25
Last reviewed: 2026-07-23

Open actionable items only. Closed items move verbatim to
docs/RESOLVED.md as part of the closing release.

## Status table

| ID | Summary | Theme | Priority | Status | Blocks |
|---|---|---|---|---|---|
| T-20 | Cache invalidation for shared access — dbf.Table.Reload done, cdx/dbc/fatfs remain | dbf | P2 | ◐ | dbf piece closed |

### T-20. Cache invalidation for shared access (stage 2 of T-19)

Theme: dbf · Priority: P2 · Status: ◐ · Blocks/after: after T-19 (closed in v0.8.2). Also touches blipperdb, cdx, dbc, fatfs — see the detail below; single-token Theme field is this document's convention, the full scope is prose, not the field line.

- **Trigger:** T-19 stage 1 shipped in v0.8.2, 2026-07-23. That stage enforces the locking protocol — a shared write without a lock fails, and the locks are real between processes. What it does not do is make a shared reader observe another process's writes, because blipper caches aggressively and nothing invalidates those caches.
- **Confirmed by direct inspection, 2026-07-24, before touching any code: no reload/refresh/invalidate mechanism existed anywhere in the codebase.** `grep -rn "func.*Reload\|func.*Refresh\|func.*Invalidate"` across every package returned zero matches. This was a bigger finding than the item's earlier framing ("invalidate four caches") suggested — there was no shared pattern to extend, because nothing had ever needed to re-read anything after `Open`. Four independently-designed subsystems, four separate design problems, not one.
- **`dbf.Table.Reload` — done, v0.9.23.** Re-reads the header (`RecordCount`, `LastUpdate`) after seeking to start, with a record-size sanity check that rejects a mismatch as "this is a different table" rather than silently accepting a corrupt or unrelated file. Deliberately narrow: schema and physical layout are not re-read, since those cannot legitimately change without a migration this package does not support performing concurrently. Verified with the actual scenario this item exists for — two independent `*Table` instances over one stream, one appends, the other is stale until `Reload`, then correct after.
- **`Reload` as a plain method, not a `Refreshable` interface** — the interface idea from this item's original writeup was reasonable in the abstract, but with only one implementation built so far there is nothing yet to extract a shared shape from. Worth revisiting once `cdx`/`dbc`/`fatfs` versions exist and their actual shapes are known, rather than designing the interface first and fitting three different structures to it.
- **Remaining, each its own design problem:**
  - `cdx.File` holds decoded nodes and a lazily-loaded per-tag header; a rebuilt index elsewhere is invisible.
  - `dbc.Catalogue` loads every row at `Open` and never re-reads.
  - `fatfs.Volume` caches the whole FAT (`fat []byte`) and root directory (`rootEntries []dirEntry`), which is what makes its writes cheap and its reads stale.
  - `blipperfs`'s `Session` holds open handles across a whole directory, a coordination question layered on top of the three above rather than a fourth cache of its own.
- **The shape of the fix, for what's left.** Invalidation on lock acquisition is still the conventional answer and still fits the existing seam: taking a lock is exactly the moment a process is about to look at shared state, so `FLock`/`RLock` becoming the points where cached state is re-read remains the right design for `cdx`/`dbc`/`fatfs`, following the pattern `dbf.Table.Reload` establishes (re-read the persistent structure, sanity-check it's still the same object, then update the cached fields — not less than that, and no more).
- **Not everything can be fixed this way.** `fatfs` has no locking mechanism at all, so a FAT image cannot support shared access however the caches behave; that combination should report `ErrLockUnsupported` and stay unsupported rather than acquiring a partial answer.
- **Verification needs two processes**, as T-19's did and `Reload`'s own test now does at the `dbf` layer: one writes and releases, the other acquires and must observe the change. `testing/synctest` covers the deterministic parts, but the cross-process case is the one that proves it.
- **Effort for what's left:** roughly a day per remaining subsystem (`cdx`, `dbc`, `fatfs`), now that `dbf.Table.Reload` is a concrete precedent for the pattern rather than an abstract design question.

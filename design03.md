
# xBase Go Library Design Document — Part 3

## The blipperdb Session Layer

Updated: 2026-07-23

This document records the design of the `blipperdb` package as
implemented in v0.1.0. Unlike Parts 1 and 2 it was written alongside
the implementation, not ahead of it.

---

# 1. Purpose

The `dbf` and `ntx` packages are pure file-format layers: they take an
`io.ReadWriteSeeker`, never open or close anything themselves, and
know nothing about each other's instances.

A Clipper-like language runtime (eventually Blipper) needs the layer
Clipper itself provides at the work-area level:

```
USE A
USE B
USE C
SELECT A
SEEK "SMITH"
SKIP -1
REPLACE ...
```

That layer is `blipperdb`.

---

# 2. The BlipperDB object

The package exposes one central object:

```go
import bdb "github.com/ha1tch/blipper/blipperdb"

db := bdb.New()
```

`BlipperDB` pools open tables and indexes in named work areas:

| Operation | Clipper analogue |
| --------- | ---------------- |
| `Use(alias, rw)` | `USE` |
| `Create(alias, rw, schema)` | `CREATE` |
| `Select(alias)` | `SELECT` |
| `Current()` | the selected work area |
| `Aliases()` | `ALIAS()` enumeration |
| `CloseArea(alias)` | `USE` (close) |
| `CloseAll()` | `CLOSE ALL` |

Aliases are case-insensitive. `Use` with an existing alias closes and
replaces that area, and every `Use`/`Create` selects the new area.

---

# 3. Ownership

`BlipperDB` owns what it is handed. If a table or index source
implements `io.Closer`, closing the area closes it. This is the
deliberate opposite of the `dbf`/`ntx` convention, and it is what
makes `CLOSE ALL` possible.

---

# 4. Work areas

An `Area` carries:

* the open `*dbf.Table`;
* attached indexes, each paired with the `ntx.KeyFunc` that derives
  its keys;
* a controlling order (`SetOrder`; 0 is natural order, n is the n-th
  attached index);
* a record pointer with xBase semantics.

## 4.1 Record pointer

`GoTop`, `GoBottom`, `GoTo`, `Skip(n)` (both directions), `Seek`,
`Eof`, `Bof`, `Recno`, `Record`.

Pointer conventions follow xBase: skipping past the last record parks
on the last record with `Eof()` true; skipping before the first parks
on the first with `Bof()` true. Deleted records are visible (`SET
DELETED OFF` behaviour).

`Seek` requires a controlling index and behaves like `SEEK` with
`SOFTSEEK ON`: it parks at the first key at or above the sought key
and reports whether the key matched as a prefix.

## 4.2 Navigation is stateless

Index-order navigation uses the `ntx` point queries (`Min`, `Max`,
`Successor`, `Predecessor`, `FirstGE`) rather than a long-lived
cursor. The area's only position state is the record number, so
writes never invalidate a traversal structure; each step re-descends
the tree at O(log n).

---

# 5. Index maintenance

The invariant that neither `dbf` nor `ntx` can own individually:

* `Append` inserts the new record's key into every attached index.
* `Replace` reads the old record first, deletes each index key whose
  value changed, and inserts the new one.
* `Delete`/`Recall` change only the deletion mark; indexes are
  untouched, because Clipper keeps deleted records in its indexes.

Unique indexes fit naturally: `ntx.Insert` on a duplicate key reports
"not added" without error, and a later `Delete` miss for a record the
index never held is equally harmless.

Direct writes through `Area.Table()` bypass maintenance and are
documented as such.

---

# 6. Non-goals

* Concurrency: a `BlipperDB` is single-threaded by design, like the
  language session it models.
* Expression evaluation: key derivation is Go code (`ntx.KeyFunc`);
  the stored key expression remains documentation.
* Filters, relations, and locking: future work, tracked in the
  register when scheduled.

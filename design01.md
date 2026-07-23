
# xBase Go Library

## Design Specification — Part 1

### DBF and NTX Compatibility Library

**Version:** 0.1
**Target Language:** Go
**Primary Compatibility Target:** dBASE III+ / Nantucket Clipper 5.x
**Status:** Design Specification

---

# 1. Purpose

This document specifies the architecture and implementation requirements for a Go library providing support for legacy xBase file formats.

The initial implementation targets:

* dBASE III+ `.DBF` table files.
* Nantucket Clipper `.NTX` index files.

The library shall provide:

* Reading existing files.
* Creating new compatible files.
* Updating records.
* Managing indexes.
* Providing idiomatic Go APIs.

The implementation shall prioritize:

1. Binary compatibility.
2. Correctness.
3. Simplicity.
4. Maintainable Go code.

---

# 2. Scope

## 2.1 Supported formats

Initial support:

| Format           | Extension | Status   |
| ---------------- | --------- | -------- |
| dBASE III+ table | `.DBF`    | Required |
| Clipper index    | `.NTX`    | Required |
| dBASE memo       | `.DBT`    | Deferred |

---

## 2.2 Non-goals

The initial implementation shall not provide:

* SQL query functionality.
* Network record locking.
* Multi-user synchronization.
* Clipper language expression evaluation.
* Transaction logging.
* Memo file support.
* GUI tools.

---

# 3. Design Principles

## 3.1 Separation of logical and physical models

The library shall separate:

* logical database objects
* binary file representations

Public structures shall not mirror disk layouts.

Example:

Incorrect:

```go
type Header struct {
    Reserved [12]byte
}
```

Correct:

```go
type Header struct {
    LastUpdate time.Time
    CodePage byte
}
```

Reserved bytes and format-specific fields shall remain internal.

---

## 3.2 Explicit serialization

Binary serialization shall:

* use fixed-size byte buffers.
* use explicit byte offsets.
* avoid Go struct memory layout assumptions.

Serialization shall not depend on:

* struct packing.
* compiler layout.
* unsafe operations.

---

## 3.3 Resource ownership

Objects shall have clear ownership boundaries.

| Object | Owns             |
| ------ | ---------------- |
| Schema | Field definition |
| Record | Data values      |
| Table  | File access      |
| Index  | Index storage    |

---

# 4. Package Structure

The repository shall contain:

```
xbase/
|
├── dbf/
|
└── ntx/
```

The DBF package shall be independent of NTX.

The NTX package may depend on DBF types.

---

# 5. DBF Package Architecture

Directory:

```
dbf/
```

---

## 5.1 types.go

Purpose:

Define public data structures.

Responsibilities:

* Field type definitions.
* Field structure.
* Schema structure.
* Header structure.
* Record structure.
* Table structure.
* Cursor structure.

Restrictions:

* No file serialization.
* No binary constants.
* No disk offsets.

---

## 5.2 schema.go

Purpose:

Schema validation and metadata.

Responsibilities:

* Validate field definitions.
* Validate field names.
* Validate field types.
* Calculate record size.
* Calculate header size.

Required operations:

```go
func (s Schema) Validate() error

func (s Schema) HeaderSize() uint16

func (s Schema) RecordSize() uint16
```

---

## 5.3 header.go

Purpose:

DBF file header serialization.

Responsibilities:

* Read 32-byte DBF header.
* Write 32-byte DBF header.
* Encode/decode dates.

Required internal operations:

```go
func readHeader(
    io.Reader,
) (Header,uint32,error)

func writeHeader(
    io.Writer,
    Header,
    Schema,
    uint32,
) error
```

---

## 5.4 field.go

Purpose:

DBF field descriptor serialization.

Responsibilities:

* Read field descriptors.
* Write field descriptors.
* Encode/decode field names.

Required operations:

```go
func readFields(
    io.Reader,
) ([]Field,error)

func writeFields(
    io.Writer,
    []Field,
) error
```

---

## 5.5 record.go

Purpose:

Logical record manipulation.

Responsibilities:

* Create records.
* Read values.
* Modify values.
* Validate values.

Restrictions:

* No file access.
* No byte encoding.

Required operations:

```go
func NewRecord(
    Schema,
) Record
```

---

## 5.6 record_codec.go

Purpose:

Binary record encoding.

Responsibilities:

* Encode record to DBF format.
* Decode DBF record.

Required operations:

```go
func encodeRecord(
    []byte,
    Schema,
    Record,
) error


func decodeRecord(
    []byte,
    Schema,
) (Record,error)
```

---

## 5.7 create.go

Purpose:

Create new DBF files.

Responsibilities:

* Validate schema.
* Write header.
* Write field descriptors.
* Write EOF marker.

---

## 5.8 open.go

Purpose:

Open existing DBF files.

Responsibilities:

* Read header.
* Read fields.
* Construct Table object.
* Validate consistency.

---

## 5.9 table.go

Purpose:

Database file operations.

Responsibilities:

* Read records.
* Write records.
* Append records.
* Delete records.
* Flush metadata.

Required operations:

```go
func Get(
    uint32,
) (Record,error)


func Put(
    uint32,
    Record,
) error


func Append(
    Record,
) (uint32,error)


func Delete(
    uint32,
) error
```

---

## 5.10 cursor.go

Purpose:

Sequential traversal.

Required operations:

```go
func Next() bool

func Record() Record

func Err() error
```

---

# 6. DBF Record Model

A DBF record consists of:

```
+----------------+----------------------+
| Delete marker  | Field data           |
| 1 byte         | variable             |
+----------------+----------------------+
```

Record size:

```
1 + sum(field lengths)
```

---

# 7. DBF Type Mapping

| DBF Type  | Go Type               |
| --------- | --------------------- |
| Character | string                |
| Numeric   | int64/float64         |
| Float     | float64               |
| Logical   | bool                  |
| Date      | time.Time             |
| Memo      | unsupported initially |

---

# 8. Record Numbering

DBF record numbers are one-based.

Example:

```
Record 1 = first physical record
Record 2 = second physical record
```

Physical offset:

```
header_size + ((record_number - 1) * record_size)
```

---

# 9. Deleted Records

Deletion shall be logical.

Deleting a record shall:

* set deletion marker.
* preserve record data.

Physical removal shall be implemented later through a PACK operation.

---

# 10. NTX Package Architecture

Directory:

```
ntx/
```

Initial files:

```
types.go
header.go
page.go
tree.go
cursor.go
insert.go
delete.go
```

Responsibilities:

* NTX header handling.
* 1024-byte page handling.
* B-tree traversal.
* Insert/delete operations.
* Index cursors.

---

# 11. DBF/NTX Dependency Model

Dependency direction:

```
       ntx
        |
        v
       dbf
```

The DBF package shall not import NTX.

---

# 12. Testing Requirements

The implementation shall include:

## DBF tests

* Header round-trip.
* Field descriptor round-trip.
* Schema validation.
* Record encoding.
* Record decoding.
* Append/read/update/delete.
* Cursor traversal.

## NTX tests

* Page serialization.
* Tree creation.
* Search.
* Insert.
* Split.
* Delete.
* Rebalance.

---

# 13. Compatibility Verification

Compatibility shall be verified against:

* Clipper-generated DBF files.
* Clipper-generated NTX files.
* Round-trip open/write/read tests.

Generated files shall remain readable by Clipper-compatible software.

---

**End of Design Specification — Part 1**

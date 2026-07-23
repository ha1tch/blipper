
# xBase Go Library Design Document — Part 2

## DBF Core Implementation Details

This extends the initial package plan with more precise API contracts, invariants, and implementation rules.

---

# 1. Package philosophy

The package implements **dBASE III+ compatible DBF files**, with Clipper 5.x compatibility as the primary target.

Goals:

* Read existing Clipper-generated `.DBF` files.
* Create DBF files readable by Clipper.
* Modify records while preserving DBF semantics.
* Provide an idiomatic Go API.
* Keep binary compatibility concerns isolated.

Non-goals for DBF core:

* SQL-like querying.
* Automatic indexing.
* Clipper expression language.
* Memo files initially.
* Network locking.
* Multi-user concurrency.

---

# 2. Package layout

Final layout:

```
dbf/
    types.go
    schema.go

    header.go
    field.go

    record.go
    record_codec.go

    table.go
    open.go
    create.go

    cursor.go

    errors.go

    *_test.go
```

Responsibilities:

| File            | Responsibility                       |
| --------------- | ------------------------------------ |
| types.go        | Public data structures               |
| schema.go       | Schema validation and field metadata |
| header.go       | DBF header binary format             |
| field.go        | Field descriptor binary format       |
| record.go       | Logical record operations            |
| record_codec.go | Record <-> bytes conversion          |
| create.go       | New DBF creation                     |
| open.go         | Existing DBF opening                 |
| table.go        | CRUD operations                      |
| cursor.go       | Sequential traversal                 |
| errors.go       | Shared errors                        |

---

# 3. Core types

## Field

```go
type Field struct {
    Name string
    Type FieldType
    Length uint8
    Decimals uint8
}
```

Properties:

* Field names are case-insensitive.
* Maximum name length: 11 bytes.
* Field ordering is significant.
* Fields cannot be added after table creation.

---

## Schema

```go
type Schema struct {
    Fields []Field
}
```

Schema invariants:

* No duplicate field names.
* Valid field types only.
* Valid lengths.
* Valid decimal counts.
* Stable ordering.

The schema defines:

* record size
* field offsets
* encoding rules

---

## Header

```go
type Header struct {
    LastUpdate time.Time
    CodePage byte
}
```

The implementation owns:

* version byte
* record count
* header size
* record size
* reserved bytes

Those are not part of the public API.

---

## Record

```go
type Record struct {
    Deleted bool
    Values []any
}
```

A Record:

* has no knowledge of its table.
* has no knowledge of its schema.
* is just data.

The schema provides interpretation.

Example:

```
Schema:

0 NAME Character 20
1 AGE  Numeric 3

Record:

[
    "JOHN",
    42,
]
```

---

# 4. Value model

DBF values map to Go values:

| DBF | Go                    |
| --- | --------------------- |
| C   | string                |
| N   | int64 / float64       |
| F   | float64               |
| L   | bool                  |
| D   | time.Time             |
| M   | unsupported initially |

The library will normalize numeric values internally.

---

# 5. Record creation

```go
record := dbf.NewRecord(schema)
```

Creates:

```go
Record{
    Deleted:false,
    Values:[
        "",
        int64(0),
        false,
        time.Time{},
    ],
}
```

based on field types.

---

# 6. Record API

Final API:

```go
func NewRecord(schema Schema) Record
```

---

## Reading

```go
func (r Record) Get(
    schema Schema,
    name string,
) (any,error)
```

Example:

```go
name,_ := rec.Get(schema,"NAME")
```

---

## Writing

```go
func (r *Record) Set(
    schema Schema,
    name string,
    value any,
) error
```

Example:

```go
rec.Set(schema,"AGE",25)
```

---

## Indexed access

For performance:

```go
func (r Record) GetIndex(
    index int,
) (any,error)


func (r *Record) SetIndex(
    schema Schema,
    index int,
    value any,
) error
```

---

# 7. Record encoding

A DBF record:

```
+--------+----------------+
| delete | field contents |
+--------+----------------+
    1 byte
```

Example:

```
20 byte name
3 byte age
8 byte date

total:

1+20+3+8=32 bytes
```

---

## Encoding rules

### Character

Input:

```
"ABC"
```

Stored:

```
ABC<spaces>
```

Length always exact.

---

### Numeric

Stored as ASCII.

Example:

Value:

```
123
```

Field length 6:

```
"   123"
```

Right aligned.

---

### Date

Stored:

```
YYYYMMDD
```

Example:

```
20260723
```

---

### Logical

Stored:

True:

```
T
Y
```

False:

```
F
N
```

---

# 8. Table abstraction

A Table owns:

* file handle
* schema
* header
* record addressing

Internal:

```go
type Table struct {
    rw io.ReadWriteSeeker

    header Header
    schema Schema

    recordCount uint32
    recordStart int64
}
```

---

# 9. Opening a table

```go
func Open(
    rw io.ReadWriteSeeker,
) (*Table,error)
```

Process:

1. Read header.
2. Read field descriptors.
3. Validate schema.
4. Calculate record start.
5. Return table.

---

# 10. Creating a table

```go
func Create(
    rw io.ReadWriteSeeker,
    schema Schema,
) (*Table,error)
```

Process:

1. Validate schema.
2. Write header.
3. Write fields.
4. Write EOF marker.
5. Return table.

Initial file:

```
header
fields
0D
1A
```

---

# 11. Record addressing

DBF records are 1-based.

Record offset:

```
headerSize +
(recordNumber - 1) * recordSize
```

Example:

```
record 1:
offset = headerSize

record 2:
offset = headerSize + recordSize
```

---

# 12. Table CRUD

## Read

```go
func (t *Table) Get(
    recno uint32,
) (Record,error)
```

---

## Append

```go
func (t *Table) Append(
    r Record,
) (uint32,error)
```

Process:

1. Seek EOF marker.
2. Write encoded record.
3. Rewrite EOF marker.
4. Increment count.
5. Rewrite header.

---

## Update

```go
func (t *Table) Put(
    recno uint32,
    r Record,
) error
```

---

## Delete

```go
func (t *Table) Delete(
    recno uint32,
) error
```

Only changes:

```
' ' -> '*'
```

No physical removal.

---

# 13. Cursor

API:

```go
cursor := table.Cursor()

for cursor.Next() {
    rec := cursor.Record()
}
```

Internally:

```go
type Cursor struct {
    table *Table
    recno uint32
    err error
}
```

---

# 14. Error model

Minimal:

```go
var (
    ErrEOF =
        errors.New("end of file")

    ErrInvalidRecord =
        errors.New("invalid record")

    ErrUnsupported =
        errors.New("unsupported feature")
)
```

Avoid excessive error types.

---

# 15. Future extensions

After DBF is complete:

```
ntx/
    types.go
    header.go
    page.go
    tree.go
    cursor.go
    insert.go
    delete.go
```

NTX will consume:

```go
type KeyFunc func(dbf.Record) []byte
```

No dependency from DBF → NTX.

---

# 16. Implementation sequence

The actual coding sequence:

```
1. types.go              DONE
2. schema.go             DONE
3. header.go             DONE
4. field.go              DONE

5. record.go
6. record_codec.go

7. errors.go

8. create.go
9. open.go

10. table.go
11. cursor.go

12. tests
```

Only after the DBF layer passes compatibility tests:

```
13. NTX package
```


package blipperdb

import (
	"fmt"
	"io"
	"testing"

	"github.com/ha1tch/blipper/dbf"
	"github.com/ha1tch/blipper/ntx"
)

// memFile is an in-memory io.ReadWriteSeeker that records whether it
// was closed.
type memFile struct {
	data   []byte
	pos    int64
	closed bool
}

func (m *memFile) Read(p []byte) (int, error) {
	if m.pos >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += int64(n)
	return n, nil
}

func (m *memFile) Write(p []byte) (int, error) {
	end := m.pos + int64(len(p))
	if end > int64(len(m.data)) {
		grown := make([]byte, end)
		copy(grown, m.data)
		m.data = grown
	}
	copy(m.data[m.pos:end], p)
	m.pos = end
	return len(p), nil
}

func (m *memFile) Seek(offset int64, whence int) (int64, error) {
	var base int64
	switch whence {
	case io.SeekStart:
		base = 0
	case io.SeekCurrent:
		base = m.pos
	case io.SeekEnd:
		base = int64(len(m.data))
	default:
		return 0, fmt.Errorf("bad whence %d", whence)
	}
	pos := base + offset
	if pos < 0 {
		return 0, fmt.Errorf("negative position")
	}
	m.pos = pos
	return pos, nil
}

func (m *memFile) Close() error {
	m.closed = true
	return nil
}

func personSchema() dbf.Schema {
	return dbf.Schema{
		Fields: []dbf.Field{
			{Name: "NAME", Type: dbf.Character, Length: 10},
			{Name: "AGE", Type: dbf.Numeric, Length: 3},
		},
	}
}

func nameKey(schema dbf.Schema) ntx.KeyFunc {
	return func(r dbf.Record) []byte {
		v, _ := r.Get(schema, "NAME")
		return ntx.CharKey(v.(string), 10)
	}
}

func appendPerson(t *testing.T, area *Area, name string, age int) uint32 {
	t.Helper()

	schema := area.Table().Schema()
	record := dbf.NewRecord(schema)
	record.Set(schema, "NAME", name)
	record.Set(schema, "AGE", age)

	recno, err := area.Append(record)
	if err != nil {
		t.Fatalf("Append %s: %v", name, err)
	}

	return recno
}

func nameAt(t *testing.T, area *Area) string {
	t.Helper()

	record, err := area.Record()
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	v, err := record.Get(area.Table().Schema(), "NAME")
	if err != nil {
		t.Fatalf("Get NAME: %v", err)
	}

	return v.(string)
}

func TestUseSelectAndAliases(t *testing.T) {
	db := New()

	for _, alias := range []string{"A", "B", "C"} {
		if _, err := db.Create(alias, &memFile{}, personSchema()); err != nil {
			t.Fatalf("Create %s: %v", alias, err)
		}
	}

	// The last USE selects its area.
	if got := db.Current().Alias(); got != "C" {
		t.Fatalf("Current = %q, want C", got)
	}

	area, err := db.Select("a") // aliases are case-insensitive
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if area.Alias() != "A" || db.Current().Alias() != "A" {
		t.Fatalf("Select did not switch the current area")
	}

	aliases := db.Aliases()
	want := []string{"A", "B", "C"}

	if len(aliases) != len(want) {
		t.Fatalf("Aliases = %v", aliases)
	}
	for i := range want {
		if aliases[i] != want[i] {
			t.Fatalf("Aliases = %v, want %v", aliases, want)
		}
	}

	if _, err := db.Select("NOPE"); err == nil {
		t.Fatalf("Select of unknown alias succeeded")
	}
}

func TestUseReopensExistingFile(t *testing.T) {
	file := &memFile{}

	db := New()

	area, err := db.Create("T", file, personSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	appendPerson(t, area, "KEPT", 1)

	if err := db.CloseArea("T"); err != nil {
		t.Fatalf("CloseArea: %v", err)
	}

	file.closed = false
	file.pos = 0

	area, err = db.Use("T", file)
	if err != nil {
		t.Fatalf("Use: %v", err)
	}

	if area.Table().RecordCount() != 1 {
		t.Fatalf("reopened table has %d records", area.Table().RecordCount())
	}

	if got := nameAt(t, area); got != "KEPT" {
		t.Fatalf("NAME = %q", got)
	}
}

func TestNaturalOrderNavigation(t *testing.T) {
	db := New()

	area, _ := db.Create("NAV", &memFile{}, personSchema())

	// Empty table: pointer at EOF.
	if !area.Eof() || area.Recno() != 0 {
		t.Fatalf("empty area: Eof=%v Recno=%d", area.Eof(), area.Recno())
	}

	for i, name := range []string{"ONE", "TWO", "THREE"} {
		appendPerson(t, area, name, i)
	}

	if err := area.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	if got := nameAt(t, area); got != "ONE" {
		t.Fatalf("top = %q", got)
	}

	area.Skip(2)
	if got := nameAt(t, area); got != "THREE" {
		t.Fatalf("after Skip(2) = %q", got)
	}

	// Past the end: xBase parks on the last record with Eof true.
	area.Skip(1)
	if !area.Eof() || area.Recno() != 3 {
		t.Fatalf("past end: Eof=%v Recno=%d", area.Eof(), area.Recno())
	}

	area.GoBottom()
	area.Skip(-2)
	if got := nameAt(t, area); got != "ONE" {
		t.Fatalf("after Skip(-2) = %q", got)
	}

	// Before the start: parked on the first with Bof true.
	area.Skip(-1)
	if !area.Bof() || area.Recno() != 1 {
		t.Fatalf("before start: Bof=%v Recno=%d", area.Bof(), area.Recno())
	}

	// Bof clears on forward movement.
	area.Skip(1)
	if area.Bof() {
		t.Fatalf("Bof still set after forward skip")
	}
	if got := nameAt(t, area); got != "TWO" {
		t.Fatalf("after recovery skip = %q", got)
	}

	if err := area.GoTo(3); err != nil {
		t.Fatalf("GoTo: %v", err)
	}
	if got := nameAt(t, area); got != "THREE" {
		t.Fatalf("GoTo(3) = %q", got)
	}

	if err := area.GoTo(4); err == nil {
		t.Fatalf("GoTo past end succeeded")
	}
}

func TestIndexOrderNavigationAndSeek(t *testing.T) {
	db := New()

	area, _ := db.Create("IDX", &memFile{}, personSchema())

	// Physical order is deliberately not alphabetical.
	appendPerson(t, area, "MANGO", 1)
	appendPerson(t, area, "APPLE", 2)
	appendPerson(t, area, "PEAR", 3)
	appendPerson(t, area, "BANANA", 4)

	schema := area.Table().Schema()

	_, err := area.CreateIndex(
		&memFile{},
		ntx.Options{KeyExpr: "NAME", KeySize: 10},
		nameKey(schema),
	)
	if err != nil {
		t.Fatalf("CreateIndex: %v", err)
	}

	if area.Order() != 1 {
		t.Fatalf("Order = %d, want 1 after first index", area.Order())
	}

	// Walk forward in index order.
	area.GoTop()

	var forward []string

	for !area.Eof() {
		forward = append(forward, nameAt(t, area))

		if err := area.Skip(1); err != nil {
			t.Fatalf("Skip: %v", err)
		}
	}

	want := []string{"APPLE", "BANANA", "MANGO", "PEAR"}

	if len(forward) != len(want) {
		t.Fatalf("forward = %v", forward)
	}
	for i := range want {
		if forward[i] != want[i] {
			t.Fatalf("forward = %v, want %v", forward, want)
		}
	}

	// Walk backward.
	area.GoBottom()

	var backward []string

	for !area.Bof() {
		backward = append(backward, nameAt(t, area))

		if err := area.Skip(-1); err != nil {
			t.Fatalf("Skip back: %v", err)
		}
	}

	for i := range want {
		if backward[len(backward)-1-i] != want[i] {
			t.Fatalf("backward = %v", backward)
		}
	}

	// Seek: exact, prefix, and miss.
	found, err := area.Seek([]byte("MANGO"))
	if err != nil || !found {
		t.Fatalf("Seek MANGO: found=%v err=%v", found, err)
	}
	if area.Recno() != 1 {
		t.Fatalf("Seek MANGO recno = %d", area.Recno())
	}

	found, _ = area.Seek([]byte("B"))
	if !found || nameAt(t, area) != "BANANA" {
		t.Fatalf("prefix seek: found=%v name=%q", found, nameAt(t, area))
	}

	found, _ = area.Seek([]byte("CHERRY"))
	if found {
		t.Fatalf("Seek CHERRY reported a match")
	}
	// Soft seek: parked at the first key above.
	if nameAt(t, area) != "MANGO" {
		t.Fatalf("soft seek parked at %q", nameAt(t, area))
	}

	// Natural order still available.
	area.SetOrder(0)
	area.GoTop()
	if got := nameAt(t, area); got != "MANGO" {
		t.Fatalf("natural top = %q", got)
	}
}

func TestWritesMaintainIndexes(t *testing.T) {
	db := New()

	area, _ := db.Create("W", &memFile{}, personSchema())

	schema := area.Table().Schema()

	_, err := area.CreateIndex(
		&memFile{},
		ntx.Options{KeyExpr: "NAME", KeySize: 10},
		nameKey(schema),
	)
	if err != nil {
		t.Fatalf("CreateIndex: %v", err)
	}

	appendPerson(t, area, "ZULU", 0)
	appendPerson(t, area, "ALPHA", 0)

	// Replace ZULU with CHARLIE: the index must move it.
	area.GoTo(1)

	record := dbf.NewRecord(schema)
	record.Set(schema, "NAME", "CHARLIE")
	record.Set(schema, "AGE", 9)

	if err := area.Replace(record); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	area.GoTop()

	var order []string

	for !area.Eof() {
		order = append(order, nameAt(t, area))
		area.Skip(1)
	}

	want := []string{"ALPHA", "CHARLIE"}

	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("index order after replace = %v, want %v", order, want)
	}

	// The old key must be gone from the index.
	found, _ := area.Seek([]byte("ZULU"))
	if found {
		t.Fatalf("stale ZULU key survived the replace")
	}

	// Deleted records stay in the index, like Clipper.
	area.Seek([]byte("ALPHA"))

	if err := area.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	found, _ = area.Seek([]byte("ALPHA"))
	if !found {
		t.Fatalf("deleted record vanished from the index")
	}

	record, _ = area.Record()
	if !record.Deleted {
		t.Fatalf("record not marked deleted")
	}

	if err := area.Recall(); err != nil {
		t.Fatalf("Recall: %v", err)
	}

	record, _ = area.Record()
	if record.Deleted {
		t.Fatalf("record still deleted after Recall")
	}
}

func TestCloseOwnership(t *testing.T) {
	db := New()

	tableFile := &memFile{}
	indexFile := &memFile{}

	area, _ := db.Create("OWN", tableFile, personSchema())

	appendPerson(t, area, "X", 1)

	_, err := area.CreateIndex(
		indexFile,
		ntx.Options{KeyExpr: "NAME", KeySize: 10},
		nameKey(area.Table().Schema()),
	)
	if err != nil {
		t.Fatalf("CreateIndex: %v", err)
	}

	if err := db.CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}

	if !tableFile.closed || !indexFile.closed {
		t.Fatalf(
			"ownership not honoured: table closed=%v index closed=%v",
			tableFile.closed,
			indexFile.closed,
		)
	}

	if db.Current() != nil || len(db.Aliases()) != 0 {
		t.Fatalf("areas survived CloseAll")
	}
}

func TestUseReplacesAlias(t *testing.T) {
	db := New()

	first := &memFile{}

	if _, err := db.Create("R", first, personSchema()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	second := &memFile{}

	if _, err := db.Create("R", second, personSchema()); err != nil {
		t.Fatalf("re-Create: %v", err)
	}

	if !first.closed {
		t.Fatalf("USE with an existing alias did not close the old area")
	}

	if len(db.Aliases()) != 1 {
		t.Fatalf("Aliases = %v", db.Aliases())
	}
}

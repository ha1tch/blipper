package blipperdb

import (
	"bytes"
	"testing"

	"github.com/ha1tch/blipper/cdx"
	"github.com/ha1tch/blipper/dbf"
)

// codeSchema is a minimal one-field schema used by the CDX
// attachment tests.
func codeSchema() dbf.Schema {
	return dbf.Schema{Fields: []dbf.Field{
		{Name: "CODE", Type: dbf.Character, Length: 10},
	}}
}

func newCDXTable(t *testing.T, codes []string) *Area {
	t.Helper()
	db := New()
	area, err := db.Create("DATA", &memFile{}, codeSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, c := range codes {
		rec := dbf.NewRecord(area.Table().Schema())
		rec.Set(area.Table().Schema(), "CODE", c)
		if _, err := area.Append(rec); err != nil {
			t.Fatalf("Append %q: %v", c, err)
		}
	}
	return area
}

func TestAreaCDXAttachAndTraverse(t *testing.T) {
	area := newCDXTable(t, []string{"BRAVO", "ALPHA", "CHARLIE"})

	entries := []cdx.Entry{
		{Key: []byte("ALPHA"), RecNo: 2},
		{Key: []byte("BRAVO"), RecNo: 1},
		{Key: []byte("CHARLIE"), RecNo: 3},
	}
	var buf bytes.Buffer
	if err := cdx.Build(&buf, []cdx.TagSpec{{
		Name:    "BYCODE",
		KeyExpr: "CODE",
		KeyLen:  10,
		Entries: entries,
	}}); err != nil {
		t.Fatalf("cdx.Build: %v", err)
	}
	if _, err := area.AttachCDX(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("AttachCDX: %v", err)
	}
	if err := area.SetOrderCDX("BYCODE"); err != nil {
		t.Fatalf("SetOrderCDX: %v", err)
	}

	var visited []uint32
	err := area.TraverseCDX(func(recNo uint32) error {
		visited = append(visited, recNo)
		return nil
	})
	if err != nil {
		t.Fatalf("TraverseCDX: %v", err)
	}
	want := []uint32{2, 1, 3}
	if len(visited) != len(want) {
		t.Fatalf("TraverseCDX yielded %v, want %v", visited, want)
	}
	for i, v := range want {
		if visited[i] != v {
			t.Errorf("index-order visit %d: got recNo %d, want %d", i, visited[i], v)
		}
	}
	if names := area.CDXTags(); len(names) != 1 || names[0] != "BYCODE" {
		t.Errorf("CDXTags = %v, want [BYCODE]", names)
	}
}

func TestAreaCDXErrorPaths(t *testing.T) {
	area := newCDXTable(t, []string{"X"})

	if err := area.SetOrderCDX("ANYTHING"); err == nil {
		t.Error("SetOrderCDX with no CDX should error")
	}
	if err := area.TraverseCDX(func(uint32) error { return nil }); err == nil {
		t.Error("TraverseCDX with no CDX should error")
	}
	if got := area.CDXTags(); got != nil {
		t.Errorf("CDXTags with no CDX = %v, want nil", got)
	}

	var buf bytes.Buffer
	err := cdx.Build(&buf, []cdx.TagSpec{{
		Name:    "K",
		KeyExpr: "K",
		KeyLen:  5,
		Entries: []cdx.Entry{{Key: []byte("A"), RecNo: 1}},
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := area.AttachCDX(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("AttachCDX: %v", err)
	}
	if err := area.SetOrderCDX("MISSING"); err == nil {
		t.Error("SetOrderCDX with missing tag should error")
	}
	if err := area.TraverseCDX(func(uint32) error { return nil }); err == nil {
		t.Error("TraverseCDX with no controlling tag should error")
	}
}

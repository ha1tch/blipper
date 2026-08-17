package cdx

import (
	"bytes"
	"testing"
)

// TestRoundTripSingleTag builds a tiny CDX with one tag and
// verifies our own reader can enumerate it.
func TestRoundTripSingleTag(t *testing.T) {
	spec := TagSpec{
		Name:    "BYCODE",
		KeyExpr: "CODE",
		KeyLen:  10,
		Entries: []Entry{
			{Key: []byte("ALPHA"), RecNo: 1},
			{Key: []byte("BRAVO"), RecNo: 2},
			{Key: []byte("CHARLIE"), RecNo: 3},
		},
	}
	var buf bytes.Buffer
	if err := Build(&buf, []TagSpec{spec}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got := buf.Len() % BlockSize; got != 0 {
		t.Fatalf("Build produced %d bytes, not block-aligned", buf.Len())
	}

	f, err := Open(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	names := f.TagNames()
	if len(names) != 1 || names[0] != "BYCODE" {
		t.Errorf("TagNames = %v, want [BYCODE]", names)
	}
	tag, err := f.Tag("BYCODE")
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}
	if got := tag.KeyExpr(); got != "CODE" {
		t.Errorf("KeyExpr = %q, want %q", got, "CODE")
	}

	var keys []string
	var recs []uint32
	err = f.Traverse(tag, func(e Entry) error {
		keys = append(keys, string(bytes.TrimRight(e.Key, " ")))
		recs = append(recs, e.RecNo)
		return nil
	})
	if err != nil {
		t.Fatalf("Traverse: %v", err)
	}
	wantKeys := []string{"ALPHA", "BRAVO", "CHARLIE"}
	wantRecs := []uint32{1, 2, 3}
	for i, k := range wantKeys {
		if keys[i] != k {
			t.Errorf("entry %d key: got %q, want %q", i, keys[i], k)
		}
		if recs[i] != wantRecs[i] {
			t.Errorf("entry %d recNo: got %d, want %d", i, recs[i], wantRecs[i])
		}
	}
}

// TestRoundTripTwoTags matches the shape of our fixture: two tags
// in a single file, one with character keys, one with numeric-
// coded keys. This exercises the tag directory as well as the
// per-tag leaves.
func TestRoundTripTwoTags(t *testing.T) {
	specs := []TagSpec{
		{
			Name:    "BYCODE",
			KeyExpr: "CODE",
			KeyLen:  10,
			Entries: []Entry{
				{Key: []byte("ALPHA"), RecNo: 1},
				{Key: []byte("BRAVO"), RecNo: 2},
				{Key: []byte("CHARLIE"), RecNo: 3},
			},
		},
		{
			Name:    "BYNVAL",
			KeyExpr: "NVAL",
			KeyLen:  8,
			// Ascending numeric keys, DTOS-style ASCII to keep
			// the test independent of NTX's numeric transform.
			Entries: []Entry{
				{Key: []byte("00000001"), RecNo: 2},
				{Key: []byte("00000010"), RecNo: 1},
				{Key: []byte("00000099"), RecNo: 3},
			},
		},
	}
	var buf bytes.Buffer
	if err := Build(&buf, specs); err != nil {
		t.Fatalf("Build: %v", err)
	}

	f, err := Open(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	names := f.TagNames()
	if len(names) != 2 {
		t.Fatalf("TagNames = %v, want 2 tags", names)
	}
	// Tag directory is sorted; BYCODE < BYNVAL.
	if names[0] != "BYCODE" || names[1] != "BYNVAL" {
		t.Errorf("tag order = %v, want [BYCODE BYNVAL]", names)
	}

	for i, spec := range specs {
		tag, err := f.Tag(spec.Name)
		if err != nil {
			t.Fatalf("Tag(%s): %v", spec.Name, err)
		}
		var seen []uint32
		err = f.Traverse(tag, func(e Entry) error {
			seen = append(seen, e.RecNo)
			return nil
		})
		if err != nil {
			t.Fatalf("Traverse(%s): %v", spec.Name, err)
		}
		if len(seen) != len(spec.Entries) {
			t.Errorf("tag %s: yielded %d entries, want %d", spec.Name, len(seen), len(spec.Entries))
			continue
		}
		for k, e := range spec.Entries {
			if seen[k] != e.RecNo {
				t.Errorf("tag %d entry %d: got recNo %d, want %d", i, k, seen[k], e.RecNo)
			}
		}
	}
}

// TestBuildRejectsUnsortedEntries proves the sortedness
// precondition is enforced. A silently-produced malformed CDX is
// exactly the kind of failure the whole compressed encoding makes
// invisible until traversal, and we prefer to catch it here.
func TestBuildRejectsUnsortedEntries(t *testing.T) {
	spec := TagSpec{
		Name:    "T",
		KeyExpr: "X",
		KeyLen:  5,
		Entries: []Entry{
			{Key: []byte("BBBB"), RecNo: 1},
			{Key: []byte("AAAA"), RecNo: 2}, // wrong order
		},
	}
	var buf bytes.Buffer
	if err := Build(&buf, []TagSpec{spec}); err == nil {
		t.Fatalf("Build accepted unsorted entries")
	}
}

// TestBuildRejectsOversizeKey guards the KeyLen contract at
// write time rather than deferring to reader confusion.
func TestBuildRejectsOversizeKey(t *testing.T) {
	spec := TagSpec{
		Name:    "T",
		KeyExpr: "X",
		KeyLen:  3,
		Entries: []Entry{
			{Key: []byte("TOOLONG"), RecNo: 1},
		},
	}
	var buf bytes.Buffer
	if err := Build(&buf, []TagSpec{spec}); err == nil {
		t.Fatal("Build accepted key longer than KeyLen")
	}
}

// TestRoundTripSeek verifies Seek works on a written file, not
// only on the reference fixture.
func TestRoundTripSeek(t *testing.T) {
	spec := TagSpec{
		Name:    "K",
		KeyExpr: "K",
		KeyLen:  5,
		Entries: []Entry{
			{Key: []byte("ANT"), RecNo: 10},
			{Key: []byte("BAT"), RecNo: 20},
			{Key: []byte("CAT"), RecNo: 30},
			{Key: []byte("DOG"), RecNo: 40},
		},
	}
	var buf bytes.Buffer
	if err := Build(&buf, []TagSpec{spec}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	f, _ := Open(bytes.NewReader(buf.Bytes()))
	tag, _ := f.Tag("K")

	e, exact, err := f.Seek(tag, []byte("CAT"))
	if err != nil || !exact || e.RecNo != 30 {
		t.Errorf("Seek(CAT): entry=%+v exact=%v err=%v", e, exact, err)
	}
	e, exact, err = f.Seek(tag, []byte("BEE"))
	if err != nil || exact {
		t.Errorf("Seek(BEE): unexpected exact match")
	}
	if got := string(bytes.TrimRight(e.Key, " ")); got != "CAT" {
		t.Errorf("Seek(BEE): landed on %q, want CAT", got)
	}
}

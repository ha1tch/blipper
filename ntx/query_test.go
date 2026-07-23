package ntx

import (
	"bytes"
	"fmt"
	"testing"
)

func TestPointQueriesEmptyIndex(t *testing.T) {
	ix, _ := newTestIndex(t, 8)

	if _, found, err := ix.Min(); err != nil || found {
		t.Fatalf("Min on empty: found=%v err=%v", found, err)
	}
	if _, found, err := ix.Max(); err != nil || found {
		t.Fatalf("Max on empty: found=%v err=%v", found, err)
	}
	if _, found, err := ix.FirstGE([]byte("A")); err != nil || found {
		t.Fatalf("FirstGE on empty: found=%v err=%v", found, err)
	}
}

func TestMinMax(t *testing.T) {
	ix, _ := newTestIndex(t, 8)

	for i, name := range []string{"MID", "AAA", "ZZZ"} {
		ix.Insert([]byte(name), uint32(i+1))
	}

	min, found, err := ix.Min()
	if err != nil || !found {
		t.Fatalf("Min: found=%v err=%v", found, err)
	}
	if got := string(bytes.TrimRight(min.Key, " ")); got != "AAA" {
		t.Errorf("Min = %q", got)
	}

	max, found, err := ix.Max()
	if err != nil || !found {
		t.Fatalf("Max: found=%v err=%v", found, err)
	}
	if got := string(bytes.TrimRight(max.Key, " ")); got != "ZZZ" {
		t.Errorf("Max = %q", got)
	}
}

func TestFirstGE(t *testing.T) {
	ix, _ := newTestIndex(t, 6)

	for i, name := range []string{"ALPHA", "BRAVO", "DELTA"} {
		ix.Insert([]byte(name), uint32(i+1))
	}

	e, found, err := ix.FirstGE([]byte("B"))
	if err != nil || !found {
		t.Fatalf("FirstGE: found=%v err=%v", found, err)
	}
	if got := string(bytes.TrimRight(e.Key, " ")); got != "BRAVO" {
		t.Errorf("FirstGE(B) = %q", got)
	}

	// Exact hit.
	e, found, _ = ix.FirstGE([]byte("DELTA"))
	if !found || string(bytes.TrimRight(e.Key, " ")) != "DELTA" {
		t.Errorf("FirstGE(DELTA) found=%v key=%q", found, e.Key)
	}

	// Past the end.
	if _, found, _ := ix.FirstGE([]byte("ZZ")); found {
		t.Errorf("FirstGE(ZZ) found an entry")
	}
}

// TestSuccessorPredecessorWalk builds a deep index and walks it end
// to end in both directions using only point queries, verifying the
// walk matches the cursor traversal exactly.
func TestSuccessorPredecessorWalk(t *testing.T) {
	// Tiny fan-out for a multi-level tree.
	ix, _ := newTestIndex(t, 200)

	const n = 150

	for i := 0; i < n; i++ {
		// Duplicate keys every third entry exercise the recno
		// tiebreak in both directions.
		key := []byte(fmt.Sprintf("K%03d", i/3))

		if _, err := ix.Insert(key, uint32(i+1)); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	want := collect(t, ix)

	if len(want) != n {
		t.Fatalf("cursor found %d entries, want %d", len(want), n)
	}

	// Forward: Min then Successor to the end.
	entry, found, err := ix.Min()
	if err != nil || !found {
		t.Fatalf("Min: found=%v err=%v", found, err)
	}

	var forward []Entry

	for {
		forward = append(forward, entry)

		entry, found, err = ix.Successor(entry.Key, entry.Recno)
		if err != nil {
			t.Fatalf("Successor: %v", err)
		}
		if !found {
			break
		}
	}

	if len(forward) != len(want) {
		t.Fatalf("forward walk found %d entries, want %d", len(forward), len(want))
	}

	for i := range want {
		if !bytes.Equal(forward[i].Key, want[i].Key) ||
			forward[i].Recno != want[i].Recno {
			t.Fatalf("forward walk diverges at %d", i)
		}
	}

	// Backward: Max then Predecessor to the start.
	entry, found, err = ix.Max()
	if err != nil || !found {
		t.Fatalf("Max: found=%v err=%v", found, err)
	}

	var backward []Entry

	for {
		backward = append(backward, entry)

		entry, found, err = ix.Predecessor(entry.Key, entry.Recno)
		if err != nil {
			t.Fatalf("Predecessor: %v", err)
		}
		if !found {
			break
		}
	}

	if len(backward) != len(want) {
		t.Fatalf("backward walk found %d entries, want %d", len(backward), len(want))
	}

	for i := range want {
		j := len(backward) - 1 - i

		if !bytes.Equal(backward[j].Key, want[i].Key) ||
			backward[j].Recno != want[i].Recno {
			t.Fatalf("backward walk diverges at %d", i)
		}
	}
}

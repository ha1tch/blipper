package dbf

import (
	"bytes"
	"testing"
)

// TestDBaseIVMemoCreateAndRoundTrip writes a fresh memo file,
// appends two memos, and confirms Get reads back exactly what was
// written — the write-side counterpart to T-31's oracle tests
// against real dBASE 5.0 output.
func TestDBaseIVMemoCreateAndRoundTrip(t *testing.T) {
	buf := &memBuffer{}

	m, err := CreateDBaseIVMemo(buf, "TESTGEN", 512)
	if err != nil {
		t.Fatalf("CreateDBaseIVMemo: %v", err)
	}
	if got := m.TableName(); got != "TESTGEN" {
		t.Errorf("TableName() = %q, want TESTGEN", got)
	}
	if got := m.BlockSize(); got != 512 {
		t.Errorf("BlockSize() = %d, want 512", got)
	}
	if got := m.NextFree(); got != 1 {
		t.Errorf("NextFree() = %d, want 1", got)
	}

	block1, err := m.Append([]byte("first memo note"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if block1 != 1 {
		t.Errorf("first block = %d, want 1", block1)
	}

	block2, err := m.Append([]byte("second"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if block2 != 2 {
		t.Errorf("second block = %d, want 2", block2)
	}

	text1, err := m.Get(block1)
	if err != nil {
		t.Fatalf("Get(%d): %v", block1, err)
	}
	if string(text1) != "first memo note" {
		t.Errorf("Get(%d) = %q, want %q", block1, text1, "first memo note")
	}

	text2, err := m.Get(block2)
	if err != nil {
		t.Fatalf("Get(%d): %v", block2, err)
	}
	if string(text2) != "second" {
		t.Errorf("Get(%d) = %q, want %q", block2, text2, "second")
	}

	// Reopen from the same bytes, as a fresh reader would, and
	// confirm the header round-trips too.
	m2, err := OpenDBaseIVMemo(buf)
	if err != nil {
		t.Fatalf("OpenDBaseIVMemo (reopen): %v", err)
	}
	if got := m2.TableName(); got != "TESTGEN" {
		t.Errorf("reopened TableName() = %q, want TESTGEN", got)
	}
	if got := m2.NextFree(); got != 3 {
		t.Errorf("reopened NextFree() = %d, want 3", got)
	}
	text1Again, err := m2.Get(block1)
	if err != nil {
		t.Fatalf("reopened Get(%d): %v", block1, err)
	}
	if string(text1Again) != "first memo note" {
		t.Errorf("reopened Get(%d) = %q, want %q", block1, text1Again, "first memo note")
	}
}

// TestDBaseIVMemoWriteMultiBlock writes a memo longer than one
// block's usable capacity and confirms it reads back exactly —
// the write-side mirror of T-31's real-1994-data multi-block
// read test (CLIENT.DBT's 580-byte record).
func TestDBaseIVMemoWriteMultiBlock(t *testing.T) {
	buf := &memBuffer{}

	m, err := CreateDBaseIVMemo(buf, "BIGMEMO", 512)
	if err != nil {
		t.Fatalf("CreateDBaseIVMemo: %v", err)
	}

	// 504 usable bytes per block (512 - 8 header). 600 bytes of
	// content forces a genuine block-boundary crossing.
	want := bytes.Repeat([]byte("0123456789"), 60) // 600 bytes
	block, err := m.Append(want)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := m.Get(block)
	if err != nil {
		t.Fatalf("Get(%d): %v", block, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("multi-block round trip: got %d bytes, want %d bytes; equal=%v",
			len(got), len(want), bytes.Equal(got, want))
	}

	// Confirm it actually spanned more than one block, so this
	// test is exercising what it claims to.
	if m.NextFree() < 3 {
		t.Errorf("NextFree() = %d after a 600-byte memo; expected it to span at least 2 blocks past the header", m.NextFree())
	}
}

// TestDBaseIVMemoCreateRejectsBadBlockSize guards the API surface.
func TestDBaseIVMemoCreateRejectsBadBlockSize(t *testing.T) {
	for _, bs := range []uint16{0, 7, 100} {
		buf := &memBuffer{}
		if _, err := CreateDBaseIVMemo(buf, "X", bs); err == nil {
			t.Errorf("CreateDBaseIVMemo with block size %d succeeded, want error", bs)
		}
	}
}

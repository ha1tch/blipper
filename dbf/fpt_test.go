package dbf

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// memBuffer is a minimal in-memory io.ReadWriteSeeker used by
// tests that need one and want to inspect the resulting bytes.
type memBuffer struct {
	data []byte
	pos  int64
}

func (m *memBuffer) Read(p []byte) (int, error) {
	if m.pos >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += int64(n)
	return n, nil
}

func (m *memBuffer) Write(p []byte) (int, error) {
	end := m.pos + int64(len(p))
	if end > int64(len(m.data)) {
		grow := make([]byte, end)
		copy(grow, m.data)
		m.data = grow
	}
	copy(m.data[m.pos:end], p)
	m.pos = end
	return len(p), nil
}

func (m *memBuffer) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		m.pos = off
	case io.SeekCurrent:
		m.pos += off
	case io.SeekEnd:
		m.pos = int64(len(m.data)) + off
	}
	return m.pos, nil
}

func TestCreateFPTDefaultsToClipperBlockSize(t *testing.T) {
	buf := &memBuffer{}
	f, err := CreateFPT(buf, 0)
	if err != nil {
		t.Fatalf("CreateFPT: %v", err)
	}
	if f.BlockSize() != FPTDefaultBlockSize {
		t.Errorf("BlockSize = %d, want %d (Clipper default)",
			f.BlockSize(), FPTDefaultBlockSize)
	}
	// With Clipper's default 64-byte blocks, first usable = 8
	// (FPTHeaderSize/blockSize). NextFree points there on a
	// freshly created file with no memos written yet.
	wantNextFree := uint32(FPTHeaderSize) / uint32(f.BlockSize())
	if f.NextFree() != wantNextFree {
		t.Errorf("NextFree = %d, want %d on fresh FPT (block size %d)",
			f.NextFree(), wantNextFree, f.BlockSize())
	}
	// Header block written as 512 bytes regardless of block size.
	if len(buf.data) != FPTHeaderSize {
		t.Errorf("Create wrote %d bytes, want %d for header",
			len(buf.data), FPTHeaderSize)
	}
}

func TestFPTHeaderBytesAreBigEndian(t *testing.T) {
	// FPT differs from DBT in this specific respect: DBT stores
	// next-free as a little-endian uint32; FPT stores it as a
	// big-endian uint32. Guard the endian at write time so a
	// silent flip doesn't produce a file only blipper can read.
	buf := &memBuffer{}
	if _, err := CreateFPT(buf, 512); err != nil {
		t.Fatalf("CreateFPT: %v", err)
	}
	// After Create, next-free = 1 (block 0 is the header, block
	// 1 is the first free). Big-endian encoding of 1 is
	// 0x00 0x00 0x00 0x01.
	got := buf.data[0:4]
	want := []byte{0x00, 0x00, 0x00, 0x01}
	if !bytes.Equal(got, want) {
		t.Errorf("next-free bytes = %v, want %v (big-endian)", got, want)
	}
	// Block size at offset 6, big-endian uint16. 512 = 0x0200.
	gotBS := buf.data[6:8]
	wantBS := []byte{0x02, 0x00}
	if !bytes.Equal(gotBS, wantBS) {
		t.Errorf("block-size bytes = %v, want %v (big-endian)", gotBS, wantBS)
	}
}

func TestFPTRoundTripSingleMemo(t *testing.T) {
	buf := &memBuffer{}
	f, err := CreateFPT(buf, 64)
	if err != nil {
		t.Fatalf("CreateFPT: %v", err)
	}

	content := []byte("Hello from an FPT memo.")
	block, err := f.Append(content, MemoText)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	// With a 64-byte block size the first usable memo block is
	// FPTHeaderSize/blockSize = 512/64 = 8. This is Clipper
	// DBFCDX's convention and what a FoxPro reader expects; a
	// value of 1 would point inside the header block.
	if block != 8 {
		t.Errorf("first memo landed on block %d, want 8 (FPTHeaderSize/blockSize for block size 64)", block)
	}

	// Re-open from the raw bytes to prove the state is fully on
	// disk (rather than only in the writer's cache).
	reopen := &memBuffer{data: buf.data}
	g, err := OpenFPT(reopen)
	if err != nil {
		t.Fatalf("OpenFPT: %v", err)
	}
	if g.BlockSize() != 64 {
		t.Errorf("reopen BlockSize = %d, want 64", g.BlockSize())
	}

	got, typ, err := g.Get(block)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("Get returned %q, want %q", got, content)
	}
	if typ != MemoText {
		t.Errorf("Get returned type %d, want %d (MemoText)", typ, MemoText)
	}
}

func TestFPTMultipleMemosAcrossBlockBoundaries(t *testing.T) {
	buf := &memBuffer{}
	f, _ := CreateFPT(buf, 64)

	// With block size 64 and per-entry header of 8 bytes, a
	// single memo of 56 bytes fits in one block; 57 bytes takes
	// two. Craft memos that force multi-block spans.
	inputs := [][]byte{
		[]byte("short"),                // 5 bytes, 1 block
		bytes.Repeat([]byte("A"), 56),  // fits in 1 block
		bytes.Repeat([]byte("B"), 57),  // spans 2 blocks
		bytes.Repeat([]byte("C"), 200), // spans 4 blocks (8+200=208, ceil(208/64)=4)
		{},                             // empty memo, 1 block
		bytes.Repeat([]byte{0x00, 0x1A, 0xFF, 0xAB}, 30), // binary data (0x1A included to prove FPT is length-driven, not terminator-driven)
	}
	blocks := make([]uint32, len(inputs))
	for i, in := range inputs {
		b, err := f.Append(in, MemoText)
		if err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
		blocks[i] = b
	}

	// Reopen and read each back.
	reopen := &memBuffer{data: buf.data}
	g, err := OpenFPT(reopen)
	if err != nil {
		t.Fatalf("OpenFPT: %v", err)
	}
	for i, in := range inputs {
		got, typ, err := g.Get(blocks[i])
		if err != nil {
			t.Fatalf("Get[%d]: %v", i, err)
		}
		if !bytes.Equal(got, in) {
			t.Errorf("memo %d: got %q, want %q", i, got, in)
		}
		if typ != MemoText {
			t.Errorf("memo %d type: got %d, want %d", i, typ, MemoText)
		}
	}
}

func TestFPTBinaryMemoTypesRoundTrip(t *testing.T) {
	buf := &memBuffer{}
	f, _ := CreateFPT(buf, 64)

	pic := bytes.Repeat([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 20)
	obj := bytes.Repeat([]byte{0xCA, 0xFE, 0xBA, 0xBE}, 15)

	bp, err := f.Append(pic, MemoPicture)
	if err != nil {
		t.Fatalf("Append picture: %v", err)
	}
	bo, err := f.Append(obj, MemoObject)
	if err != nil {
		t.Fatalf("Append object: %v", err)
	}

	reopen := &memBuffer{data: buf.data}
	g, _ := OpenFPT(reopen)

	gp, tp, _ := g.Get(bp)
	if !bytes.Equal(gp, pic) || tp != MemoPicture {
		t.Errorf("picture round-trip: got type %d len %d, want %d %d", tp, len(gp), MemoPicture, len(pic))
	}
	go_, to, _ := g.Get(bo)
	if !bytes.Equal(go_, obj) || to != MemoObject {
		t.Errorf("object round-trip: got type %d len %d, want %d %d", to, len(go_), MemoObject, len(obj))
	}
}

func TestFPTRejectsBadBlockSize(t *testing.T) {
	// Too small
	if _, err := CreateFPT(&memBuffer{}, 16); err == nil {
		t.Error("CreateFPT(16) accepted; want rejection (below FPTMinBlockSize)")
	}
	// Too large
	if _, err := CreateFPT(&memBuffer{}, 2048); err == nil {
		t.Error("CreateFPT(2048) accepted; want rejection (above FPTMaxBlockSize)")
	}
	// Not a multiple of 32
	if _, err := CreateFPT(&memBuffer{}, 100); err == nil {
		t.Error("CreateFPT(100) accepted; want rejection (not a multiple of 32)")
	}
}

func TestFPTOpenRejectsCorruptHeader(t *testing.T) {
	// A header claiming block 0 is free.
	buf := &memBuffer{data: make([]byte, FPTHeaderSize)}
	// All zeros: next-free = 0, block size = 0.
	_, err := OpenFPT(buf)
	if err == nil || !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("OpenFPT on zero header: err=%v, want ErrInvalidRecord", err)
	}
}

func TestFPTGetBlockZeroIsError(t *testing.T) {
	buf := &memBuffer{}
	f, _ := CreateFPT(buf, 64)
	_, _, err := f.Get(0)
	if err == nil {
		t.Error("Get(0) should error; block 0 is the header, not a valid pointer")
	}
}

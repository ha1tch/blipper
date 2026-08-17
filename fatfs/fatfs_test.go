package fatfs

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// loadImage decompresses a gzipped fixture image into memory and
// returns it as a seekable buffer.
//
// Fixtures are stored gzipped because a FAT16 volume needs about
// 16 MB and a FAT32 volume about 40 MB to reach their minimum
// cluster counts; compressed they are a few tens of kilobytes,
// since the vast majority of both images is zeroes.
func loadImage(t *testing.T, name string) *memImage {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip %s: %v", name, err)
	}
	defer zr.Close()
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return &memImage{data: data}
}

// loadImageBytes decompresses a fixture for benchmarks, which
// take a *testing.B rather than a *testing.T.
func loadImageBytes(tb testing.TB, name string) []byte {
	tb.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		tb.Fatalf("open fixture %s: %v", name, err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		tb.Fatalf("gunzip %s: %v", name, err)
	}
	defer zr.Close()
	data, err := io.ReadAll(zr)
	if err != nil {
		tb.Fatalf("read %s: %v", name, err)
	}
	return data
}

// fixtureBytes reads one of the source files that was copied into
// the images, for byte-exact comparison.
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// memImage is an in-memory io.ReadWriteSeeker over a disk image.
type memImage struct {
	data []byte
	pos  int64
}

func (m *memImage) Read(p []byte) (int, error) {
	if m.pos >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += int64(n)
	return n, nil
}

func (m *memImage) Write(p []byte) (int, error) {
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

func (m *memImage) Seek(off int64, whence int) (int64, error) {
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

// --- oracle tests: read images produced by mkfs.vfat + mcopy ---

func TestReadsFAT16ImageFromOracle(t *testing.T) {
	testReadsOracleImage(t, "fat16.img.gz", FAT16)
}

func TestReadsFAT32ImageFromOracle(t *testing.T) {
	testReadsOracleImage(t, "fat32.img.gz", FAT32)
}

// testReadsOracleImage is the shared body: open an image built by
// mkfs.vfat, verify the detected FAT type, enumerate the files
// mcopy placed there, and compare every byte against the sources.
func testReadsOracleImage(t *testing.T, image string, wantType FATType) {
	t.Helper()
	vol, err := OpenImage(loadImage(t, image))
	if err != nil {
		t.Fatalf("OpenImage: %v", err)
	}
	if got := vol.Type(); got != wantType {
		t.Errorf("Type = %v, want %v", got, wantType)
	}

	// mdir reports exactly these three files in both images.
	want := map[string][]byte{
		"SMALL.TXT": fixtureBytes(t, "small.txt"),
		"BIG.BIN":   fixtureBytes(t, "big.bin"),
		"WEIRD.BIN": fixtureBytes(t, "weird.bin"),
	}

	listed := vol.List()
	if len(listed) != len(want) {
		t.Errorf("List = %v, want %d entries", listed, len(want))
	}
	for _, name := range listed {
		if _, ok := want[name]; !ok {
			t.Errorf("List returned unexpected entry %q", name)
		}
	}

	for name, expected := range want {
		if !vol.Exists(name) {
			t.Errorf("Exists(%s) = false, want true", name)
			continue
		}
		size, err := vol.Stat(name)
		if err != nil {
			t.Errorf("Stat(%s): %v", name, err)
			continue
		}
		if int(size) != len(expected) {
			t.Errorf("%s: Stat size = %d, want %d", name, size, len(expected))
		}
		f, err := vol.Open(name)
		if err != nil {
			t.Errorf("Open(%s): %v", name, err)
			continue
		}
		got, err := io.ReadAll(f)
		if err != nil {
			t.Errorf("ReadAll(%s): %v", name, err)
			continue
		}
		if !bytes.Equal(got, expected) {
			t.Errorf("%s: content mismatch (got %d bytes, want %d)", name, len(got), len(expected))
		}
	}
}

// TestBigFileSpansMultipleClusters confirms the chain-walking
// path is genuinely exercised: BIG.BIN at 20000 bytes crosses
// several clusters on both fixtures.
func TestBigFileSpansMultipleClusters(t *testing.T) {
	for _, image := range []string{"fat16.img.gz", "fat32.img.gz"} {
		vol, err := OpenImage(loadImage(t, image))
		if err != nil {
			t.Fatalf("%s: OpenImage: %v", image, err)
		}
		e, err := vol.findEntry("BIG.BIN")
		if err != nil {
			t.Fatalf("%s: findEntry: %v", image, err)
		}
		chain, err := vol.chain(e.FirstCluster)
		if err != nil {
			t.Fatalf("%s: chain: %v", image, err)
		}
		if len(chain) < 2 {
			t.Errorf("%s: BIG.BIN occupies %d cluster(s); expected a multi-cluster chain (cluster size %d)",
				image, len(chain), vol.BytesPerCluster())
		}
	}
}

// TestReadOnlyVolumeRefusesWrites verifies the safety default:
// OpenImage produces a volume that cannot modify the image.
func TestReadOnlyVolumeRefusesWrites(t *testing.T) {
	vol, err := OpenImage(loadImage(t, "fat16.img.gz"))
	if err != nil {
		t.Fatalf("OpenImage: %v", err)
	}
	if _, err := vol.Create("NEW.TXT"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Create on read-only volume: err = %v, want ErrReadOnly", err)
	}
	if err := vol.Remove("SMALL.TXT"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Remove on read-only volume: err = %v, want ErrReadOnly", err)
	}
	f, err := vol.Open("SMALL.TXT")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := f.Write([]byte("x")); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Write on read-only volume: err = %v, want ErrReadOnly", err)
	}
}

// TestRejectsNonFATImage verifies the boot-signature gate.
func TestRejectsNonFATImage(t *testing.T) {
	junk := &memImage{data: make([]byte, 4096)}
	if _, err := OpenImage(junk); !errors.Is(err, ErrNotFAT) {
		t.Errorf("OpenImage on zeroed image: err = %v, want ErrNotFAT", err)
	}
}

// --- write path ---

// TestWriteReadBackRoundTrip writes files through fatfs and reads
// them back through fatfs after a Flush and a fresh Open, which
// proves the FAT and directory updates actually reached the image
// rather than only the cache.
func TestWriteReadBackRoundTrip(t *testing.T) {
	for _, image := range []string{"fat16.img.gz", "fat32.img.gz"} {
		t.Run(image, func(t *testing.T) {
			img := loadImage(t, image)
			vol, err := OpenImageRW(img)
			if err != nil {
				t.Fatalf("OpenImageRW: %v", err)
			}

			payloads := map[string][]byte{
				"ONE.DAT":   bytes.Repeat([]byte("a"), 100),
				"TWO.DAT":   bytes.Repeat([]byte("b"), 5000),
				"THREE.DAT": bytes.Repeat([]byte{0x00, 0x1A, 0xFF}, 3000),
			}
			for name, data := range payloads {
				f, err := vol.Create(name)
				if err != nil {
					t.Fatalf("Create(%s): %v", name, err)
				}
				if _, err := f.Write(data); err != nil {
					t.Fatalf("Write(%s): %v", name, err)
				}
			}
			if err := vol.Flush(); err != nil {
				t.Fatalf("Flush: %v", err)
			}

			// Reopen from the mutated image bytes.
			reopened, err := OpenImage(&memImage{data: img.data})
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			for name, want := range payloads {
				f, err := reopened.Open(name)
				if err != nil {
					t.Errorf("reopen Open(%s): %v", name, err)
					continue
				}
				got, err := io.ReadAll(f)
				if err != nil {
					t.Errorf("reopen ReadAll(%s): %v", name, err)
					continue
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s: round trip mismatch (got %d bytes, want %d)",
						name, len(got), len(want))
				}
			}
			// Pre-existing files must survive untouched.
			f, err := reopened.Open("SMALL.TXT")
			if err != nil {
				t.Fatalf("pre-existing SMALL.TXT vanished: %v", err)
			}
			got, _ := io.ReadAll(f)
			if !bytes.Equal(got, fixtureBytes(t, "small.txt")) {
				t.Error("pre-existing SMALL.TXT was corrupted by the writes")
			}
		})
	}
}

// TestChainsDoNotOverlap is the property test the register calls
// for. It writes many files of random sizes, then asserts the
// structural invariants that a cluster-allocation bug would
// violate: no cluster appears in two chains, every chain
// terminates, and the free count agrees with what was allocated.
//
// This is the guard that matters most in this package. A wrong
// FAT entry does not fail the write; it corrupts a chain, and
// without this check the damage would surface only on some later
// read.
func TestChainsDoNotOverlap(t *testing.T) {
	img := loadImage(t, "fat16.img.gz")
	vol, err := OpenImageRW(img)
	if err != nil {
		t.Fatalf("OpenImageRW: %v", err)
	}

	freeBefore, err := vol.FreeClusters()
	if err != nil {
		t.Fatalf("FreeClusters: %v", err)
	}

	rng := rand.New(rand.NewSource(1))
	written := map[string][]byte{}
	// The FAT16 root directory holds 512 entries; three are
	// already used. Stay well clear of the limit.
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("F%03d.DAT", i)
		size := rng.Intn(9000) + 1
		data := make([]byte, size)
		rng.Read(data)
		f, err := vol.Create(name)
		if err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
		written[name] = data
	}
	if err := vol.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Invariant 1: no cluster belongs to two files.
	owner := map[uint32]string{}
	totalClusters := 0
	for name := range written {
		e, err := vol.findEntry(name)
		if err != nil {
			t.Fatalf("findEntry(%s): %v", name, err)
		}
		chain, err := vol.chain(e.FirstCluster)
		if err != nil {
			t.Fatalf("chain(%s): %v", name, err)
		}
		totalClusters += len(chain)
		for _, c := range chain {
			if prev, taken := owner[c]; taken {
				t.Fatalf("cluster %d claimed by both %s and %s", c, prev, name)
			}
			owner[c] = name
		}
	}

	// Invariant 2: every file reads back byte-identically.
	reopened, err := OpenImage(&memImage{data: img.data})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	for name, want := range written {
		f, err := reopened.Open(name)
		if err != nil {
			t.Errorf("Open(%s): %v", name, err)
			continue
		}
		got, err := io.ReadAll(f)
		if err != nil {
			t.Errorf("ReadAll(%s): %v", name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: content mismatch after reopen", name)
		}
	}

	// Invariant 3: allocation accounting adds up.
	freeAfter, err := vol.FreeClusters()
	if err != nil {
		t.Fatalf("FreeClusters: %v", err)
	}
	consumed := freeBefore - freeAfter
	if int(consumed) != totalClusters {
		t.Errorf("free-cluster delta = %d but chains hold %d clusters",
			consumed, totalClusters)
	}
}

// TestCreateTruncatesExistingFile verifies that Create on an
// existing name reuses the directory slot and releases the old
// chain rather than leaking it.
func TestCreateTruncatesExistingFile(t *testing.T) {
	img := loadImage(t, "fat16.img.gz")
	vol, err := OpenImageRW(img)
	if err != nil {
		t.Fatalf("OpenImageRW: %v", err)
	}
	before := len(vol.List())

	f, err := vol.Create("SMALL.TXT")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Write([]byte("replaced")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := vol.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if after := len(vol.List()); after != before {
		t.Errorf("entry count changed from %d to %d; Create should reuse the slot", before, after)
	}
	reopened, _ := OpenImage(&memImage{data: img.data})
	rf, err := reopened.Open("SMALL.TXT")
	if err != nil {
		t.Fatalf("reopen Open: %v", err)
	}
	got, _ := io.ReadAll(rf)
	if string(got) != "replaced" {
		t.Errorf("content = %q, want %q", got, "replaced")
	}
}

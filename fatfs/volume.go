package fatfs

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// Volume is an open FAT filesystem over an image.
//
// The FAT and (for FAT16) the root directory region are cached in
// memory from Open. Writes mutate the cache and are pushed to the
// image by Flush, so allocating a long chain touches memory
// repeatedly and the image once.
//
// A Volume is not safe for concurrent use.
type Volume struct {
	img       io.ReadWriteSeeker
	geo       *geometry
	readOnly  bool
	longNames bool

	// fat is the whole FAT as raw bytes, one copy. Entries are
	// decoded on access rather than kept as a []uint32, which
	// keeps writeback a straight byte-range copy.
	fat      []byte
	fatDirty bool

	// dir caches directory entries by their containing region.
	// For FAT16 the root is a fixed region; for FAT32 it is a
	// chain like any other directory.
	rootEntries []dirEntry
	rootDirty   bool
}

// Option configures a Volume at open time.
type Option func(*volumeConfig)

type volumeConfig struct {
	longNames bool
}

// WithLongNames enables VFAT long filename support: long names
// are reassembled on read and generated on write, alongside the
// 8.3 alias every file carries regardless.
//
// Off by default. xBase filenames are 8.3 by construction, so
// most callers gain nothing, and enabling it makes directory
// enumeration reassemble names across multiple entries and makes
// creation scan for contiguous slot runs and alias collisions.
// Turning it on is a decision, not a default.
//
// Note that clearing orphaned long-name entries on Create and
// Remove happens either way: those orphans are produced by
// writing a short entry over someone else's long name, and
// leaving them would mislead an LFN-aware reader whatever this
// volume was configured to do.
func WithLongNames(enabled bool) Option {
	return func(c *volumeConfig) { c.longNames = enabled }
}

// OpenImage opens a FAT volume read-only.
//
// Use this for images you do not intend to modify — which, for a
// vintage disk image, is the safe default. Write operations
// return ErrReadOnly.
func OpenImage(r io.ReadSeeker, opts ...Option) (*Volume, error) {
	// A read-only volume still satisfies the internal interface by
	// wrapping the reader; write paths check readOnly before
	// touching img.
	return openVolume(readOnlyImage{r}, true, opts...)
}

// OpenImageRW opens a FAT volume for reading and writing.
//
// Modifications are cached and applied on Flush. Callers are
// responsible for calling Flush before discarding the Volume;
// unflushed changes are lost.
func OpenImageRW(rw io.ReadWriteSeeker, opts ...Option) (*Volume, error) {
	return openVolume(rw, false, opts...)
}

func openVolume(img io.ReadWriteSeeker, readOnly bool, opts ...Option) (*Volume, error) {
	var cfg volumeConfig
	for _, o := range opts {
		o(&cfg)
	}
	b, err := readBPB(img)
	if err != nil {
		return nil, err
	}
	geo, err := deriveGeometry(b)
	if err != nil {
		return nil, err
	}
	v := &Volume{img: img, geo: geo, readOnly: readOnly, longNames: cfg.longNames}
	if err := v.loadFAT(); err != nil {
		return nil, err
	}
	if err := v.loadRoot(); err != nil {
		return nil, err
	}
	return v, nil
}

// Type returns the volume's FAT variant.
func (v *Volume) Type() FATType { return v.geo.fatType }

// BytesPerCluster returns the volume's allocation unit size.
func (v *Volume) BytesPerCluster() uint32 { return v.geo.bytesPerCluster }

// ClusterCount returns the number of data clusters.
func (v *Volume) ClusterCount() uint32 { return v.geo.countOfClusters }

// loadFAT reads FAT copy 0 into the cache.
func (v *Volume) loadFAT() error {
	size := int64(v.geo.fatSectors) * int64(v.geo.bytesPerSector)
	off := int64(v.geo.fatStartSector) * int64(v.geo.bytesPerSector)
	if _, err := v.img.Seek(off, io.SeekStart); err != nil {
		return fmt.Errorf("fatfs: seek to FAT: %w", err)
	}
	v.fat = make([]byte, size)
	if _, err := io.ReadFull(v.img, v.fat); err != nil {
		return fmt.Errorf("fatfs: read FAT: %w", err)
	}
	return nil
}

// fatEntry reads a FAT entry from the cache.
func (v *Volume) fatEntry(cluster uint32) (uint32, error) {
	if v.geo.fatType == FAT16 {
		off := int(cluster) * 2
		if off+2 > len(v.fat) {
			return 0, fmt.Errorf("%w: FAT16 entry %d past end of table", ErrCorrupt, cluster)
		}
		return uint32(binary.LittleEndian.Uint16(v.fat[off : off+2])), nil
	}
	off := int(cluster) * 4
	if off+4 > len(v.fat) {
		return 0, fmt.Errorf("%w: FAT32 entry %d past end of table", ErrCorrupt, cluster)
	}
	return binary.LittleEndian.Uint32(v.fat[off:off+4]) & 0x0FFFFFFF, nil
}

// setFATEntry writes a FAT entry to the cache and marks it dirty.
// FAT32 preserves the top four reserved bits of the existing
// entry, as the specification requires.
func (v *Volume) setFATEntry(cluster, value uint32) error {
	if v.geo.fatType == FAT16 {
		off := int(cluster) * 2
		if off+2 > len(v.fat) {
			return fmt.Errorf("%w: FAT16 entry %d past end of table", ErrCorrupt, cluster)
		}
		binary.LittleEndian.PutUint16(v.fat[off:off+2], uint16(value))
	} else {
		off := int(cluster) * 4
		if off+4 > len(v.fat) {
			return fmt.Errorf("%w: FAT32 entry %d past end of table", ErrCorrupt, cluster)
		}
		old := binary.LittleEndian.Uint32(v.fat[off : off+4])
		binary.LittleEndian.PutUint32(v.fat[off:off+4], (old&0xF0000000)|(value&0x0FFFFFFF))
	}
	v.fatDirty = true
	return nil
}

// chain walks a cluster chain from its first cluster, returning
// every cluster in order.
//
// Chain walking is where a corrupt volume does the most damage,
// so both failure modes are checked: a cluster number outside the
// data region, and a loop. The loop guard is a visited set rather
// than a step limit, which catches a chain that rejoins itself
// partway rather than only one that cycles from the start.
func (v *Volume) chain(first uint32) ([]uint32, error) {
	if first == 0 {
		return nil, nil // zero-length file: no clusters allocated
	}
	var out []uint32
	seen := map[uint32]bool{}
	c := first
	for {
		if !v.geo.validCluster(c) {
			return nil, fmt.Errorf("%w: cluster %d outside data region (2..%d)",
				ErrCorrupt, c, v.geo.countOfClusters+1)
		}
		if seen[c] {
			return nil, fmt.Errorf("%w: cluster chain loops at %d", ErrCorrupt, c)
		}
		seen[c] = true
		out = append(out, c)

		next, err := v.fatEntry(c)
		if err != nil {
			return nil, err
		}
		if v.geo.isEOC(next) {
			return out, nil
		}
		if next == 0 {
			return nil, fmt.Errorf("%w: chain reaches a free cluster after %d", ErrCorrupt, c)
		}
		c = next
	}
}

// allocate finds a free cluster, marks it as the end of a chain,
// and returns it. When prev is non-zero the new cluster is linked
// after it.
//
// The scan starts at cluster 2 every time rather than tracking a
// hint. That is O(clusters) per allocation in the worst case, but
// the FAT is in memory, so a full scan of a large FAT32 table is
// a few hundred microseconds — and correctness here matters more
// than the constant factor.
func (v *Volume) allocate(prev uint32) (uint32, error) {
	if v.readOnly {
		return 0, ErrReadOnly
	}
	for c := uint32(2); c < v.geo.countOfClusters+2; c++ {
		entry, err := v.fatEntry(c)
		if err != nil {
			return 0, err
		}
		if entry != 0 {
			continue
		}
		if err := v.setFATEntry(c, v.geo.eocMarker()); err != nil {
			return 0, err
		}
		if prev != 0 {
			if err := v.setFATEntry(prev, c); err != nil {
				return 0, err
			}
		}
		return c, nil
	}
	return 0, ErrNoSpace
}

// freeChain releases every cluster in a chain.
func (v *Volume) freeChain(first uint32) error {
	if v.readOnly {
		return ErrReadOnly
	}
	if first == 0 {
		return nil
	}
	clusters, err := v.chain(first)
	if err != nil {
		return err
	}
	for _, c := range clusters {
		if err := v.setFATEntry(c, 0); err != nil {
			return err
		}
	}
	return nil
}

// readCluster reads one cluster's bytes into dst, which must be
// exactly bytesPerCluster long.
func (v *Volume) readCluster(cluster uint32, dst []byte) error {
	if !v.geo.validCluster(cluster) {
		return fmt.Errorf("%w: read of invalid cluster %d", ErrCorrupt, cluster)
	}
	if _, err := v.img.Seek(v.geo.clusterOffset(cluster), io.SeekStart); err != nil {
		return err
	}
	_, err := io.ReadFull(v.img, dst)
	return err
}

// writeCluster writes one cluster's bytes.
func (v *Volume) writeCluster(cluster uint32, src []byte) error {
	if v.readOnly {
		return ErrReadOnly
	}
	if !v.geo.validCluster(cluster) {
		return fmt.Errorf("%w: write to invalid cluster %d", ErrCorrupt, cluster)
	}
	if _, err := v.img.Seek(v.geo.clusterOffset(cluster), io.SeekStart); err != nil {
		return err
	}
	_, err := v.img.Write(src)
	return err
}

// Flush writes cached FAT and directory changes to the image.
//
// Every FAT copy is updated, not just copy 0: a volume whose
// copies disagree is one that other drivers may read
// inconsistently, and keeping them identical is cheap.
//
// This is the commit point. Clipper's own COMMIT flushes buffers
// for a work area; this is the volume-level equivalent.
func (v *Volume) Flush() error {
	if v.readOnly {
		return nil
	}
	if v.rootDirty {
		if err := v.flushRoot(); err != nil {
			return err
		}
		v.rootDirty = false
	}
	if v.fatDirty {
		fatBytes := int64(v.geo.fatSectors) * int64(v.geo.bytesPerSector)
		for i := uint32(0); i < v.geo.numFATs; i++ {
			off := (int64(v.geo.fatStartSector) + int64(i)*int64(v.geo.fatSectors)) *
				int64(v.geo.bytesPerSector)
			if _, err := v.img.Seek(off, io.SeekStart); err != nil {
				return fmt.Errorf("fatfs: seek to FAT copy %d: %w", i, err)
			}
			if _, err := v.img.Write(v.fat[:fatBytes]); err != nil {
				return fmt.Errorf("fatfs: write FAT copy %d: %w", i, err)
			}
		}
		v.fatDirty = false
	}
	return nil
}

// FreeClusters counts unallocated clusters. Useful for tests that
// assert allocation accounting, and for callers checking capacity
// before a large write.
func (v *Volume) FreeClusters() (uint32, error) {
	var free uint32
	for c := uint32(2); c < v.geo.countOfClusters+2; c++ {
		entry, err := v.fatEntry(c)
		if err != nil {
			return 0, err
		}
		if entry == 0 {
			free++
		}
	}
	return free, nil
}

// readOnlyImage adapts an io.ReadSeeker to the io.ReadWriteSeeker
// the Volume holds. Writes are impossible by construction; the
// readOnly flag stops them before they reach here, and this
// returns an error if one ever slips through.
type readOnlyImage struct {
	io.ReadSeeker
}

func (readOnlyImage) Write([]byte) (int, error) { return 0, ErrReadOnly }

// normaliseName converts a filename to the 11-byte on-disk 8.3
// form: name padded to 8, extension padded to 3, no dot, upper
// case. Returns an error for names that cannot be represented.
func normaliseName(name string) ([11]byte, error) {
	var out [11]byte
	for i := range out {
		out[i] = ' '
	}
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return out, fmt.Errorf("fatfs: empty filename")
	}
	base, ext := name, ""
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		base, ext = name[:dot], name[dot+1:]
	}
	if len(base) > 8 || len(ext) > 3 {
		return out, fmt.Errorf("fatfs: %q is not a valid 8.3 name", name)
	}
	copy(out[0:8], base)
	copy(out[8:11], ext)
	return out, nil
}

// formatName converts an on-disk 11-byte 8.3 field back to a
// conventional NAME.EXT string.
func formatName(raw [11]byte) string {
	base := strings.TrimRight(string(raw[0:8]), " ")
	ext := strings.TrimRight(string(raw[8:11]), " ")
	if ext == "" {
		return base
	}
	return base + "." + ext
}

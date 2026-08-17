// Package fatfs implements a FAT16 and FAT32 filesystem driver
// over an image held in an io.ReadWriteSeeker.
//
// The package depends only on the standard library and knows
// nothing about its consumers. Reading a disk image is a
// self-contained problem and this package treats it as one.
//
// Coverage and deliberate exclusions:
//
//   - FAT16 and FAT32 are supported. FAT12 is not: its 12-bit
//     entries straddle byte boundaries, and no realistic dataset
//     arrives on a volume small enough to require it.
//   - Short (8.3) names only. VFAT long-name entries are skipped
//     on read; every FAT32 volume carries an 8.3 alias for every
//     file, so short-name-only enumeration is correct, it simply
//     reports CUSTOM~1.DBF rather than a long name.
//   - Volume creation is out of scope. An image is supplied,
//     not made.
//
// Read and write are separated at the constructor:
//
//	vol, err := fatfs.OpenImage(r)     // read-only
//	vol, err := fatfs.OpenImageRW(rw)  // read-write
//
// This is deliberate. A wrong FAT entry does not fail loudly; it
// corrupts a cluster chain and the damage surfaces on some later
// read. Vintage images are often the only copy of what they hold,
// so modifying one must be an explicit decision rather than an
// available side effect.
//
// Writes go through a write-back cache: the FAT and directory
// regions are held in memory and written on Flush, so allocating
// many clusters touches memory many times and the image once.
package fatfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// FATType identifies which FAT variant a volume uses.
type FATType int

const (
	// FAT16 volumes have 16-bit FAT entries and a fixed-size
	// root directory region.
	FAT16 FATType = 16

	// FAT32 volumes have 32-bit FAT entries (28 usable) and a
	// root directory that is an ordinary cluster chain.
	FAT32 FATType = 32
)

func (t FATType) String() string {
	switch t {
	case FAT16:
		return "FAT16"
	case FAT32:
		return "FAT32"
	default:
		return fmt.Sprintf("FAT%d", int(t))
	}
}

// Errors returned by this package.
var (
	// ErrNotFAT is returned when the image has no recognisable
	// boot sector or reports geometry that cannot be a FAT volume.
	ErrNotFAT = errors.New("fatfs: not a FAT volume")

	// ErrUnsupportedFAT is returned for FAT12 volumes, which this
	// package deliberately does not implement.
	ErrUnsupportedFAT = errors.New("fatfs: unsupported FAT type (FAT12 not implemented)")

	// ErrNotFound is returned when a named file is absent.
	ErrNotFound = errors.New("fatfs: file not found")

	// ErrReadOnly is returned by write operations on a volume
	// opened with OpenImage rather than OpenImageRW.
	ErrReadOnly = errors.New("fatfs: volume is read-only")

	// ErrCorrupt indicates a structural inconsistency: a chain
	// that loops, a cluster number past the end of the volume,
	// a directory entry that cannot be decoded.
	ErrCorrupt = errors.New("fatfs: corrupt volume structure")

	// ErrNoSpace is returned when allocation finds no free
	// cluster.
	ErrNoSpace = errors.New("fatfs: no free clusters")

	// ErrExists is returned by Create when a name is taken and
	// truncation is not possible.
	ErrExists = errors.New("fatfs: file already exists")
)

// bpb holds the BIOS Parameter Block fields this driver uses,
// decoded from the boot sector. Field names follow the Microsoft
// FAT specification so they can be checked against it directly.
type bpb struct {
	BytesPerSector    uint16 // BPB_BytsPerSec
	SectorsPerCluster uint8  // BPB_SecPerClus
	ReservedSectors   uint16 // BPB_RsvdSecCnt
	NumFATs           uint8  // BPB_NumFATs
	RootEntryCount    uint16 // BPB_RootEntCnt  (0 on FAT32)
	TotalSectors16    uint16 // BPB_TotSec16
	FATSize16         uint16 // BPB_FATSz16     (0 on FAT32)
	TotalSectors32    uint32 // BPB_TotSec32
	FATSize32         uint32 // BPB_FATSz32     (FAT32 only)
	RootCluster       uint32 // BPB_RootClus    (FAT32 only)
}

// geometry holds the derived layout of a volume: where each
// region starts and how large it is. Computed once at Open.
type geometry struct {
	fatType FATType

	bytesPerSector    uint32
	sectorsPerCluster uint32
	bytesPerCluster   uint32

	fatStartSector  uint32 // first sector of FAT #0
	fatSectors      uint32 // sectors per FAT copy
	numFATs         uint32
	rootStartSector uint32 // FAT16 only: first sector of root dir
	rootSectors     uint32 // FAT16 only: sectors in root dir
	rootCluster     uint32 // FAT32 only: first cluster of root dir
	dataStartSector uint32 // first sector of the data region
	countOfClusters uint32 // number of data clusters
}

// readBPB decodes the boot sector at offset 0.
func readBPB(r io.ReadSeeker) (*bpb, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("fatfs: seek to boot sector: %w", err)
	}
	var raw [512]byte
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return nil, fmt.Errorf("fatfs: read boot sector: %w", err)
	}

	// The boot signature at 510-511 is 0x55 0xAA on every
	// well-formed volume. Its absence is the cheapest reliable
	// "this is not a FAT image" test.
	if raw[510] != 0x55 || raw[511] != 0xAA {
		return nil, fmt.Errorf("%w: boot signature is %02X %02X, want 55 AA",
			ErrNotFAT, raw[510], raw[511])
	}

	b := &bpb{
		BytesPerSector:    binary.LittleEndian.Uint16(raw[11:13]),
		SectorsPerCluster: raw[13],
		ReservedSectors:   binary.LittleEndian.Uint16(raw[14:16]),
		NumFATs:           raw[16],
		RootEntryCount:    binary.LittleEndian.Uint16(raw[17:19]),
		TotalSectors16:    binary.LittleEndian.Uint16(raw[19:21]),
		FATSize16:         binary.LittleEndian.Uint16(raw[22:24]),
		TotalSectors32:    binary.LittleEndian.Uint32(raw[32:36]),
		FATSize32:         binary.LittleEndian.Uint32(raw[36:40]),
		RootCluster:       binary.LittleEndian.Uint32(raw[44:48]),
	}

	// Sanity checks drawn from the Microsoft specification's own
	// validity rules. A volume failing these is not FAT, whatever
	// the boot signature says.
	switch b.BytesPerSector {
	case 512, 1024, 2048, 4096:
	default:
		return nil, fmt.Errorf("%w: bytes-per-sector %d is not a legal value",
			ErrNotFAT, b.BytesPerSector)
	}
	switch b.SectorsPerCluster {
	case 1, 2, 4, 8, 16, 32, 64, 128:
	default:
		return nil, fmt.Errorf("%w: sectors-per-cluster %d is not a power of two in 1..128",
			ErrNotFAT, b.SectorsPerCluster)
	}
	if b.ReservedSectors == 0 {
		return nil, fmt.Errorf("%w: reserved sector count is zero", ErrNotFAT)
	}
	if b.NumFATs == 0 {
		return nil, fmt.Errorf("%w: FAT count is zero", ErrNotFAT)
	}
	return b, nil
}

// deriveGeometry computes region layout and determines the FAT
// type, following the Microsoft specification's cluster-count
// rule. That rule is the only correct way to tell FAT16 from
// FAT32; the strings in the boot sector's filesystem-type field
// are informational and are not consulted.
func deriveGeometry(b *bpb) (*geometry, error) {
	fatSize := uint32(b.FATSize16)
	if fatSize == 0 {
		fatSize = b.FATSize32
	}
	if fatSize == 0 {
		return nil, fmt.Errorf("%w: both FAT size fields are zero", ErrNotFAT)
	}

	totalSectors := uint32(b.TotalSectors16)
	if totalSectors == 0 {
		totalSectors = b.TotalSectors32
	}
	if totalSectors == 0 {
		return nil, fmt.Errorf("%w: both total-sector fields are zero", ErrNotFAT)
	}

	bytesPerSector := uint32(b.BytesPerSector)
	// Root directory occupies a whole number of sectors, rounded up.
	rootDirSectors := (uint32(b.RootEntryCount)*32 + bytesPerSector - 1) / bytesPerSector

	dataStart := uint32(b.ReservedSectors) + uint32(b.NumFATs)*fatSize + rootDirSectors
	if dataStart >= totalSectors {
		return nil, fmt.Errorf("%w: data region starts at sector %d of %d",
			ErrNotFAT, dataStart, totalSectors)
	}
	dataSectors := totalSectors - dataStart
	countOfClusters := dataSectors / uint32(b.SectorsPerCluster)

	// The specification's determination rule, verbatim: fewer than
	// 4085 clusters is FAT12, fewer than 65525 is FAT16, otherwise
	// FAT32.
	var fatType FATType
	switch {
	case countOfClusters < 4085:
		return nil, fmt.Errorf("%w: volume has %d clusters (FAT12 range)",
			ErrUnsupportedFAT, countOfClusters)
	case countOfClusters < 65525:
		fatType = FAT16
	default:
		fatType = FAT32
	}

	g := &geometry{
		fatType:           fatType,
		bytesPerSector:    bytesPerSector,
		sectorsPerCluster: uint32(b.SectorsPerCluster),
		bytesPerCluster:   bytesPerSector * uint32(b.SectorsPerCluster),
		fatStartSector:    uint32(b.ReservedSectors),
		fatSectors:        fatSize,
		numFATs:           uint32(b.NumFATs),
		dataStartSector:   dataStart,
		countOfClusters:   countOfClusters,
	}
	if fatType == FAT16 {
		g.rootStartSector = uint32(b.ReservedSectors) + uint32(b.NumFATs)*fatSize
		g.rootSectors = rootDirSectors
	} else {
		g.rootCluster = b.RootCluster
		if g.rootCluster < 2 {
			return nil, fmt.Errorf("%w: FAT32 root cluster is %d", ErrCorrupt, g.rootCluster)
		}
	}
	return g, nil
}

// clusterOffset returns the byte offset of a data cluster.
// Cluster numbering starts at 2; clusters 0 and 1 are reserved
// and have no on-disk data region.
func (g *geometry) clusterOffset(cluster uint32) int64 {
	sector := g.dataStartSector + (cluster-2)*g.sectorsPerCluster
	return int64(sector) * int64(g.bytesPerSector)
}

// validCluster reports whether a cluster number addresses real
// data on this volume.
func (g *geometry) validCluster(cluster uint32) bool {
	return cluster >= 2 && cluster < g.countOfClusters+2
}

// isEOC reports whether a FAT entry marks the end of a chain.
func (g *geometry) isEOC(entry uint32) bool {
	if g.fatType == FAT16 {
		return entry >= 0xFFF8
	}
	return (entry & 0x0FFFFFFF) >= 0x0FFFFFF8
}

// eocMarker returns the value written to terminate a chain.
func (g *geometry) eocMarker() uint32 {
	if g.fatType == FAT16 {
		return 0xFFFF
	}
	return 0x0FFFFFFF
}

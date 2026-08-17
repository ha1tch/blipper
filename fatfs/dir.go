package fatfs

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// Directory entry attribute bits.
const (
	attrReadOnly  = 0x01
	attrHidden    = 0x02
	attrSystem    = 0x04
	attrVolumeID  = 0x08
	attrDirectory = 0x10
	attrArchive   = 0x20

	// attrLongName marks a VFAT long-name fragment. This driver
	// skips those entries: every file also has an 8.3 alias, so
	// short-name enumeration remains correct and complete.
	attrLongName = attrReadOnly | attrHidden | attrSystem | attrVolumeID // 0x0F
)

// Sentinel values in the first byte of a directory entry.
const (
	entryFree     = 0xE5 // deleted, slot reusable
	entryEndOfDir = 0x00 // this and all following entries are unused
	entryKanjiE5  = 0x05 // real first byte is 0xE5 (Kanji lead byte)
	dirEntrySize  = 32
)

// dirEntry is one decoded 32-byte directory entry.
type dirEntry struct {
	Name         [11]byte // 8.3, space padded, no dot
	Attr         uint8
	FirstCluster uint32
	Size         uint32

	// slot records where this entry lives so writes can find it
	// again: the index within the containing directory region.
	slot int

	// free marks a slot that holds no live file, either never
	// used or deleted.
	free bool

	// longName is the reassembled VFAT name when the entry is
	// preceded by a valid long-name run and the volume has long
	// names enabled. Empty otherwise.
	longName string

	// lfnStart is the slot index of the first long-name entry
	// belonging to this file, or equal to slot when there is
	// none. Create and Remove clear this range so orphaned
	// long-name entries do not outlive the file they described.
	lfnStart int

	// raw holds the verbatim 32 bytes of a long-name slot, which
	// has no field structure worth decoding into and is written
	// back unchanged.
	raw []byte
}

// isFile reports whether the entry describes a regular file, as
// opposed to a directory, volume label, or long-name fragment.
func (e *dirEntry) isFile() bool {
	if e.free {
		return false
	}
	if e.Attr&attrLongName == attrLongName {
		return false // VFAT long-name fragment
	}
	if e.Attr&(attrDirectory|attrVolumeID) != 0 {
		return false
	}
	return true
}

// decodeDirEntry reads one 32-byte entry.
func decodeDirEntry(raw []byte, slot int, fatType FATType) dirEntry {
	e := dirEntry{slot: slot}
	copy(e.Name[:], raw[0:11])
	e.Attr = raw[11]
	e.Size = binary.LittleEndian.Uint32(raw[28:32])

	low := uint32(binary.LittleEndian.Uint16(raw[26:28]))
	if fatType == FAT32 {
		high := uint32(binary.LittleEndian.Uint16(raw[20:22]))
		e.FirstCluster = high<<16 | low
	} else {
		e.FirstCluster = low
	}

	switch raw[0] {
	case entryEndOfDir, entryFree:
		e.free = true
	case entryKanjiE5:
		e.Name[0] = 0xE5
	}
	return e
}

// encodeDirEntry writes an entry back into a 32-byte slot,
// preserving the fields this driver does not model (creation and
// access timestamps, the NT reserved byte) so a round trip
// through fatfs does not discard metadata another tool wrote.
func encodeDirEntry(raw []byte, e *dirEntry, fatType FATType) {
	copy(raw[0:11], e.Name[:])
	raw[11] = e.Attr
	binary.LittleEndian.PutUint32(raw[28:32], e.Size)
	binary.LittleEndian.PutUint16(raw[26:28], uint16(e.FirstCluster&0xFFFF))
	if fatType == FAT32 {
		binary.LittleEndian.PutUint16(raw[20:22], uint16(e.FirstCluster>>16))
	}
}

// loadRoot reads the root directory into the cache. FAT16's root
// is a fixed region; FAT32's is an ordinary cluster chain, which
// means the FAT32 path reuses the same chain-walking code as any
// other file.
func (v *Volume) loadRoot() error {
	raw, err := v.readRootBytes()
	if err != nil {
		return err
	}
	// Every slot is decoded, not just those up to the first
	// end-of-directory marker.
	//
	// Enumeration can stop at the first 0x00 — nothing live
	// follows it. Allocation cannot: the slots after it are
	// precisely the free space a new file needs. Loading only
	// as far as the marker leaves allocEntry with nothing to
	// hand out on a volume that is mostly empty, which is the
	// common case.
	//
	// isFile() already filters the free slots out of List and
	// findEntry, so carrying them costs a little memory and
	// buys a working write path.
	v.rootEntries = nil

	// pendingLFN collects the long-name run preceding a short
	// entry. The run is in reverse sequence order on disk, which
	// assembleLongName accounts for.
	var pendingLFN [][]byte
	pendingStart := -1

	for i := 0; i+dirEntrySize <= len(raw); i += dirEntrySize {
		slot := i / dirEntrySize
		chunk := raw[i : i+dirEntrySize]

		// A long-name fragment. Collect it and move on; it
		// describes the short entry that follows.
		if chunk[11] == attrLongName && chunk[0] != entryFree && chunk[0] != entryEndOfDir {
			if pendingStart < 0 {
				pendingStart = slot
			}
			buf := make([]byte, dirEntrySize)
			copy(buf, chunk)
			pendingLFN = append(pendingLFN, buf)
			// The slot still needs a placeholder so indices line
			// up with the on-disk region.
			v.rootEntries = append(v.rootEntries, dirEntry{
				slot: slot, free: false, lfnStart: slot,
				Attr: attrLongName, raw: buf,
			})
			continue
		}

		e := decodeDirEntry(chunk, slot, v.geo.fatType)
		e.lfnStart = slot
		if len(pendingLFN) > 0 {
			if v.longNames && e.isFile() {
				// The checksum decides whether this run really
				// belongs to this short entry. A tool that
				// rewrote the short name leaves a run pointing
				// at a name that is no longer there, and
				// reporting it would name the wrong file.
				if name, ok := assembleLongName(pendingLFN, e.Name); ok {
					e.longName = name
				}
			}
			// Whether or not the name was used, the run belongs
			// to this entry and must be cleared with it.
			e.lfnStart = pendingStart
			pendingLFN = nil
			pendingStart = -1
		}
		v.rootEntries = append(v.rootEntries, e)
	}
	return nil
}

// readRootBytes returns the raw bytes of the root directory.
func (v *Volume) readRootBytes() ([]byte, error) {
	if v.geo.fatType == FAT16 {
		size := int64(v.geo.rootSectors) * int64(v.geo.bytesPerSector)
		off := int64(v.geo.rootStartSector) * int64(v.geo.bytesPerSector)
		if _, err := v.img.Seek(off, io.SeekStart); err != nil {
			return nil, fmt.Errorf("fatfs: seek to root directory: %w", err)
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(v.img, buf); err != nil {
			return nil, fmt.Errorf("fatfs: read root directory: %w", err)
		}
		return buf, nil
	}

	// FAT32: the root is a chain like any other directory.
	clusters, err := v.chain(v.geo.rootCluster)
	if err != nil {
		return nil, fmt.Errorf("fatfs: walk root directory chain: %w", err)
	}
	buf := make([]byte, 0, len(clusters)*int(v.geo.bytesPerCluster))
	cbuf := make([]byte, v.geo.bytesPerCluster)
	for _, c := range clusters {
		if err := v.readCluster(c, cbuf); err != nil {
			return nil, err
		}
		buf = append(buf, cbuf...)
	}
	return buf, nil
}

// flushRoot writes cached root directory entries back to the image.
func (v *Volume) flushRoot() error {
	raw, err := v.readRootBytes()
	if err != nil {
		return err
	}
	for i := range v.rootEntries {
		e := &v.rootEntries[i]
		off := e.slot * dirEntrySize
		if off+dirEntrySize > len(raw) {
			return fmt.Errorf("%w: directory slot %d past end of region", ErrCorrupt, e.slot)
		}
		// A long-name slot is written back verbatim: it has no
		// field structure this driver models.
		if !e.free && e.raw != nil {
			copy(raw[off:off+dirEntrySize], e.raw)
			continue
		}
		if e.free {
			// Preserve the distinction between never-used (0x00)
			// and deleted (0xE5): overwriting the former with the
			// latter would defeat the end-of-directory shortcut
			// every other FAT driver relies on.
			if raw[off] != entryEndOfDir {
				raw[off] = entryFree
			}
			continue
		}
		encodeDirEntry(raw[off:off+dirEntrySize], e, v.geo.fatType)
	}
	return v.writeRootBytes(raw)
}

// writeRootBytes writes the root directory region back.
func (v *Volume) writeRootBytes(raw []byte) error {
	if v.geo.fatType == FAT16 {
		off := int64(v.geo.rootStartSector) * int64(v.geo.bytesPerSector)
		if _, err := v.img.Seek(off, io.SeekStart); err != nil {
			return err
		}
		_, err := v.img.Write(raw)
		return err
	}
	clusters, err := v.chain(v.geo.rootCluster)
	if err != nil {
		return err
	}
	cs := int(v.geo.bytesPerCluster)
	for i, c := range clusters {
		start := i * cs
		if start >= len(raw) {
			break
		}
		end := start + cs
		if end > len(raw) {
			end = len(raw)
		}
		chunk := make([]byte, cs)
		copy(chunk, raw[start:end])
		if err := v.writeCluster(c, chunk); err != nil {
			return err
		}
	}
	return nil
}

// findEntry locates a live file entry by name.
func (v *Volume) findEntry(name string) (*dirEntry, error) {
	// A long name matches first when the volume carries them, so
	// a caller that wrote "Customers Archive.dbf" can open it by
	// that name rather than having to know its alias.
	if v.longNames {
		for i := range v.rootEntries {
			e := &v.rootEntries[i]
			if e.isFile() && e.longName != "" && strings.EqualFold(e.longName, name) {
				return e, nil
			}
		}
	}
	want, err := normaliseName(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	for i := range v.rootEntries {
		e := &v.rootEntries[i]
		if e.isFile() && e.Name == want {
			return e, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// List returns the names of every regular file in the root
// directory, in on-disk order.
func (v *Volume) List() []string {
	var out []string
	for i := range v.rootEntries {
		e := &v.rootEntries[i]
		if !e.isFile() {
			continue
		}
		if v.longNames && e.longName != "" {
			out = append(out, e.longName)
			continue
		}
		out = append(out, formatName(e.Name))
	}
	return out
}

// Exists reports whether a regular file with the given name is
// present.
func (v *Volume) Exists(name string) bool {
	_, err := v.findEntry(name)
	return err == nil
}

// Stat returns the size of a named file.
func (v *Volume) Stat(name string) (size uint32, err error) {
	e, err := v.findEntry(name)
	if err != nil {
		return 0, err
	}
	return e.Size, nil
}

// allocEntry finds a free directory slot, or returns an error if
// the root directory is full. On FAT16 the root has a fixed
// capacity and cannot grow, which is a real limit worth reporting
// clearly rather than as a generic failure.
func (v *Volume) allocEntry() (*dirEntry, error) {
	for i := range v.rootEntries {
		if v.rootEntries[i].free {
			return &v.rootEntries[i], nil
		}
	}
	return nil, fmt.Errorf("fatfs: root directory is full (%d entries)", len(v.rootEntries))
}

// allocEntryRun finds n consecutive free slots and returns the
// index of the first.
//
// A long name needs its LFN entries immediately before the short
// entry they describe, so the slots must be contiguous — any free
// slot will not do. On FAT16 the root directory cannot grow, so a
// fragmented directory can fail to place a long name while free
// slots remain scattered elsewhere. That is a real condition and
// is reported as such rather than as a generic "directory full".
func (v *Volume) allocEntryRun(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("fatfs: run length %d", n)
	}
	run := 0
	for i := range v.rootEntries {
		if v.rootEntries[i].free {
			run++
			if run == n {
				return i - n + 1, nil
			}
			continue
		}
		run = 0
	}
	free := 0
	for i := range v.rootEntries {
		if v.rootEntries[i].free {
			free++
		}
	}
	if free >= n {
		return 0, fmt.Errorf("fatfs: %d free directory slots but no run of %d; directory is fragmented",
			free, n)
	}
	return 0, fmt.Errorf("fatfs: root directory is full (%d free slots, need %d)", free, n)
}

// clearLFNRun marks the long-name entries preceding a short entry
// as free.
//
// This runs whether or not long names are enabled. The orphans it
// removes are created by writing a short entry over a file that
// another tool had given a long name; leaving them behind would
// have an LFN-aware reader report a stale name whose checksum no
// longer matches anything.
func (v *Volume) clearLFNRun(e *dirEntry) {
	for slot := e.lfnStart; slot < e.slot; slot++ {
		if slot < 0 || slot >= len(v.rootEntries) {
			continue
		}
		v.rootEntries[slot].free = true
		v.rootEntries[slot].longName = ""
	}
	e.lfnStart = e.slot
}

// aliasTaken reports whether an 8.3 name is already used, for
// alias generation.
func (v *Volume) aliasTaken(short [11]byte) bool {
	for i := range v.rootEntries {
		e := &v.rootEntries[i]
		if e.isFile() && e.Name == short {
			return true
		}
	}
	return false
}

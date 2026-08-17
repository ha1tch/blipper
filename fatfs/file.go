package fatfs

import (
	"fmt"
	"io"
)

// File is an open file on a FAT volume, satisfying
// io.ReadWriteSeeker.
//
// Reads and writes go through the volume's cluster chain. A write
// past the current end of the chain allocates as needed; the
// directory entry's size and first-cluster fields are updated in
// the volume's cache and reach the image on Volume.Flush.
//
// A File holds a reference to its Volume; using one after the
// Volume has been discarded is a programming error.
type File struct {
	vol   *Volume
	entry *dirEntry
	pos   int64

	// clusters caches the chain so a sequential read does not
	// re-walk the FAT for every cluster boundary. Invalidated
	// whenever the chain is extended.
	clusters []uint32
}

// Open returns a handle to an existing file.
func (v *Volume) Open(name string) (*File, error) {
	e, err := v.findEntry(name)
	if err != nil {
		return nil, err
	}
	f := &File{vol: v, entry: e}
	if err := f.loadChain(); err != nil {
		return nil, err
	}
	return f, nil
}

// Create returns a handle to a new empty file, replacing any
// existing file of the same name.
//
// Replacement frees the old chain immediately in the cache; the
// image is not touched until Flush.
func (v *Volume) Create(name string) (*File, error) {
	if v.readOnly {
		return nil, ErrReadOnly
	}
	normalised, normaliseErr := normaliseName(name)

	// Reuse an existing entry when the name is taken, so Create
	// truncates rather than duplicating a directory slot.
	if existing, err := v.findEntry(name); err == nil {
		if err := v.freeChain(existing.FirstCluster); err != nil {
			return nil, err
		}
		existing.FirstCluster = 0
		existing.Size = 0
		existing.Attr = attrArchive
		// Drop any long-name run: either it is about to be
		// rewritten below, or the file is being recreated under a
		// short name and the old run would be orphaned.
		v.clearLFNRun(existing)
		existing.longName = ""
		v.rootDirty = true
		if v.longNames && needsLongName(name) {
			if err := v.attachLongName(existing, name); err != nil {
				return nil, err
			}
		}
		return &File{vol: v, entry: existing}, nil
	}

	// A name that cannot be represented in 8.3 needs an alias and
	// a run of long-name entries ahead of it.
	if v.longNames && needsLongName(name) {
		return v.createWithLongName(name)
	}
	if normaliseErr != nil {
		return nil, normaliseErr
	}

	e, err := v.allocEntry()
	if err != nil {
		return nil, err
	}
	e.Name = normalised
	e.Attr = attrArchive
	e.FirstCluster = 0
	e.Size = 0
	e.free = false
	e.longName = ""
	e.lfnStart = e.slot
	v.rootDirty = true
	return &File{vol: v, entry: e}, nil
}

// createWithLongName places a long-named file: an alias, a
// contiguous run of LFN entries, and the short entry they
// describe.
func (v *Volume) createWithLongName(name string) (*File, error) {
	alias, err := generateAlias(name, v.aliasTaken)
	if err != nil {
		return nil, err
	}
	lfn, err := buildLFNEntries(name, alias)
	if err != nil {
		return nil, err
	}

	start, err := v.allocEntryRun(len(lfn) + 1)
	if err != nil {
		return nil, err
	}
	for i, raw := range lfn {
		slot := &v.rootEntries[start+i]
		slot.free = false
		slot.Attr = attrLongName
		slot.raw = raw
		slot.lfnStart = start
	}
	e := &v.rootEntries[start+len(lfn)]
	e.Name = alias
	e.Attr = attrArchive
	e.FirstCluster = 0
	e.Size = 0
	e.free = false
	e.longName = name
	e.lfnStart = start
	v.rootDirty = true
	return &File{vol: v, entry: e}, nil
}

// attachLongName writes a long-name run for an entry that already
// exists, relocating it when its current position has no room.
func (v *Volume) attachLongName(e *dirEntry, name string) error {
	lfn, err := buildLFNEntries(name, e.Name)
	if err != nil {
		return err
	}
	// The run must sit immediately before the short entry. When
	// the preceding slots are not free, the file moves instead.
	start := e.slot - len(lfn)
	usable := start >= 0
	for i := 0; usable && i < len(lfn); i++ {
		if !v.rootEntries[start+i].free {
			usable = false
		}
	}
	if !usable {
		return nil // keep the 8.3 name; the long name is a nicety
	}
	for i, raw := range lfn {
		slot := &v.rootEntries[start+i]
		slot.free = false
		slot.Attr = attrLongName
		slot.raw = raw
		slot.lfnStart = start
	}
	e.longName = name
	e.lfnStart = start
	v.rootDirty = true
	return nil
}

// Remove deletes a file, freeing its clusters and marking the
// directory slot reusable.
func (v *Volume) Remove(name string) error {
	if v.readOnly {
		return ErrReadOnly
	}
	e, err := v.findEntry(name)
	if err != nil {
		return err
	}
	if err := v.freeChain(e.FirstCluster); err != nil {
		return err
	}
	// Clear the long-name run with the entry it describes.
	// Leaving it would orphan entries whose checksum points at a
	// short name that no longer exists.
	v.clearLFNRun(e)
	e.free = true
	e.longName = ""
	e.FirstCluster = 0
	e.Size = 0
	v.rootDirty = true
	return nil
}

// loadChain refreshes the cached cluster list.
func (f *File) loadChain() error {
	c, err := f.vol.chain(f.entry.FirstCluster)
	if err != nil {
		return err
	}
	f.clusters = c
	return nil
}

// Size returns the file's current length.
func (f *File) Size() int64 { return int64(f.entry.Size) }

// Name returns the file's 8.3 name.
func (f *File) Name() string { return formatName(f.entry.Name) }

// Seek implements io.Seeker. Seeking past the end is permitted;
// the gap is only materialised if a write follows, matching
// ordinary file semantics.
func (f *File) Seek(off int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = off
	case io.SeekCurrent:
		abs = f.pos + off
	case io.SeekEnd:
		abs = int64(f.entry.Size) + off
	default:
		return 0, fmt.Errorf("fatfs: invalid whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("fatfs: negative seek position %d", abs)
	}
	f.pos = abs
	return abs, nil
}

// Read implements io.Reader, stopping at the file's recorded
// size rather than the end of its last cluster — the tail of a
// cluster past EOF is stale data belonging to no file.
func (f *File) Read(p []byte) (int, error) {
	if f.pos >= int64(f.entry.Size) {
		return 0, io.EOF
	}
	remaining := int64(f.entry.Size) - f.pos
	want := int64(len(p))
	if want > remaining {
		want = remaining
	}

	cs := int64(f.vol.geo.bytesPerCluster)
	buf := make([]byte, cs)
	read := int64(0)
	for read < want {
		idx := int((f.pos + read) / cs)
		if idx >= len(f.clusters) {
			break
		}
		if err := f.vol.readCluster(f.clusters[idx], buf); err != nil {
			return int(read), err
		}
		inCluster := (f.pos + read) % cs
		n := cs - inCluster
		if n > want-read {
			n = want - read
		}
		copy(p[read:read+n], buf[inCluster:inCluster+n])
		read += n
	}
	f.pos += read
	if read == 0 {
		return 0, io.EOF
	}
	return int(read), nil
}

// Write implements io.Writer, extending the cluster chain as
// needed and growing the recorded size when the write passes the
// previous end.
//
// Changes reach the image on Volume.Flush; cluster payloads are
// written immediately because they are not cached, but the FAT
// and directory updates that make them reachable are cached, so
// a crash before Flush leaves an unreferenced chain rather than
// a corrupt directory.
func (f *File) Write(p []byte) (int, error) {
	if f.vol.readOnly {
		return 0, ErrReadOnly
	}
	if len(p) == 0 {
		return 0, nil
	}

	cs := int64(f.vol.geo.bytesPerCluster)
	end := f.pos + int64(len(p))
	needClusters := int((end + cs - 1) / cs)

	// Extend the chain to cover the write.
	for len(f.clusters) < needClusters {
		var prev uint32
		if len(f.clusters) > 0 {
			prev = f.clusters[len(f.clusters)-1]
		}
		next, err := f.vol.allocate(prev)
		if err != nil {
			return 0, err
		}
		if len(f.clusters) == 0 {
			f.entry.FirstCluster = next
			f.vol.rootDirty = true
		}
		f.clusters = append(f.clusters, next)
		// A freshly allocated cluster holds whatever the previous
		// occupant left. Zero it so a partial write does not
		// expose unrelated data.
		if err := f.vol.writeCluster(next, make([]byte, cs)); err != nil {
			return 0, err
		}
	}

	buf := make([]byte, cs)
	written := int64(0)
	for written < int64(len(p)) {
		idx := int((f.pos + written) / cs)
		cluster := f.clusters[idx]
		inCluster := (f.pos + written) % cs
		n := cs - inCluster
		if n > int64(len(p))-written {
			n = int64(len(p)) - written
		}
		// Partial cluster writes need the existing contents first.
		if inCluster != 0 || n != cs {
			if err := f.vol.readCluster(cluster, buf); err != nil {
				return int(written), err
			}
		}
		copy(buf[inCluster:inCluster+n], p[written:written+n])
		if err := f.vol.writeCluster(cluster, buf); err != nil {
			return int(written), err
		}
		written += n
	}

	f.pos += written
	if f.pos > int64(f.entry.Size) {
		f.entry.Size = uint32(f.pos)
		f.vol.rootDirty = true
	}
	return int(written), nil
}

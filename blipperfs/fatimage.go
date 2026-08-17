package blipperfs

import (
	"fmt"
	"io"

	"github.com/ha1tch/blipper/fatfs"
)

// fatFileSet adapts a fatfs.Volume to the FileSet interface.
//
// The adapter lives here rather than in fatfs so the dependency
// points one way: fatfs is a general-purpose FAT driver that
// knows nothing about blipper, and this file is the only place
// the two meet. Anyone wanting fatfs for something unrelated
// takes the package without dragging blipper along.
type fatFileSet struct {
	vol *fatfs.Volume
}

// FATImage opens a FAT16 or FAT32 disk image read-only and
// returns it as a FileSet, so a whole xBase dataset held on a
// vintage image can be opened without extracting it first:
//
//	fs, err := blipperfs.FATImage(imageFile)
//	s, err := blipperfs.OpenFileSet(fs)
//	area, err := s.Select("CUSTOMERS")
//
// Read-only is the default deliberately. A disk image is often
// the only surviving copy of what it holds, and a cluster-
// allocation bug corrupts silently rather than failing loudly.
// Use FATImageRW when modification is actually intended.
func FATImage(img io.ReadSeeker, opts ...fatfs.Option) (FileSet, error) {
	vol, err := fatfs.OpenImage(img, opts...)
	if err != nil {
		return nil, fmt.Errorf("blipperfs: open FAT image: %w", err)
	}
	return &fatFileSet{vol: vol}, nil
}

// FATImageRW opens a FAT16 or FAT32 image for reading and
// writing. Changes are cached and reach the image on Flush.
func FATImageRW(img io.ReadWriteSeeker, opts ...fatfs.Option) (FileSet, error) {
	vol, err := fatfs.OpenImageRW(img, opts...)
	if err != nil {
		return nil, fmt.Errorf("blipperfs: open FAT image: %w", err)
	}
	return &fatFileSet{vol: vol}, nil
}

func (f *fatFileSet) Open(name string) (io.ReadWriteSeeker, error) {
	return f.vol.Open(name)
}

func (f *fatFileSet) Create(name string) (io.ReadWriteSeeker, error) {
	return f.vol.Create(name)
}

func (f *fatFileSet) Exists(name string) bool { return f.vol.Exists(name) }

func (f *fatFileSet) List() []string { return f.vol.List() }

// Flush commits cached FAT and directory changes to the image.
//
// This is the tablespace-level counterpart to Clipper's COMMIT:
// Clipper's flushes one work area's buffers, this flushes the
// container holding every table. A FileSet that owns real
// storage needs a commit point, and this is it.
func (f *fatFileSet) Flush() error { return f.vol.Flush() }

// Volume exposes the underlying fatfs volume, for callers needing
// FAT-specific detail (type, cluster size, free space) that the
// FileSet interface deliberately does not carry.
func (f *fatFileSet) Volume() *fatfs.Volume { return f.vol }

// Flusher is implemented by FileSets that buffer writes and need
// an explicit commit. OSDir does not implement it — the operating
// system's own filesystem provides that guarantee — while
// container-backed sets such as FAT images do.
//
// Callers holding a FileSet of unknown provenance should type-
// assert rather than assume:
//
//	if fl, ok := fs.(blipperfs.Flusher); ok {
//	    if err := fl.Flush(); err != nil { ... }
//	}
type Flusher interface {
	Flush() error
}

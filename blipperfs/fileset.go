package blipperfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileSet abstracts the storage holding a set of xBase files.
// Implementations decide what a "name" means: OSDir treats it as a
// filename within a directory, MemFileSet as a map key.
//
// Names are compared case-insensitively, matching DOS-era
// conventions where CUSTOMERS.DBF and customers.dbf are the same
// file. Implementations are responsible for that normalisation.
type FileSet interface {
	// Open returns a handle to an existing file, or an error if
	// it does not exist.
	Open(name string) (io.ReadWriteSeeker, error)

	// Create returns a handle to a new (or truncated) file.
	Create(name string) (io.ReadWriteSeeker, error)

	// Exists reports whether a file is present.
	Exists(name string) bool

	// List returns every name in the set, in unspecified order.
	List() []string
}

// --- OS-backed implementation ---

// osFileSet is a FileSet rooted at a directory on disk.
type osFileSet struct {
	root string
}

// OSDir returns a FileSet rooted at the given directory. The
// directory must exist; files within it are opened lazily.
//
// Long filenames need no option here: the host filesystem
// decides what names are legal, and every modern one takes them
// natively. Only FAT has the 8.3 restriction that
// fatfs.WithLongNames exists to lift.
//
// This constructor performs no I/O beyond what the caller's own
// filesystem does on path resolution — use OpenDir when you want
// a whole directory opened as a session.
func OSDir(path string) FileSet {
	return &osFileSet{root: path}
}

// resolve finds the on-disk name matching the requested name
// case-insensitively, returning the requested name unchanged when
// no case variant exists (so Create writes the caller's spelling).
func (f *osFileSet) resolve(name string) string {
	direct := filepath.Join(f.root, name)
	if _, err := os.Stat(direct); err == nil {
		return direct
	}
	entries, err := os.ReadDir(f.root)
	if err != nil {
		return direct
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return filepath.Join(f.root, e.Name())
		}
	}
	return direct
}

func (f *osFileSet) Open(name string) (io.ReadWriteSeeker, error) {
	h, err := os.OpenFile(f.resolve(name), os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	// Wrapped so the handle satisfies blipperdb.Locker: an area
	// over a real file can coordinate with other processes, which
	// is the only place locking can mean anything.
	return &lockableFile{File: h}, nil
}

func (f *osFileSet) Create(name string) (io.ReadWriteSeeker, error) {
	h, err := os.Create(filepath.Join(f.root, name))
	if err != nil {
		return nil, err
	}
	return &lockableFile{File: h}, nil
}

func (f *osFileSet) Exists(name string) bool {
	_, err := os.Stat(f.resolve(name))
	return err == nil
}

func (f *osFileSet) List() []string {
	entries, err := os.ReadDir(f.root)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// --- in-memory implementation ---

// MemFileSet is an in-memory FileSet, primarily for tests. It lets
// the whole resolution path run without touching disk.
type MemFileSet struct {
	files map[string]*memFile
}

// NewMemFileSet returns an empty in-memory FileSet.
func NewMemFileSet() *MemFileSet {
	return &MemFileSet{files: map[string]*memFile{}}
}

// key normalises a name for case-insensitive lookup.
func memKey(name string) string { return strings.ToUpper(name) }

func (m *MemFileSet) Open(name string) (io.ReadWriteSeeker, error) {
	f, ok := m.files[memKey(name)]
	if !ok {
		return nil, fmt.Errorf("blipperfs: %s not found", name)
	}
	f.pos = 0
	return f, nil
}

func (m *MemFileSet) Create(name string) (io.ReadWriteSeeker, error) {
	f := &memFile{name: name}
	m.files[memKey(name)] = f
	return f, nil
}

func (m *MemFileSet) Exists(name string) bool {
	_, ok := m.files[memKey(name)]
	return ok
}

func (m *MemFileSet) List() []string {
	out := make([]string, 0, len(m.files))
	for _, f := range m.files {
		out = append(out, f.name)
	}
	sort.Strings(out)
	return out
}

// Bytes returns the raw content of a named file, for tests that
// want to inspect what was written.
func (m *MemFileSet) Bytes(name string) ([]byte, bool) {
	f, ok := m.files[memKey(name)]
	if !ok {
		return nil, false
	}
	return f.data, true
}

// memFile is an in-memory io.ReadWriteSeeker.
type memFile struct {
	name string
	data []byte
	pos  int64
}

func (m *memFile) Read(p []byte) (int, error) {
	if m.pos >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += int64(n)
	return n, nil
}

func (m *memFile) Write(p []byte) (int, error) {
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

func (m *memFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		m.pos = off
	case io.SeekCurrent:
		m.pos += off
	case io.SeekEnd:
		m.pos = int64(len(m.data)) + off
	default:
		return 0, fmt.Errorf("blipperfs: bad whence %d", whence)
	}
	if m.pos < 0 {
		return 0, fmt.Errorf("blipperfs: negative position")
	}
	return m.pos, nil
}

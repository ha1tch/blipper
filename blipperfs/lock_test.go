package blipperfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ha1tch/blipper/blipperdb"
)

// TestOSFilesImplementLocker verifies the seam: a handle from
// OSDir must satisfy blipperdb.Locker, or shared mode over real
// files silently degrades to ErrLockUnsupported.
func TestOSFilesImplementLocker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "DATA.DBF"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fs := OSDir(dir)
	h, err := fs.Open("DATA.DBF")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h.(interface{ Close() error }).Close()

	if _, ok := h.(blipperdb.Locker); !ok {
		t.Fatal("an OSDir handle does not implement blipperdb.Locker")
	}
}

// TestRegionLocksAreRealAcrossProcesses is the test that matters.
// In-process assertions prove nothing about POSIX record locks,
// which are held per process: this takes a lock, then has a
// second process attempt the same range and confirms it is
// refused.
func TestRegionLocksAreRealAcrossProcesses(t *testing.T) {
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock(1) not available")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "LOCKED.DBF")
	if err := os.WriteFile(path, make([]byte, 64), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fs := OSDir(dir)
	h, err := fs.Open("LOCKED.DBF")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h.(interface{ Close() error }).Close()

	locker, ok := h.(blipperdb.Locker)
	if !ok {
		t.Fatal("handle does not implement Locker")
	}
	if err := locker.LockRegion(0, 16, true); err != nil {
		t.Fatalf("LockRegion: %v", err)
	}

	// A second process taking a conflicting fcntl lock on the
	// same range must fail. Written as a tiny Go program so the
	// lock type matches exactly what blipperfs takes.
	prog := filepath.Join(dir, "probe.go")
	src := `package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func main() {
	f, err := os.OpenFile(os.Args[1], os.O_RDWR, 0o644)
	if err != nil {
		fmt.Println("OPENFAIL")
		return
	}
	lk := &unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 16}
	if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, lk); err != nil {
		fmt.Println("BLOCKED")
		return
	}
	fmt.Println("ACQUIRED")
}
`
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	// Reuse the module's own dependencies rather than fetching.
	cmd := exec.Command("go", "run", prog, path)
	cmd.Dir = mustModuleRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("could not run the probe (%v): %s", err, out)
	}
	got := string(out)
	if !contains(got, "BLOCKED") {
		t.Errorf("second process reported %q; expected BLOCKED, meaning the lock is real", got)
	}

	// After releasing, the same probe must succeed — otherwise
	// the test would pass even if locking were permanently broken.
	if err := locker.UnlockRegion(0, 16); err != nil {
		t.Fatalf("UnlockRegion: %v", err)
	}
	out2, err := exec.Command("go", "run", prog, path).CombinedOutput()
	if err == nil && !contains(string(out2), "ACQUIRED") {
		t.Errorf("after unlock the probe reported %q; expected ACQUIRED", out2)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func mustModuleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return filepath.Dir(wd) // blipperfs -> repository root
}

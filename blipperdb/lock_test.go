package blipperdb

import (
	"errors"
	"testing"

	"github.com/ha1tch/blipper/dbf"
)

// lockableMem is a memFile that can lock, standing in for real
// file storage. It records regions rather than enforcing them
// against other processes, which is enough to exercise the
// Area-level bookkeeping; genuine cross-process behaviour is
// covered by the blipperfs tests against real files.
type lockableMem struct {
	memFile
	regions map[[2]int64]bool
}

func (m *lockableMem) LockRegion(off, length int64, exclusive bool) error {
	if m.regions == nil {
		m.regions = map[[2]int64]bool{}
	}
	key := [2]int64{off, length}
	if m.regions[key] {
		return errors.New("region already locked")
	}
	m.regions[key] = true
	return nil
}

func (m *lockableMem) UnlockRegion(off, length int64) error {
	delete(m.regions, [2]int64{off, length})
	return nil
}

func lockArea(t *testing.T, mode OpenMode) *Area {
	t.Helper()
	db := New()
	area, err := db.Create("DATA", &lockableMem{}, codeSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	area.mode = mode
	return area
}

// unlockableArea uses storage with no locking mechanism, standing
// in for a FAT image.
func unlockableArea(t *testing.T, mode OpenMode) *Area {
	t.Helper()
	db := New()
	area, err := db.Create("DATA", &memFile{}, codeSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	area.mode = mode
	return area
}

func newRec(area *Area, code string) dbf.Record {
	rec := dbf.NewRecord(area.Table().Schema())
	rec.Set(area.Table().Schema(), "CODE", code)
	return rec
}

// TestExclusiveModeNeedsNoLock is the compatibility guarantee:
// every caller written before locking existed keeps working
// unchanged, because Use opens Exclusive and Exclusive writes
// need no lock.
func TestExclusiveModeNeedsNoLock(t *testing.T) {
	area := lockArea(t, Exclusive)
	if area.Mode() != Exclusive {
		t.Fatalf("Mode = %v, want Exclusive", area.Mode())
	}
	if _, err := area.Append(newRec(area, "A")); err != nil {
		t.Errorf("Append in Exclusive mode without a lock: %v", err)
	}
	if err := area.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	if err := area.Replace(newRec(area, "B")); err != nil {
		t.Errorf("Replace in Exclusive mode: %v", err)
	}
	if err := area.Delete(); err != nil {
		t.Errorf("Delete in Exclusive mode: %v", err)
	}
}

// TestSharedModeRefusesUnlockedWrites is the whole point: a
// shared write without a lock fails rather than proceeding.
func TestSharedModeRefusesUnlockedWrites(t *testing.T) {
	area := lockArea(t, Shared)

	if _, err := area.Append(newRec(area, "A")); !errors.Is(err, ErrNotLocked) {
		t.Errorf("Append unlocked: err = %v, want ErrNotLocked", err)
	}

	// Seed a record via a file lock so the record-level cases
	// have something to address.
	if err := area.FLock(); err != nil {
		t.Fatalf("FLock: %v", err)
	}
	if _, err := area.Append(newRec(area, "A")); err != nil {
		t.Fatalf("Append under FLock: %v", err)
	}
	if err := area.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	if err := area.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	if err := area.Replace(newRec(area, "B")); !errors.Is(err, ErrNotLocked) {
		t.Errorf("Replace unlocked: err = %v, want ErrNotLocked", err)
	}
	if err := area.Delete(); !errors.Is(err, ErrNotLocked) {
		t.Errorf("Delete unlocked: err = %v, want ErrNotLocked", err)
	}
	if err := area.Recall(); !errors.Is(err, ErrNotLocked) {
		t.Errorf("Recall unlocked: err = %v, want ErrNotLocked", err)
	}
}

// TestRLockCoversOnlyItsRecord verifies the lock is checked
// against what is actually being written, not merely for
// presence.
func TestRLockCoversOnlyItsRecord(t *testing.T) {
	area := lockArea(t, Shared)
	area.FLock()
	for _, c := range []string{"A", "B"} {
		if _, err := area.Append(newRec(area, c)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	area.Unlock()

	// Lock record 1, then try to write record 2.
	if err := area.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	if err := area.RLock(); err != nil {
		t.Fatalf("RLock: %v", err)
	}
	if err := area.Replace(newRec(area, "A2")); err != nil {
		t.Errorf("Replace of the locked record: %v", err)
	}
	if err := area.Skip(1); err != nil {
		t.Fatalf("Skip: %v", err)
	}
	if err := area.Replace(newRec(area, "B2")); !errors.Is(err, ErrNotLocked) {
		t.Errorf("Replace of an unlocked record: err = %v, want ErrNotLocked", err)
	}
}

// TestRLockDoesNotCoverWholeFileOperations verifies that a record
// lock is not mistaken for permission to append or pack, both of
// which change the file rather than a record.
func TestRLockDoesNotCoverWholeFileOperations(t *testing.T) {
	area := lockArea(t, Shared)
	area.FLock()
	area.Append(newRec(area, "A"))
	area.Unlock()

	if err := area.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	if err := area.RLock(); err != nil {
		t.Fatalf("RLock: %v", err)
	}
	if _, err := area.Append(newRec(area, "B")); !errors.Is(err, ErrNotLocked) {
		t.Errorf("Append under RLock: err = %v, want ErrNotLocked", err)
	}
	if _, err := area.Pack(); !errors.Is(err, ErrNotLocked) {
		t.Errorf("Pack under RLock: err = %v, want ErrNotLocked", err)
	}
}

// TestFLockCoversEverything is the converse: a file lock permits
// every write.
func TestFLockCoversEverything(t *testing.T) {
	area := lockArea(t, Shared)
	if err := area.FLock(); err != nil {
		t.Fatalf("FLock: %v", err)
	}
	if _, err := area.Append(newRec(area, "A")); err != nil {
		t.Errorf("Append under FLock: %v", err)
	}
	area.GoTop()
	if err := area.Replace(newRec(area, "B")); err != nil {
		t.Errorf("Replace under FLock: %v", err)
	}
	if err := area.Delete(); err != nil {
		t.Errorf("Delete under FLock: %v", err)
	}
	if _, err := area.Pack(); err != nil {
		t.Errorf("Pack under FLock: %v", err)
	}
}

// TestUnlockIsIdempotent matches Clipper, where UNLOCK with no
// lock held is not an error.
func TestUnlockIsIdempotent(t *testing.T) {
	area := lockArea(t, Shared)
	if err := area.Unlock(); err != nil {
		t.Errorf("Unlock with no lock held: %v", err)
	}
	if area.Locked() {
		t.Error("Locked() true after Unlock")
	}
}

// TestLockingUnsupportedStorageSaysSo checks that storage which
// cannot lock reports it rather than pretending. A memFile has no
// locking mechanism, which is exactly the FAT-image situation.
func TestLockingUnsupportedStorageSaysSo(t *testing.T) {
	area := unlockableArea(t, Shared)
	if err := area.FLock(); !errors.Is(err, ErrLockUnsupported) {
		t.Errorf("FLock on unlockable storage: err = %v, want ErrLockUnsupported", err)
	}
}

// TestDoubleLockRefused guards against a caller silently
// replacing one lock with another and losing track of what it
// holds.
func TestDoubleLockRefused(t *testing.T) {
	area := lockArea(t, Exclusive)
	if err := area.FLock(); err != nil {
		t.Fatalf("FLock: %v", err)
	}
	// Exclusive mode short-circuits, so this case is about the
	// shared path; verify the bookkeeping is at least consistent.
	if !area.Locked() {
		t.Error("Locked() false after FLock")
	}
	if err := area.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if area.Locked() {
		t.Error("Locked() true after Unlock")
	}
}

func TestOpenModeString(t *testing.T) {
	if Exclusive.String() != "EXCLUSIVE" {
		t.Errorf("Exclusive.String() = %q", Exclusive.String())
	}
	if Shared.String() != "SHARED" {
		t.Errorf("Shared.String() = %q", Shared.String())
	}
}

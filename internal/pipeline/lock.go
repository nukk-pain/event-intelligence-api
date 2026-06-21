package pipeline

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrLocked is returned by AcquireLock when another ingest run already holds the
// single-flight lock. Callers (cron, cmd) treat this as a clean skip: log and
// exit 0 so overlapping runs do not pile up.
var ErrLocked = errors.New("pipeline: ingest already running (lock held)")

// FileLock is an advisory single-flight lock backed by syscall.Flock on a lock
// file. The lock is released when Unlock is called or the process exits (the OS
// drops the flock on fd close / process death), so a crashed run never leaves a
// permanently stuck lock — no stale-pid scavenging needed.
type FileLock struct {
	path string
	f    *os.File
}

// AcquireLock opens (creating if needed) the lock file at path and takes a
// non-blocking exclusive flock. If another process holds it, it returns
// ErrLocked WITHOUT blocking, so overlapping cron runs are skipped cleanly.
func AcquireLock(path string) (*FileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("pipeline: open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("pipeline: flock %s: %w", path, err)
	}
	// Record our pid for operator visibility (best-effort; not used for liveness).
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0)
	return &FileLock{path: path, f: f}, nil
}

// Unlock releases the flock and closes the file. Safe to call once; the OS would
// also release on process exit.
func (l *FileLock) Unlock() error {
	if l == nil || l.f == nil {
		return nil
	}
	ferr := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	cerr := l.f.Close()
	l.f = nil
	if ferr != nil {
		return ferr
	}
	return cerr
}

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var errDailyBudget = errors.New("daily provider budget exhausted")

// dailyBudget is an append-only reservation journal, separate from event data.
// The exclusive process lock prevents concurrent owners. Sync precedes every
// provider request; partial writes, corrupt state and failed sync fail closed.
// Reservations are never refunded, including provider failures and cancellation.
type dailyBudget struct {
	mu           sync.Mutex
	file, lock   *os.File
	day          string
	count, limit int
	failed       bool
	now          func() time.Time
}

type reservation struct {
	TS      string `json:"ts"`
	Event   string `json:"event"`
	Actor   string `json:"actor"`
	Payload struct {
		Day   string `json:"utc_day"`
		Count int    `json:"count"`
	} `json:"payload"`
}

func openDailyBudget(dir string, limit int) (*dailyBudget, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("quota directory must be absolute")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, errors.New("quota directory must exist with mode 0700")
	}
	lock, err := os.OpenFile(filepath.Join(dir, "quota.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("quota state already locked")
	}
	b := &dailyBudget{lock: lock, limit: limit, now: time.Now}
	b.file, err = os.OpenFile(filepath.Join(dir, "reservations.jsonl"), os.O_CREATE|os.O_RDWR|os.O_APPEND|unix.O_NOFOLLOW, 0600)
	if err != nil {
		b.Close()
		return nil, err
	}
	for _, f := range []*os.File{b.lock, b.file} {
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			b.Close()
			return nil, errors.New("quota files must be regular and private")
		}
	}
	// Persist directory entries too, so first-start reservations cannot disappear
	// after a crash while the journal's own fsync appeared to have succeeded.
	d, err := os.Open(dir)
	if err == nil {
		err = d.Sync()
		d.Close()
	}
	if err != nil {
		b.Close()
		return nil, err
	}
	scanner := bufio.NewScanner(b.file)
	for scanner.Scan() {
		var r reservation
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil || !validReservation(r, b.day, b.count) {
			b.Close()
			return nil, errors.New("quota reservation journal is corrupt")
		}
		b.day, b.count = r.Payload.Day, r.Payload.Count
	}
	if err := scanner.Err(); err != nil {
		b.Close()
		return nil, err
	}
	// An unterminated record could have been only partially committed. Reject it
	// rather than silently skipping it or appending a second JSON value to it.
	info, err = b.file.Stat()
	if err != nil {
		b.Close()
		return nil, err
	}
	if info.Size() > 0 {
		var last [1]byte
		if _, err := b.file.ReadAt(last[:], info.Size()-1); err != nil || last[0] != '\n' {
			b.Close()
			return nil, errors.New("quota journal has incomplete record")
		}
	}
	return b, nil
}

func validReservation(r reservation, previousDay string, count int) bool {
	ts, err := time.Parse(time.RFC3339Nano, r.TS)
	if err != nil || ts.UTC().Format("2006-01-02") != r.Payload.Day || r.Event != "provider_reserved" || r.Actor != "eventmcp" || r.Payload.Count < 1 {
		return false
	}
	if r.Payload.Day < previousDay {
		return false
	}
	if r.Payload.Day == previousDay {
		return r.Payload.Count == count+1
	}
	return r.Payload.Count == 1
}

func (b *dailyBudget) reserve() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failed {
		return errors.New("quota storage unavailable")
	}
	now := b.now().UTC()
	day := now.Format("2006-01-02")
	if day < b.day {
		return errors.New("quota clock moved backwards")
	}
	count := b.count
	if day > b.day {
		count = 0
	}
	if count >= b.limit {
		return errDailyBudget
	}
	r := reservation{TS: now.Format(time.RFC3339Nano), Event: "provider_reserved", Actor: "eventmcp"}
	r.Payload.Day, r.Payload.Count = day, count+1
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	n, err := b.file.Write(line)
	if err == nil && n != len(line) {
		err = errors.New("short quota write")
	}
	if err == nil {
		err = b.file.Sync()
	}
	if err != nil {
		b.failed = true
		return fmt.Errorf("persist quota reservation: %w", err)
	}
	b.day, b.count = day, count+1
	return nil
}

func (b *dailyBudget) Close() {
	if b.file != nil {
		b.file.Close()
	}
	if b.lock != nil {
		b.lock.Close()
	}
}

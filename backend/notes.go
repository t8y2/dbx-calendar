package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrNoDataDir is returned when the sidecar runs without a host-provided
// DBX_PLUGIN_DATA_DIR, so notes cannot be persisted. Callers must surface this
// instead of pretending the write succeeded.
var ErrNoDataDir = errors.New("plugin data directory is not set; notes cannot be persisted")

const notesSchemaVersion = 1

// notesFile is the on-disk shape. version exists so the format can evolve
// without guessing whether an old file is still readable.
type notesFile struct {
	Version int               `json:"version"`
	Notes   map[string]string `json:"notes"`
}

// notesStore owns notes for one data directory. Each connection gets its own
// handle, so a slow write on one connection never blocks another.
type notesStore struct {
	dataDir string
}

func newNotesStore(dataDir string) *notesStore {
	return &notesStore{dataDir: dataDir}
}

// Persistence reports whether notes can survive a process restart.
func (store *notesStore) Persistence() bool {
	return store.dataDir != ""
}

// connNotes holds one connection's notes. memory guards the map only; disk
// guards file IO only. The write path takes memory then disk, never the other
// way round, so the two locks cannot deadlock.
type connNotes struct {
	memory  sync.RWMutex
	disk    sync.Mutex
	path    string
	noteMap map[string]string
	loaded  bool
}

// Open returns the per-connection handle. It does not touch the disk: the file
// is read on first use, and a missing file is not an error. Without a data
// directory the handle is still usable in memory -- Persistence reports that,
// and Set refuses rather than pretending the write was durable.
func (store *notesStore) Open(connectionID string) (*connNotes, error) {
	path, err := notesPath(store.dataDir, connectionID)
	if err != nil {
		// An unsafe id is a caller bug and must always surface. A missing data
		// directory is not: the store advertises that through Persistence.
		if errors.Is(err, ErrNoDataDir) {
			return &connNotes{noteMap: map[string]string{}}, nil
		}
		return nil, err
	}
	return &connNotes{path: path, noteMap: map[string]string{}}, nil
}

func notesPath(dataDir, connectionID string) (string, error) {
	if dataDir == "" {
		return "", ErrNoDataDir
	}
	if connectionID == "" {
		return "", errors.New("connection id is required")
	}
	// The id reaches us from the host and the UI; never let it escape the
	// notes directory. filepath.Base alone would let ".." through, and a
	// backslash is a separator on Windows but not to filepath on other GOOSes.
	if connectionID != filepath.Base(connectionID) || strings.ContainsAny(connectionID, `/\`) || connectionID == "." || connectionID == ".." {
		return "", fmt.Errorf("unsafe connection id: %q", connectionID)
	}
	return filepath.Join(dataDir, "notes", connectionID+".json"), nil
}

// ensure returns the notes map, reading the file on first use. A corrupt file
// is quarantined rather than deleted, because notes are not reproducible.
func (handle *connNotes) ensure() (map[string]string, error) {
	handle.memory.RLock()
	if handle.loaded {
		notes := handle.noteMap
		handle.memory.RUnlock()
		return notes, nil
	}
	handle.memory.RUnlock()

	handle.memory.Lock()
	defer handle.memory.Unlock()
	if handle.loaded {
		return handle.noteMap, nil
	}

	data, err := os.ReadFile(handle.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		handle.noteMap, handle.loaded = map[string]string{}, true
		return handle.noteMap, nil
	case err != nil:
		return nil, err
	}

	var parsed notesFile
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Notes == nil {
		quarantine := handle.path + ".corrupt-" + time.Now().UTC().Format("20060102T150405")
		_ = os.Rename(handle.path, quarantine)
		handle.noteMap, handle.loaded = map[string]string{}, true
		return handle.noteMap, nil
	}
	handle.noteMap, handle.loaded = parsed.Notes, true
	return handle.noteMap, nil
}

// Notes returns a copy, so callers cannot mutate the store's map.
func (handle *connNotes) Notes() (map[string]string, error) {
	if _, err := handle.ensure(); err != nil {
		return nil, err
	}
	handle.memory.RLock()
	defer handle.memory.RUnlock()
	copied := make(map[string]string, len(handle.noteMap))
	for key, value := range handle.noteMap {
		copied[key] = value
	}
	return copied, nil
}

// Note returns one day's note and whether it exists.
func (handle *connNotes) Note(dateKey string) (string, bool, error) {
	notes, err := handle.ensure()
	if err != nil {
		return "", false, err
	}
	handle.memory.RLock()
	defer handle.memory.RUnlock()
	value, ok := notes[dateKey]
	return value, ok, nil
}

// Set writes or clears one day's note, then flushes to disk. An empty value
// deletes the key, matching the UI's "saving an empty editor removes the note".
func (handle *connNotes) Set(dateKey, value string) error {
	if err := validDateKey(dateKey); err != nil {
		return err
	}
	if !handle.persistenceAvailable() {
		return ErrNoDataDir
	}
	if _, err := handle.ensure(); err != nil {
		return err
	}

	// Hold memory across mutate+snapshot so two concurrent Sets cannot
	// interleave into a lost update.
	handle.memory.Lock()
	if value == "" {
		delete(handle.noteMap, dateKey)
	} else {
		handle.noteMap[dateKey] = value
	}
	snapshot := handle.snapshotLocked()
	handle.memory.Unlock()

	return handle.flush(snapshot)
}

// Replace swaps the whole note set at once, for bulk import.
func (handle *connNotes) Replace(incoming map[string]string) error {
	if !handle.persistenceAvailable() {
		return ErrNoDataDir
	}
	cleaned := make(map[string]string, len(incoming))
	for key, value := range incoming {
		if value == "" {
			continue
		}
		if err := validDateKey(key); err != nil {
			return err
		}
		cleaned[key] = value
	}

	handle.memory.Lock()
	handle.noteMap = cleaned
	snapshot := handle.snapshotLocked()
	handle.loaded = true
	handle.memory.Unlock()

	return handle.flush(snapshot)
}

func (handle *connNotes) snapshotLocked() notesFile {
	notes := make(map[string]string, len(handle.noteMap))
	for key, value := range handle.noteMap {
		notes[key] = value
	}
	return notesFile{Version: notesSchemaVersion, Notes: notes}
}

func (handle *connNotes) persistenceAvailable() bool {
	return handle.path != ""
}

// flush writes atomically: a temp file in the same directory, then a rename.
// rename over an existing file is atomic, but on Windows it fails while the
// target is open, so it retries briefly.
func (handle *connNotes) flush(payload notesFile) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	handle.disk.Lock()
	defer handle.disk.Unlock()

	if err := os.MkdirAll(filepath.Dir(handle.path), 0o700); err != nil {
		return err
	}
	tmp := handle.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if lastErr = os.Rename(tmp, handle.path); lastErr == nil {
			return nil
		}
		time.Sleep(time.Duration(20*(attempt+1)) * time.Millisecond)
	}
	_ = os.Remove(tmp)
	return lastErr
}

// validDateKey keeps the map keys to the canonical YYYY-MM-DD the UI sends.
func validDateKey(dateKey string) error {
	if len(dateKey) != 10 || dateKey[4] != '-' || dateKey[7] != '-' {
		return fmt.Errorf("invalid date key: %q", dateKey)
	}
	if _, err := time.Parse("2006-01-02", dateKey); err != nil {
		return fmt.Errorf("invalid date key: %q", dateKey)
	}
	return nil
}

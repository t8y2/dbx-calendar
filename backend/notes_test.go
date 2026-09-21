package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	dbxpluginsdk "github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
)

// The store must keep working without a host-provided data directory, but it
// must refuse to pretend a note was persisted.
func TestNotesSetWithoutDataDirFails(t *testing.T) {
	t.Setenv(dataDirEnvVar, "")
	store := newNotesStore("")
	notes, err := store.Open("conn-1")
	if err != nil {
		t.Fatal(err)
	}
	if store.Persistence() {
		t.Fatal("expected persistence to be reported as unavailable")
	}
	if err := notes.Set("2026-02-14", "dinner"); !errors.Is(err, ErrNoDataDir) {
		t.Fatalf("expected ErrNoDataDir, got %v", err)
	}
	// Reading still works in memory-only mode.
	if _, ok, err := notes.Note("2026-02-14"); err != nil || ok {
		t.Fatalf("unexpected read result: ok=%v err=%v", ok, err)
	}
}

func TestNotesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := newNotesStore(dir)
	notes, err := store.Open("conn-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := notes.Set("2026-02-14", "dinner"); err != nil {
		t.Fatal(err)
	}
	if err := notes.Set("2026-02-15", "lunch"); err != nil {
		t.Fatal(err)
	}

	// A second handle reads what the first one wrote: this is the restart path.
	reopened, err := store.Open("conn-1")
	if err != nil {
		t.Fatal(err)
	}
	value, ok, err := reopened.Note("2026-02-14")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || value != "dinner" {
		t.Fatalf("unexpected note after reload: ok=%v value=%q", ok, value)
	}

	// Empty value clears the note, matching the UI's editor behaviour.
	if err := reopened.Set("2026-02-14", ""); err != nil {
		t.Fatal(err)
	}
	all, err := reopened.Notes()
	if err != nil {
		t.Fatal(err)
	}
	if _, present := all["2026-02-14"]; present {
		t.Fatalf("expected the cleared note to be gone: %#v", all)
	}
	if all["2026-02-15"] != "lunch" {
		t.Fatalf("unexpected notes: %#v", all)
	}
}

func TestNotesFileLayout(t *testing.T) {
	dir := t.TempDir()
	store := newNotesStore(dir)
	notes, err := store.Open("562a4465-7306-4392-869e-1eeef3da6091")
	if err != nil {
		t.Fatal(err)
	}
	if err := notes.Set("2026-02-14", "dinner"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "notes", "562a4465-7306-4392-869e-1eeef3da6091.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected notes at %s: %v", path, err)
	}
	var parsed notesFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Version != notesSchemaVersion || parsed.Notes["2026-02-14"] != "dinner" {
		t.Fatalf("unexpected on-disk shape: %#v", parsed)
	}
}

func TestNotesRejectsUnsafeKeys(t *testing.T) {
	store := newNotesStore(t.TempDir())
	if _, err := store.Open(`..\..\escape`); err == nil {
		t.Fatal("expected a traversal attempt in the connection id to be rejected")
	}
	notes, err := store.Open("conn-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := notes.Set("../../etc/passwd", "x"); err == nil {
		t.Fatal("expected a malformed date key to be rejected")
	}
	if err := notes.Set("2026-2-14", "x"); err == nil {
		t.Fatal("expected a non-canonical date key to be rejected")
	}
}

// Concurrent saves to one connection must not lose an update.
func TestNotesConcurrentSet(t *testing.T) {
	store := newNotesStore(t.TempDir())
	notes, err := store.Open("conn-1")
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for day := 1; day <= 20; day++ {
		group.Add(1)
		go func(day int) {
			defer group.Done()
			dateKey := "2026-02-" + string(rune('0'+day/10)) + string(rune('0'+day%10))
			if err := notes.Set(dateKey, "note"); err != nil {
				t.Error(err)
			}
		}(day)
	}
	group.Wait()

	all, err := notes.Notes()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 20 {
		t.Fatalf("expected 20 notes, got %d: %#v", len(all), all)
	}
}

// A corrupt file must be quarantined instead of silently emptied.
func TestNotesQuarantinesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	store := newNotesStore(dir)
	notes, err := store.Open("conn-1")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "notes", "conn-1.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := notes.Notes(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected the corrupt file to be quarantined, found %v", matches)
	}
	if err := notes.Set("2026-02-14", "dinner"); err != nil {
		t.Fatal(err)
	}
	if value, _, err := notes.Note("2026-02-14"); err != nil || value != "dinner" {
		t.Fatalf("expected the store to recover after quarantine: %q %v", value, err)
	}
}

// The RPC surface must report a save failure instead of a fake success.
func TestNotesMethodsFailWithoutDataDir(t *testing.T) {
	instance := &plugin{
		connections: map[string]*connNotes{},
		notes:       newNotesStore(""),
	}
	params, _ := json.Marshal(map[string]any{"connection": map[string]any{"id": "conn-1"}, "date": "2026-02-14", "value": "dinner"})
	result, pluginError := instance.Handle(dbxpluginsdk.RequestContext{}, "dbx-calendar/notes/set", params, nil)
	if pluginError == nil {
		t.Fatalf("expected a plugin error, got %#v", result)
	}
	if pluginError.Code != -32002 {
		t.Fatalf("unexpected error code: %#v", pluginError)
	}
}

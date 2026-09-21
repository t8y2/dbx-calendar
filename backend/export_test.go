package main

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sidecar fallback must be able to write what the UI could not download.
func TestExportWritesFile(t *testing.T) {
	dir := t.TempDir()
	store := newExportStore(dir)
	payload := []byte("date\tnote\n2026-02-14\tdinner\n")

	written, err := store.Write("calendar-notes-2026-02-14.txt", base64.StdEncoding.EncodeToString(payload))
	if err != nil {
		t.Fatal(err)
	}
	if written.Size != int64(len(payload)) {
		t.Fatalf("unexpected size: %d", written.Size)
	}
	if !strings.HasPrefix(written.Path, filepath.Join(dir, "exports")) {
		t.Fatalf("expected the file under the exports folder: %s", written.Path)
	}
	// A timestamp suffix keeps two exports of the same day from colliding.
	if !strings.Contains(filepath.Base(written.Path), "calendar-notes-2026-02-14-") {
		t.Fatalf("expected a timestamped name: %s", filepath.Base(written.Path))
	}

	stored, err := os.ReadFile(written.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(payload) {
		t.Fatalf("stored bytes differ: %q", stored)
	}
}

// Without a data directory the export must fail loudly, exactly like a note
// write, rather than reporting a file the user cannot find.
func TestExportWithoutDataDirFails(t *testing.T) {
	_, err := newExportStore("").Write("a.txt", base64.StdEncoding.EncodeToString([]byte("x")))
	if !errors.Is(err, ErrNoDataDir) {
		t.Fatalf("expected ErrNoDataDir, got %v", err)
	}
}

func TestExportRejectsUnsafeName(t *testing.T) {
	store := newExportStore(t.TempDir())
	encoded := base64.StdEncoding.EncodeToString([]byte("x"))
	for _, name := range []string{`..\..\escape.txt`, "../escape.txt", "sub/dir.txt", `sub\dir.txt`, ".", "..", ""} {
		if _, err := store.Write(name, encoded); err == nil {
			t.Fatalf("expected the name %q to be rejected", name)
		}
	}
}

func TestExportRejectsBadPayload(t *testing.T) {
	store := newExportStore(t.TempDir())
	if _, err := store.Write("a.txt", "not base64!!"); err == nil {
		t.Fatal("expected invalid base64 to be rejected")
	}
	// A payload over the bridge budget is refused before decoding.
	oversized := strings.Repeat("A", maxExportBytes/3*4+16)
	if _, err := store.Write("a.txt", oversized); !errors.Is(err, ErrExportTooLarge) {
		t.Fatalf("expected ErrExportTooLarge, got %v", err)
	}
}

// The RPC surface reports a size refusal as a parameter error and a missing data
// directory as the persistence error, so the UI can tell them apart.
func TestProtocolExportWrite(t *testing.T) {
	dir := t.TempDir()
	instance := &plugin{
		connections: map[string]*connNotes{},
		notes:       newNotesStore(dir),
		exports:     newExportStore(dir),
	}
	payload := base64.StdEncoding.EncodeToString([]byte("hello"))

	responses := serve(t, instance, requestLine(1, "dbx-calendar/export/write", map[string]any{
		"name": "notes.txt",
		"data": payload,
	}))
	result := responses[1]["result"].(map[string]any)
	if result["success"] != true {
		t.Fatalf("unexpected export result: %#v", result)
	}
	path, _ := result["path"].(string)
	if path == "" {
		t.Fatalf("expected a path in the response: %#v", result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected the reported file to exist: %v", err)
	}

	// A missing name is a parameter error.
	responses = serve(t, instance, requestLine(1, "dbx-calendar/export/write", map[string]any{"data": payload}))
	if responses[1]["error"] == nil {
		t.Fatalf("expected a parameter error: %#v", responses[1])
	}

	// No data directory is the persistence error the UI already understands.
	helpless := &plugin{
		connections: map[string]*connNotes{},
		notes:       newNotesStore(""),
		exports:     newExportStore(""),
	}
	responses = serve(t, helpless, requestLine(1, "dbx-calendar/export/write", map[string]any{
		"name": "notes.txt",
		"data": payload,
	}))
	failure := responses[1]["error"].(map[string]any)
	if failure["code"] != float64(-32002) {
		t.Fatalf("expected the persistence error code, got %#v", failure)
	}
}

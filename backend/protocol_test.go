package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbxpluginsdk "github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
)

// requestLine builds one JSON Lines protocol request, the transport the host
// uses by default.
func requestLine(id int, method string, params map[string]any) string {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	return string(payload) + "\n"
}

// decodeResponses indexes by request id: the SDK dispatches each request on its
// own goroutine, so response order is not the request order.
func decodeResponses(t *testing.T, output string) map[int]map[string]any {
	t.Helper()
	responses := map[int]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("undecodable response %q: %v", line, err)
		}
		id, ok := response["id"].(float64)
		if !ok {
			t.Fatalf("response without a numeric id: %#v", response)
		}
		responses[int(id)] = response
	}
	return responses
}

// serve runs the sidecar over in-memory JSON Lines streams, exactly as the host
// drives it, and returns the decoded responses keyed by request id.
func serve(t *testing.T, instance *plugin, requests ...string) map[int]map[string]any {
	t.Helper()
	input := bytes.NewBufferString(strings.Join(requests, ""))
	var output, errors bytes.Buffer
	server := dbxpluginsdk.NewServer(
		dbxpluginsdk.Metadata{ID: "com.tenltrs.dbx-calendar", Version: "0.1.5"},
		instance,
	).WithIO(input, &output, &errors)
	if err := server.Serve(); err != nil {
		t.Fatalf("serve failed: %v (stderr: %s)", err, errors.String())
	}
	return decodeResponses(t, output.String())
}

// This is the wiring the Svelte UI depends on, driven over the real protocol.
func TestProtocolWorkbenchFlow(t *testing.T) {
	t.Setenv(dataDirEnvVar, t.TempDir())
	dir := t.TempDir()

	instance := &plugin{
		connections: map[string]*connNotes{},
		notes:       newNotesStore(dir),
	}

	responses := serve(t, instance,
		requestLine(1, "plugin/initialize", map[string]any{
			"host": map[string]any{"protocolVersions": []int{1}},
		}),
		// The workbench is addressed without a connection at all.
		requestLine(2, "dbx-calendar/notes/list", map[string]any{}),
	)
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d: %#v", len(responses), responses)
	}

	initialize := responses[1]["result"].(map[string]any)
	if initialize["protocolVersion"] != float64(dbxpluginsdk.ProtocolVersion) {
		t.Fatalf("unexpected initialize result: %#v", initialize)
	}

	empty := responses[2]["result"].(map[string]any)
	if len(empty["notes"].(map[string]any)) != 0 {
		t.Fatalf("expected no notes on a fresh install: %#v", empty)
	}

	// Writes are driven separately: the SDK dispatches each request on its own
	// goroutine, so a list in the same batch could legitimately observe the
	// write that raced with it.
	responses = serve(t, instance,
		requestLine(1, "dbx-calendar/notes/set", map[string]any{"date": "2026-02-14", "value": "dinner"}),
	)
	saved := responses[1]["result"].(map[string]any)
	if saved["success"] != true {
		t.Fatalf("unexpected save result: %#v", saved)
	}
	notes := saved["notes"].(map[string]any)
	if notes["2026-02-14"] != "dinner" {
		t.Fatalf("expected the note in the response: %#v", notes)
	}

	// A second sidecar process reading the same data directory must see the
	// note: this is the restart path the host takes on every reconnect.
	restarted := &plugin{
		connections: map[string]*connNotes{},
		notes:       newNotesStore(dir),
	}
	responses = serve(t, restarted, requestLine(1, "dbx-calendar/notes/list", map[string]any{}))
	reloaded := responses[1]["result"].(map[string]any)
	if reloaded["notes"].(map[string]any)["2026-02-14"] != "dinner" {
		t.Fatalf("expected the note to survive a restart: %#v", reloaded)
	}
}

// An import replaces the whole set in one round trip, which is what the export
// dialog's companion path relies on.
func TestProtocolReplaceNotes(t *testing.T) {
	dir := t.TempDir()
	instance := &plugin{
		connections: map[string]*connNotes{},
		notes:       newNotesStore(dir),
	}

	serve(t, instance,
		requestLine(1, "dbx-calendar/notes/set", map[string]any{"date": "2026-01-01", "value": "old"}),
	)

	responses := serve(t, instance, requestLine(1, "dbx-calendar/notes/replace", map[string]any{
		"notes": map[string]any{
			"2026-02-14": "dinner",
			"2026-02-15": "lunch",
		},
	}))
	replaced := responses[1]["result"].(map[string]any)
	if replaced["success"] != true {
		t.Fatalf("unexpected replace result: %#v", replaced)
	}
	notes := replaced["notes"].(map[string]any)
	if len(notes) != 2 || notes["2026-02-14"] != "dinner" {
		t.Fatalf("expected the replaced set: %#v", notes)
	}
	// Replace is a replacement, not a merge: the pre-existing date is gone.
	if _, present := notes["2026-01-01"]; present {
		t.Fatalf("expected the old date to be dropped: %#v", notes)
	}

	// A payload of the wrong shape is rejected rather than half-applied.
	responses = serve(t, instance, requestLine(1, "dbx-calendar/notes/replace", map[string]any{
		"notes": map[string]any{"2026-02-14": 42},
	}))
	if responses[1]["error"] == nil {
		t.Fatalf("expected a non-string note to be rejected: %#v", responses[1])
	}
}

// A malformed date key in an import must be refused rather than written.
func TestNotesReplaceRejectsBadDate(t *testing.T) {
	store := newNotesStore(t.TempDir())
	handle, err := store.Open("conn-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Replace(map[string]string{"2026-2-14": "x"}); err == nil {
		t.Fatal("expected a non-canonical date key to be rejected")
	}
	if err := handle.Replace(map[string]string{"../escape": "x"}); err == nil {
		t.Fatal("expected an unsafe date key to be rejected")
	}
}

// The data directory layout must stay what the README documents.
func TestDataDirectoryLayout(t *testing.T) {
	dir := t.TempDir()
	notes := newNotesStore(dir)
	handle, err := notes.Open(workbenchNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Set("2026-02-14", "dinner"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "notes", "workbench.json")); err != nil {
		t.Fatalf("expected notes/workbench.json to exist: %v", err)
	}
	// The holiday schedule is not stored: a year chinese-days does not carry
	// simply has no markers.
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatal(err)
	} else {
		for _, entry := range entries {
			if entry.Name() == "holiday" {
				t.Fatal("expected no holiday directory in the data directory")
			}
		}
	}
}

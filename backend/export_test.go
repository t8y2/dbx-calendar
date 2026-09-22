package main

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func encodeForTest(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func TestExportWritesToTheGivenPath(t *testing.T) {
	dir := t.TempDir()
	store := newExportStore(dir)
	target := filepath.Join(dir, "notes.xls")

	written, err := store.Write(target, encodeForTest([]byte("workbook")), false)
	if err != nil {
		t.Fatal(err)
	}
	if written.Path != target {
		t.Fatalf("expected the file at %s, got %s", target, written.Path)
	}
	if written.Directory != dir {
		t.Fatalf("expected the directory %s, got %s", dir, written.Directory)
	}
	if written.Size != int64(len("workbook")) {
		t.Fatalf("unexpected size: %d", written.Size)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "workbook" {
		t.Fatalf("unexpected contents: %q", body)
	}
}

// A second export on the same day must not replace the first: the destination
// is dated rather than unique, so collisions are ordinary.
func TestExportKeepsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	store := newExportStore(dir)
	target := filepath.Join(dir, "notes.xls")

	first, err := store.Write(target, encodeForTest([]byte("one")), false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Write(target, encodeForTest([]byte("two")), false)
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.Write(target, encodeForTest([]byte("three")), false)
	if err != nil {
		t.Fatal(err)
	}

	if first.Path != target {
		t.Fatalf("unexpected first path: %s", first.Path)
	}
	want := []string{
		filepath.Join(dir, "notes (2).xls"),
		filepath.Join(dir, "notes (3).xls"),
	}
	for i, path := range []string{second.Path, third.Path} {
		if path != want[i] {
			t.Fatalf("expected %s, got %s", want[i], path)
		}
	}
	// The original must still hold what it held.
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "one" {
		t.Fatalf("the first export was replaced: %q", body)
	}
}

// overwrite means the user named this exact file in the save dialog, where the
// OS already asked about replacing it.
func TestExportOverwritesWhenAsked(t *testing.T) {
	dir := t.TempDir()
	store := newExportStore(dir)
	target := filepath.Join(dir, "notes.xls")

	if _, err := store.Write(target, encodeForTest([]byte("one")), false); err != nil {
		t.Fatal(err)
	}
	written, err := store.Write(target, encodeForTest([]byte("two")), true)
	if err != nil {
		t.Fatal(err)
	}
	if written.Path != target {
		t.Fatalf("expected an in-place write, got %s", written.Path)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "two" {
		t.Fatalf("expected the file to be replaced: %q", body)
	}
}

func TestExportRejectsUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	store := newExportStore(dir)
	payload := encodeForTest([]byte("x"))

	// Only absolute paths are accepted, and the name has to be a plain file
	// name rather than a path or a bare directory reference.
	for _, path := range []string{
		"",
		"   ",
		"notes.xls",
		filepath.Join("sub", "notes.xls"),
		dir,
		dir + string(filepath.Separator),
	} {
		if _, err := store.Write(path, payload, false); err == nil {
			t.Fatalf("expected %q to be rejected", path)
		}
	}
}

// A ".." segment is normalised rather than refused: after Clean the value names
// exactly one file, which is the property that matters. The path that would
// escape is the one that never reaches here as written.
func TestExportNormalisesClimbingPaths(t *testing.T) {
	dir := t.TempDir()
	store := newExportStore(dir)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}

	climbing := filepath.Join(dir, "sub", "..", "notes.xls")
	written, err := store.Write(climbing, encodeForTest([]byte("x")), true)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "notes.xls"); written.Path != want {
		t.Fatalf("expected %s, got %s", want, written.Path)
	}
}


func TestExportRejectsBadPayload(t *testing.T) {
	dir := t.TempDir()
	store := newExportStore(dir)
	target := filepath.Join(dir, "notes.xls")

	if _, err := store.Write(target, "not base64!", false); err == nil {
		t.Fatal("expected invalid base64 to be rejected")
	}
	if _, err := store.Write(target, encodeForTest([]byte("x")), false); err != nil {
		t.Fatalf("expected a valid payload to succeed: %v", err)
	}
}

func TestExportWithoutOverwriteCreatesMissingFolders(t *testing.T) {
	dir := t.TempDir()
	store := newExportStore(dir)
	target := filepath.Join(dir, "a", "b", "notes.xls")

	written, err := store.Write(target, encodeForTest([]byte("x")), false)
	if err != nil {
		t.Fatal(err)
	}
	if written.Path != target {
		t.Fatalf("unexpected path: %s", written.Path)
	}
}

// The remembered folder is what makes the second export open where the first
// one landed.
func TestExportRemembersTheChosenFolder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "somewhere")
	if err := os.MkdirAll(first, 0o700); err != nil {
		t.Fatal(err)
	}

	store := newExportStore(filepath.Join(dir, "data"))
	if _, err := store.Write(filepath.Join(first, "notes.xls"), encodeForTest([]byte("x")), false); err != nil {
		t.Fatal(err)
	}
	store.Remember(filepath.Join(first, "notes.xls"))

	// A second process reading the same data directory must see it: this is the
	// restart path the host takes on every reconnect.
	restarted := newExportStore(filepath.Join(dir, "data"))
	if got := restarted.Destination(); got != first {
		t.Fatalf("expected the remembered folder %s, got %s", first, got)
	}
}

// A remembered folder that has since been deleted must not be offered.
func TestExportForgetsAMissingFolder(t *testing.T) {
	dir := t.TempDir()
	store := newExportStore(filepath.Join(dir, "data"))
	gone := filepath.Join(dir, "gone")
	store.Remember(filepath.Join(gone, "notes.xls"))

	if got := store.Destination(); got == gone {
		t.Fatalf("expected the missing folder to be dropped, got %s", got)
	}
}

func TestExportDestinationFallsBack(t *testing.T) {
	// With nothing remembered, the destination is the downloads folder when it
	// exists, and the plugin's own exports folder otherwise. Either way it must
	// be a directory, never the empty string.
	store := newExportStore(t.TempDir())
	if got := store.Destination(); got == "" {
		t.Fatal("expected a destination")
	}
}

func TestExportWithoutDataDirStillWrites(t *testing.T) {
	// The destination is the user's, so a missing plugin data directory only
	// costs the memory of where they exported last -- the write itself must
	// still work.
	dir := t.TempDir()
	store := newExportStore("")
	target := filepath.Join(dir, "notes.xls")

	written, err := store.Write(target, encodeForTest([]byte("x")), false)
	if err != nil {
		t.Fatal(err)
	}
	if written.Path != target {
		t.Fatalf("unexpected path: %s", written.Path)
	}
	store.Remember(target)

	// Whatever is offered as a destination has to be usable as one: an empty
	// answer is honest, a relative path would write somewhere unpredictable.
	if got := store.Destination(); got != "" && !filepath.IsAbs(got) {
		t.Fatalf("expected an absolute destination or none, got %q", got)
	}
}

func TestDecodePickSanitisesTheExtension(t *testing.T) {
	// The extension ends up in a dialog filter, so anything that is not a plain
	// word falls back rather than reaching the shell.
	for _, given := range []string{".xls", "xls", "txt"} {
		_, _, _, extension, err := decodePick(map[string]any{
			"initial": `C:\out\notes.xls`, "extension": given,
		})
		if err != nil {
			t.Fatal(err)
		}
		if extension != given[1:] && extension != given {
			t.Fatalf("unexpected extension for %q: %s", given, extension)
		}
	}
	for _, given := range []string{"", "x ls", "x;rm", "*.xls"} {
		_, _, _, extension, err := decodePick(map[string]any{
			"initial": `C:\out\notes.xls`, "extension": given,
		})
		if err != nil {
			t.Fatal(err)
		}
		if extension != "xls" {
			t.Fatalf("expected the fallback for %q, got %s", given, extension)
		}
	}
	if _, _, _, _, err := decodePick(map[string]any{}); err == nil {
		t.Fatal("expected a pick without a starting path to be rejected")
	}
}

func TestDecodeExportRequiresPathAndData(t *testing.T) {
	if _, _, _, err := decodeExport(map[string]any{"data": "x"}); err == nil {
		t.Fatal("expected a missing path to be rejected")
	}
	if _, _, _, err := decodeExport(map[string]any{"path": "/tmp/x"}); err == nil {
		t.Fatal("expected missing data to be rejected")
	}
	_, _, overwrite, err := decodeExport(map[string]any{"path": "/tmp/x", "data": "x", "overwrite": true})
	if err != nil {
		t.Fatal(err)
	}
	if !overwrite {
		t.Fatal("expected overwrite to be read")
	}
}

// The shell-backed features are the plugin's only Windows-specific code, and
// they must say so rather than fail obscurely elsewhere.
func TestShellFeaturesAreWindowsOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this host has the shell these features need")
	}
	if err := setClipboard("x"); !errors.Is(err, ErrWindowsOnly) {
		t.Fatalf("expected ErrWindowsOnly, got %v", err)
	}
	if _, err := pickSavePath("notes.xls", "Export", "File", "xls"); !errors.Is(err, ErrWindowsOnly) {
		t.Fatalf("expected ErrWindowsOnly, got %v", err)
	}
	if _, err := revealInExplorer("notes.xls"); !errors.Is(err, ErrWindowsOnly) {
		t.Fatalf("expected ErrWindowsOnly, got %v", err)
	}
}

func TestPowerShellCommandEncoding(t *testing.T) {
	// -EncodedCommand wants UTF-16LE, so an ASCII character encodes to two
	// bytes: "A" is 0x41 0x00, which is "QQA=" in base64.
	if got := encodePowerShellCommand("A"); got != "QQA=" {
		t.Fatalf("unexpected encoding: %s", got)
	}
}

// The RPC the UI actually calls, driven over the real protocol rather than
// through the store directly: this is the wiring the dialog depends on.
func TestProtocolExportFlow(t *testing.T) {
	dataDir := t.TempDir()
	outDir := t.TempDir()
	instance := &plugin{
		connections: map[string]*connNotes{},
		notes:       newNotesStore(dataDir),
		exports:     newExportStore(dataDir),
	}

	target := filepath.Join(outDir, "notes.xls")
	responses := serve(t, instance,
		requestLine(1, "dbx-calendar/export/destination", map[string]any{}),
		requestLine(2, "dbx-calendar/export/write", map[string]any{
			"path": target,
			"data": encodeForTest([]byte("workbook")),
		}),
	)

	destination := responses[1]["result"].(map[string]any)
	if destination["directory"] == "" {
		t.Fatalf("expected a destination: %#v", destination)
	}

	written := responses[2]["result"].(map[string]any)
	if written["path"] != target {
		t.Fatalf("expected the file at %s, got %#v", target, written)
	}
	if written["success"] != true {
		t.Fatalf("unexpected write result: %#v", written)
	}

	// Writing remembers the folder, so the next dialog opens there.
	followup := serve(t, instance, requestLine(1, "dbx-calendar/export/destination", map[string]any{}))
	if got := followup[1]["result"].(map[string]any)["directory"]; got != outDir {
		t.Fatalf("expected the destination to be remembered as %s, got %v", outDir, got)
	}
}

// A destination the sidecar cannot honour has to come back as an error rather
// than as a write that quietly went somewhere else.
func TestProtocolExportRejectsRelativePaths(t *testing.T) {
	instance := &plugin{
		connections: map[string]*connNotes{},
		notes:       newNotesStore(t.TempDir()),
		exports:     newExportStore(t.TempDir()),
	}
	responses := serve(t, instance, requestLine(1, "dbx-calendar/export/write", map[string]any{
		"path": "notes.xls",
		"data": encodeForTest([]byte("x")),
	}))
	if _, failed := responses[1]["error"]; !failed {
		t.Fatalf("expected an error response: %#v", responses[1])
	}
}

// The shell-backed methods carry their errors through the protocol intact, so
// the UI can show what actually went wrong.
//
// This deliberately stops at the argument check: anything further would open a
// real dialog, put something on the user's clipboard, or launch Explorer while
// the test suite runs.
func TestProtocolExportShellFeaturesValidateArguments(t *testing.T) {
	instance := &plugin{
		connections: map[string]*connNotes{},
		notes:       newNotesStore(t.TempDir()),
		exports:     newExportStore(t.TempDir()),
	}
	responses := serve(t, instance,
		requestLine(1, "dbx-calendar/export/copy", map[string]any{}),
		requestLine(2, "dbx-calendar/export/pick", map[string]any{}),
	)
	if _, failed := responses[1]["error"]; !failed {
		t.Fatalf("expected a missing text to be rejected: %#v", responses[1])
	}
	if _, failed := responses[2]["error"]; !failed {
		t.Fatalf("expected a pick with no starting path to be rejected: %#v", responses[2])
	}
}

func TestPowerShellQuoting(t *testing.T) {
	// An apostrophe in a folder name is ordinary input, not an edge case.
	if got := quotePowerShell(`D:\it's\x.xls`); got != `'D:\it''s\x.xls'` {
		t.Fatalf("unexpected quoting: %s", got)
	}
}

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxExportBytes bounds one exported file. The payload crosses the bridge as
// base64 inside a JSON message, and the SDK caps a JSON frame at 8 MiB, so the
// decoded budget has to stay comfortably under that. Base64 inflates by 4/3.
const maxExportBytes = 4 * 1024 * 1024

// ErrExportTooLarge is returned when an export exceeds the bridge budget.
var ErrExportTooLarge = errors.New("the export is too large to write")

// exportTargetFile remembers the folder the user last exported into, as a plain
// line of text. It lives beside the notes so a restart keeps the same
// destination.
const exportTargetFile = "export-dir"

// exportStore writes the file the UI cannot write itself.
//
// The UI runs in a sandboxed frame with an opaque origin: a download is dropped
// without an error, and no file API is exposed at all. The sidecar is an
// ordinary process, so the destination is the user's to choose and this is what
// honours it.
type exportStore struct {
	dataDir string
}

func newExportStore(dataDir string) *exportStore {
	return &exportStore{dataDir: dataDir}
}

// WriteResult is what the UI reports to the user.
type WriteResult struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Directory string `json:"directory"`
}

// Destination is the folder an export should be offered in before the user has
// chosen one: the last folder they picked, else their downloads folder, else
// the plugin's own exports folder.
//
// It returns "" when none of those can be resolved, rather than a relative
// path: a relative destination would be resolved against whatever working
// directory the sidecar happens to have, which is not a place to put a file.
func (store *exportStore) Destination() string {
	if remembered := store.rememberedDirectory(); remembered != "" {
		return remembered
	}
	if home, err := os.UserHomeDir(); err == nil {
		downloads := filepath.Join(home, "Downloads")
		if info, err := os.Stat(downloads); err == nil && info.IsDir() {
			return downloads
		}
	}
	if store.dataDir == "" {
		return ""
	}
	return filepath.Join(store.dataDir, "exports")
}

// Remember records the folder of a path the user chose, so the next export
// opens there. Failing to remember is not worth failing an export over.
func (store *exportStore) Remember(path string) {
	if store.dataDir == "" {
		return
	}
	directory := filepath.Dir(path)
	if directory == "" || directory == "." {
		return
	}
	if err := os.MkdirAll(store.dataDir, 0o700); err != nil {
		return
	}
	target := filepath.Join(store.dataDir, exportTargetFile)
	// Atomic replace, so an interrupted write cannot leave half a path behind.
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(directory), 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
	}
}

func (store *exportStore) rememberedDirectory() string {
	if store.dataDir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(store.dataDir, exportTargetFile))
	if err != nil {
		return ""
	}
	directory := strings.TrimSpace(string(raw))
	if directory == "" {
		return ""
	}
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		return ""
	}
	return directory
}

// Write decodes the payload and stores it at path.
//
// The path comes from the UI and is where the file goes — this is a deliberate
// widening of what the sidecar will write. The old rule was "never outside the
// exports folder"; that cannot survive a user-chosen destination, so the guard
// is now about the shape of the value rather than its location: an absolute,
// cleaned path with a plain file name. The caller is the plugin's own UI, in
// the same process tree, and the user picked the folder through the OS dialog.
//
// overwrite says the user named this exact path in the save dialog, where the
// OS already asked about replacing an existing file. Without it, an existing
// file is never replaced: the name gains " (2)", " (3)" and so on.
func (store *exportStore) Write(path string, encoded string, overwrite bool) (WriteResult, error) {
	target, err := exportPath(path)
	if err != nil {
		return WriteResult{}, err
	}

	// An oversized payload is rejected before decoding, so a hostile or buggy
	// caller cannot make us allocate far beyond the budget.
	if len(encoded) > maxExportBytes/3*4+8 {
		return WriteResult{}, ErrExportTooLarge
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return WriteResult{}, fmt.Errorf("export payload is not valid base64: %w", err)
	}
	if len(data) > maxExportBytes {
		return WriteResult{}, ErrExportTooLarge
	}

	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return WriteResult{}, err
	}
	// A path that names an existing folder is a mistake worth naming plainly.
	// This has to run before the collision rule below, which would otherwise
	// treat the folder as a taken name and write beside it.
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return WriteResult{}, fmt.Errorf("export path is a folder: %q", target)
	}
	if !overwrite {
		target = availablePath(target)
	}

	// Atomic replace, so a failure cannot leave a truncated workbook behind.
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return WriteResult{}, err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return WriteResult{}, err
	}
	return WriteResult{Path: target, Size: int64(len(data)), Directory: directory}, nil
}

// exportPath checks the caller-supplied destination. The name has to be a plain
// file name: a separator or a ".." segment would let the value mean a different
// file than the one the dialog showed.
func exportPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("export path is required")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("export path must be absolute: %q", path)
	}
	cleaned := filepath.Clean(path)
	name := filepath.Base(cleaned)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return "", fmt.Errorf("unsafe export name: %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("unsafe export name: %q", name)
	}
	return cleaned, nil
}

// availablePath returns target, or the first "name (2)", "name (3)" … that is
// free. Exports are dated rather than unique, so a second export on the same day
// lands next to the first instead of quietly replacing it.
func availablePath(target string) string {
	if _, err := os.Stat(target); err != nil {
		return target
	}
	extension := filepath.Ext(target)
	stem := strings.TrimSuffix(target, extension)
	for n := 2; n < 1000; n++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, n, extension)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
	return target
}

// decodeExport reads a write request out of the decoded params. The values
// arrive as generic JSON, so the shape is checked here next to the handler.
func decodeExport(values map[string]any) (string, string, bool, error) {
	path, _ := values["path"].(string)
	encoded, _ := values["data"].(string)
	if path == "" || encoded == "" {
		return "", "", false, errors.New("an export needs both a path and data")
	}
	overwrite, _ := values["overwrite"].(bool)
	return path, encoded, overwrite, nil
}

// decodePick reads a save-dialog request. Title and filter label are UI copy,
// passed in already localized so the sidecar holds no user-facing text.
func decodePick(values map[string]any) (initial, title, label, extension string, err error) {
	initial, _ = values["initial"].(string)
	title, _ = values["title"].(string)
	label, _ = values["label"].(string)
	extension, _ = values["extension"].(string)
	// The extension lands in a dialog filter, so keep it to letters and digits.
	extension = strings.TrimLeft(extension, ".")
	if extension == "" || strings.ContainsFunc(extension, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) {
		extension = "xls"
	}
	if title == "" {
		title = "Export"
	}
	if label == "" {
		label = "File"
	}
	if initial == "" {
		return "", "", "", "", errors.New("a pick needs a starting path")
	}
	return initial, title, label, extension, nil
}

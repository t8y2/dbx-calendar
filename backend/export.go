package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxExportBytes bounds one exported file. The payload crosses the bridge as
// base64 inside a JSON message, and the SDK caps a JSON frame at 8 MiB, so the
// decoded budget has to stay comfortably under that. Base64 inflates by 4/3.
const maxExportBytes = 4 * 1024 * 1024

// ErrExportTooLarge is returned when an export exceeds the bridge budget.
var ErrExportTooLarge = errors.New("the export is too large to write")

// exportStore writes exported workbooks and text files into the plugin data
// directory.
//
// This exists because a plugin UI cannot hand a file to the user itself: the
// host renders the UI in a sandboxed frame without the allow-downloads flag, so
// an anchor click with a blob URL is dropped silently, and the host bridge
// exposes no download or save method. The sidecar is an ordinary process, so it
// can write the file and report where it landed.
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

// Write decodes the payload and stores it under the data directory. It returns
// ErrNoDataDir when the host provided none, which the UI surfaces rather than
// pretending the export succeeded.
func (store *exportStore) Write(name string, encoded string) (WriteResult, error) {
	if store.dataDir == "" {
		return WriteResult{}, ErrNoDataDir
	}
	directory := filepath.Join(store.dataDir, "exports")
	if err := os.MkdirAll(directory, 0o700); err != nil {
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

	path, err := exportPath(directory, name)
	if err != nil {
		return WriteResult{}, err
	}
	// Atomic replace, so a failure cannot leave a truncated workbook behind.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return WriteResult{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return WriteResult{}, err
	}
	return WriteResult{Path: path, Size: int64(len(data)), Directory: directory}, nil
}

// exportPath sanitises the caller-supplied name and appends a timestamp so two
// exports of the same day do not overwrite each other.
func exportPath(directory, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("export name is required")
	}
	// The name reaches us from the UI; never let it escape the exports folder.
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("unsafe export name: %q", name)
	}
	extension := filepath.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	if len(stem) > 64 {
		stem = stem[:64]
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	return filepath.Join(directory, fmt.Sprintf("%s-%s%s", stem, stamp, extension)), nil
}

// decodeExport reads the write request out of the decoded params. The values
// arrive as generic JSON, so the shape is checked here next to the handler.
func decodeExport(values map[string]any) (string, string, error) {
	name, _ := values["name"].(string)
	encoded, _ := values["data"].(string)
	if name == "" || encoded == "" {
		return "", "", errors.New("an export needs both a name and data")
	}
	return name, encoded, nil
}

package main

import (
	"errors"
	"os"
)

// dataDirEnvVar names the host-provided directory, matching
// dbxpluginsdk.DataDirEnvVar.
const dataDirEnvVar = "DBX_PLUGIN_DATA_DIR"

// ensureDataDir mirrors dbxpluginsdk.EnsureDataDir().
//
// The SDK ships that helper in the release whose sdk.go also defines
// DataDirEnvVar/DataDir/EnsureDataDir; the copy this module builds against today
// (bundled with @dbx-app/plugin-cli) predates it. Once go.mod requires that
// release, replace this function with the SDK call of the same name -- main.go's
// call site does not change.
func ensureDataDir() (string, error) {
	dir := os.Getenv(dataDirEnvVar)
	if dir == "" {
		return "", errors.New("plugin data directory is not set; running outside a DBX host")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

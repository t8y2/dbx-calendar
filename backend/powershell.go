package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrWindowsOnly marks the host features that need a Windows shell: the native
// save dialog, revealing a file in Explorer, and the clipboard. The UI hides or
// reports these rather than failing silently on another platform.
var ErrWindowsOnly = errors.New("this feature needs Windows")

// The plugin UI cannot open a native dialog or reveal a file itself: it runs in
// a sandboxed frame with an opaque origin, where the File System Access API is
// not exposed at all. The sidecar is an ordinary process, so it borrows the
// shell instead.
//
// Three details below are not stylistic, they are all that stands between a
// working dialog and a silent failure:
//
//   - `-STA` is required by the Windows Forms dialogs; without it ShowDialog
//     throws.
//   - The script travels as base64 UTF-16LE via -EncodedCommand, so the script
//     text itself never meets the console code page.
//   - Results come back through a UTF-8 temp file, never stdout. A piped
//     PowerShell 5.1 console writes GBK, which turns a Chinese path into
//     U+FFFD replacement characters and makes every later file open fail.
const powershellExe = "powershell"

// runPowerShell executes a script and returns its combined output, which is
// used for error messages only. Any value the caller needs must come back
// through a file, for the encoding reason above.
func runPowerShell(script string, environment ...string) ([]byte, error) {
	if runtime.GOOS != "windows" {
		return nil, ErrWindowsOnly
	}
	command := exec.Command(
		powershellExe,
		"-NoProfile", "-NonInteractive", "-STA",
		"-EncodedCommand", encodePowerShellCommand(script),
	)
	command.Env = append(os.Environ(), environment...)
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return output, fmt.Errorf("powershell failed: %s", detail)
	}
	return output, nil
}

// encodePowerShellCommand base64-encodes the script as UTF-16LE, which is what
// -EncodedCommand expects.
func encodePowerShellCommand(script string) string {
	units := utf16.Encode([]rune(script))
	raw := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		raw = append(raw, byte(unit), byte(unit>>8))
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// quotePowerShell makes a value safe inside a single-quoted PowerShell string,
// where the only escape is a doubled quote. Paths reach this from the user, so
// an apostrophe in a folder name is ordinary input, not an edge case.
func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// readUTF8File reads a file PowerShell wrote with `Out-File -Encoding utf8`,
// which prefixes a BOM. The decode is strict on purpose: a surprise here is
// exactly the encoding bug this whole detour exists to avoid, and it should
// surface at this line rather than as a missing file later.
func readUTF8File(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	body := bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(body) {
		return "", fmt.Errorf("%s is not valid UTF-8", filepath.Base(path))
	}
	return strings.TrimSpace(string(body)), nil
}

// scratchFile reserves a unique path for one shell round trip. The prefix keeps
// concurrent calls from colliding, and the caller removes it afterwards.
func scratchFile(prefix string) (string, error) {
	file, err := os.CreateTemp("", fmt.Sprintf("dbx-calendar-%s-*", prefix))
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		return "", err
	}
	return name, nil
}

// pickSavePath opens the OS "save as" dialog and returns the chosen path, or ""
// when the user cancels.
//
// The dialog blocks for as long as the user browses, so the caller's RPC waits
// too. The host bridge caps an invoke at 120 seconds; a browse that outlives
// that fails the request, which the user can simply retry.
func pickSavePath(initial, title, filterLabel, extension string) (string, error) {
	out, err := scratchFile("pick")
	if err != nil {
		return "", err
	}
	defer os.Remove(out)

	directory := filepath.Dir(initial)
	name := filepath.Base(initial)
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$top = New-Object System.Windows.Forms.Form
$top.TopMost = $true
$top.MinimizeBox = $false
$dlg = New-Object System.Windows.Forms.SaveFileDialog
$dlg.Title = %s
$dlg.Filter = %s
$dlg.FileName = %s
$dlg.InitialDirectory = %s
$dlg.OverwritePrompt = $true
$dlg.AddExtension = $true
if ($dlg.ShowDialog($top) -eq [System.Windows.Forms.DialogResult]::OK) {
  $dlg.FileName | Out-File -FilePath $env:DBX_CALENDAR_PS_OUT -Encoding utf8
} else {
  '' | Out-File -FilePath $env:DBX_CALENDAR_PS_OUT -Encoding utf8
}
$top.Dispose()
`,
		quotePowerShell(title),
		quotePowerShell(fmt.Sprintf("%s|*.%s", filterLabel, extension)),
		quotePowerShell(name),
		quotePowerShell(directory),
	)

	// The owner form is TopMost so the dialog cannot open behind the host
	// window, where the user would never see it.
	if _, err := runPowerShell(script, "DBX_CALENDAR_PS_OUT="+out); err != nil {
		return "", err
	}
	chosen, err := readUTF8File(out)
	if err != nil {
		return "", err
	}
	return chosen, nil
}

// revealInExplorer opens the containing folder with the file selected. A file
// that does not exist yet still gets its folder opened, which is the useful
// half of the gesture before the first export.
func revealInExplorer(path string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", ErrWindowsOnly
	}
	folder := filepath.Dir(path)
	target := path
	// A file that does not exist yet still gets its folder opened, which is the
	// useful half of the gesture before the first export.
	if _, err := os.Stat(path); err != nil {
		target = folder
	}
	if _, err := os.Stat(target); err != nil {
		return "", fmt.Errorf("no folder to open: %s", folder)
	}

	// explorer.exe reports a non-zero exit code even when it succeeds, so the
	// process is started and left alone rather than waited on.
	command := exec.Command("explorer", "/select,"+target)
	if err := command.Start(); err != nil {
		return "", fmt.Errorf("could not open the folder: %w", err)
	}
	return folder, nil
}

// setClipboard puts text on the clipboard.
//
// The text travels through a temp file for the same reason results do: it may
// hold Chinese, quotes and newlines, and none of those survive a command line
// intact.
func setClipboard(text string) error {
	if runtime.GOOS != "windows" {
		return ErrWindowsOnly
	}
	in, err := scratchFile("clip")
	if err != nil {
		return err
	}
	defer os.Remove(in)
	if err := os.WriteFile(in, []byte(text), 0o600); err != nil {
		return err
	}

	script := `
$text = [System.IO.File]::ReadAllText($env:DBX_CALENDAR_PS_IN, [System.Text.Encoding]::UTF8)
Set-Clipboard -Value $text
`
	if _, err := runPowerShell(script, "DBX_CALENDAR_PS_IN="+in); err != nil {
		return fmt.Errorf("could not copy to the clipboard: %w", err)
	}
	return nil
}

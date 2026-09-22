<img src="assets/icon.svg" width="96" alt="Calendar Notes icon">

# Calendar Notes

A month-view calendar for [DBX](https://dbxio.com). Each day carries its Chinese lunar date, solar term or public-holiday arrangement, and you can double-click any day to write a note on it.

Also available in [简体中文](README.zh-CN.md).

## Features

**The grid**

- Month view with a weekday header row, weeks starting on Monday
- Weekend columns tinted, days from the neighbouring months dimmed
- Month navigation and a "Today" button
- Today's cell is outlined and tinted
- The grid fills the panel height and scrolls internally

**Each day cell**

- Day number, then one label chosen by priority: **lunar festival → public holiday → solar term → lunar date**
- A `休` / `Off` or `班` / `Work` marker for statutory days off and 调休 makeup workdays
- A multi-day holiday is labelled on the day itself; the other days show their lunar date

**Notes**

- Double-click a day to open the editor
- `Ctrl`/`Cmd` + `Enter` saves, `Esc` closes, and the same editor clears an existing note
- Notes persist through the Go sidecar, under the host's plugin data directory

**Export and import**

- The toolbar menu exports notes as `.xls` or `.txt`, over all dates or a range, or on a custom range
- The destination is shown in the dialog and can be changed; a copy-path and an open-folder button act on it
- Import reads the first sheet of an `.xls` or `.xlsx` file, one date and note per row
- The date column accepts text, a real date cell, or an Excel serial number
- Rows whose first cell is not a date — the header and blanks — are skipped
- Dates the file and the calendar both carry are reported as conflicts, resolved by overwrite, skip, or merge
- Import replaces the whole note set in one call, so a failure cannot leave a half-applied import behind

**Host integration**

- Follows the DBX host locale, Chinese or English
- Uses the host's `Canvas` / `CanvasText` theme colours

## Requirements

- DBX `>=0.6.0`, host API `1`
- Node.js for the UI, Go 1.22 for the sidecar, and the `dbx-plugin` CLI for development

## Develop

```bash
npm install
dbx-plugin dev --path . --port 5190
```

Source in `src/` is compiled by Vite into `ui/`, which is what the host loads; `backend/` is compiled into the sidecar binary. Use the real DBX host for final integration testing.

## Architecture

The plugin has two halves that talk over the DBX plugin protocol:

- **`src/` — the Svelte UI.** Renders the month grid and the note editor. Lunar dates, the 24 solar terms and festivals are computed locally, so a cell never waits on the sidecar.
- **`backend/` — the Go sidecar.** Owns the one thing that must survive a restart: the notes.

The split follows the data: anything computed deterministically stays in the UI, anything that is stored goes through the sidecar.

`src/dbx.js` is the only module that touches the host bridge. If the bridge is absent — the page opened directly in a browser — the calendar still renders using locally computed data, and note writes report that they could not be stored.

### Where notes are stored

The host sets `DBX_PLUGIN_DATA_DIR` on every sidecar. Notes live in `notes/<namespace>.json` under that directory, written atomically (temp file plus rename) so an interrupted write cannot corrupt the file. A file that fails to parse is renamed aside as `*.corrupt-<timestamp>` rather than deleted, because notes are not reproducible.

The namespace is the connection id when the host addresses a connection, and the fixed string `workbench` otherwise. Without a data directory the sidecar still runs, but a note write is refused with an explicit error instead of a fake success, and the UI shows it.

### Where exports go

The host renders this UI in a sandboxed frame with an opaque origin, and a frame without `allow-downloads` **silently drops** an anchor click on a blob URL: nothing throws, and no file arrives. That route is therefore not an option inside DBX, which leaves the sidecar — an ordinary process, which can put the file wherever the user says.

The export dialog shows the destination and lets the user change it:

- The path starts at the folder they last exported into, or their downloads folder the first time. It is remembered under the data directory as `export-dir`.
- **Choose…** opens the OS save dialog, so the folder *and* the file name are the user's. Whatever it returns is used verbatim; the OS asks about replacing an existing file there, so that file is replaced.
- A destination that was not named by hand is never allowed to overwrite: a taken name gains ` (2)`, ` (3)` and so on.
- **Copy path** and **Open folder** act on the same path, the latter revealing the file in Explorer.

A failed write is surfaced rather than reported as a successful export. Opening the page outside the host leaves no sidecar to write for us, so a plain browser tab falls back to its own download.

The native save dialog, revealing a file and the clipboard all go through the Windows shell, so **those three features are Windows-only**; the export itself is portable.

## Build and release

```bash
npm run build          # compile src/ into ui/
dbx-plugin package .   # build the sidecar and produce dist/*.dbxp
```

Publishing a GitHub release triggers `.github/workflows/plugin-release.yml`, which packages the plugin and hands it to the shared DBX release workflow.

## Calendar data

Lunar dates, the 24 solar terms and traditional festivals are computed and available for any year. They come from [`chinese-days`](https://www.npmjs.com/package/chinese-days).

Public holidays and 调休 makeup workdays are published by the State Council each year, and `chinese-days` carries that schedule up to 2026. [`src/lunar.js`](src/lunar.js) probes the library for the year it is rendering: when the library has a schedule, each cell also shows its `休` / `班` marker; when it does not, the cell shows its lunar date and solar term only.

There is deliberately no fallback data source. A year the library does not carry simply has no holiday markers, and the next year's arrangement arrives by upgrading the dependency.

## Project structure

```
src/
  App.svelte         month grid, day cells, note editor
  lunar.js           lunar dates, solar terms, holiday priority
  dbx.js             the only module that talks to the host bridge
  main.js            mounts the app
backend/
  main.go            sidecar entry point and the RPC method table
  notes.go           atomic per-namespace note storage
  export.go          writes the exported file wherever the user pointed it
  powershell.go      native save dialog, Explorer and the clipboard (Windows)
  datadir.go         resolves the host-provided data directory
assets/              plugin icons
ui/                  build output — generated, do not edit
manifest.json        DBX plugin manifest
dbx-plugin.toml      packaging, backend and dev commands
```

## Built with

Svelte 5 (runes) and Vite 7 for the UI; Go 1.22 and the DBX Go plugin SDK for the sidecar. The UI reaches the sidecar only through the `window.dbxPlugin` bridge.

See the [DBX plugin development guide](https://dbxio.com/en/docs/plugin-development) for the manifest format, Host API and packaging.

## License

Apache-2.0. See [LICENSE](LICENSE).

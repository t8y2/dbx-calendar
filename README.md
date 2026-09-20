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
- Notes persist in `localStorage`

**Host integration**

- Follows the DBX host locale, Chinese or English
- Uses the host's `Canvas` / `CanvasText` theme colours

## Requirements

- DBX `>=0.5.68`, host API `1`
- Node.js and the `dbx-plugin` CLI for development

## Develop

```bash
npm install
dbx-plugin dev --path . --port 5190
```

Source in `src/` is compiled by Vite into `ui/`, which is what the host loads. Use the real DBX host for final integration testing.

## Build and release

```bash
npm run build          # compile src/ into ui/
dbx-plugin package .   # produce dist/*.dbxp
```

Publishing a GitHub release triggers `.github/workflows/plugin-release.yml`, which packages the plugin and hands it to the shared DBX release workflow.

## Calendar data

Lunar dates, the 24 solar terms and traditional festivals are computed and available for any year. They come from [`chinese-days`](https://www.npmjs.com/package/chinese-days).

Public holidays and 调休 makeup workdays are published by the State Council each year. They come from two places, selected in [`src/lunar.js`](src/lunar.js):

1. The `chinese-days` package, which carries the schedule up to 2026.
2. `json/holiday-<year>.json`, bundled by [`src/holiday-data.js`](src/holiday-data.js), for years the package does not cover.

For a year with no schedule in either place, the calendar shows lunar dates and solar terms only, without the `休` / `班` markers.

### Updating the holiday data

Add `json/holiday-<year>.json` once the arrangement is published, then rebuild. No code change and no dependency upgrade is needed.

```jsonc
{
  "holiday": {
    "01-01": { "holiday": true,  "name": "元旦" },
    "01-04": { "holiday": false, "name": "元旦后补班", "target": "元旦" }
  }
}
```

`holiday: true` marks a day off, `holiday: false` a makeup workday. Only `name` and `holiday` are read; the file shipped here also carries `date`, `wage` and `rest` fields, which the plugin ignores.

Files are inlined into the bundle at build time. The packaged plugin is self-contained and does not read from disk at runtime.

## Project structure

```
src/
  App.svelte         month grid, day cells, note editor
  lunar.js           lunar dates, solar terms, holiday priority
  holiday-data.js    bundles json/holiday-<year>.json
  main.js            mounts the app
json/                holiday schedules, one file per year
assets/              plugin icon
ui/                  build output — generated, do not edit
manifest.json        DBX plugin manifest
dbx-plugin.toml      packaging and dev commands
```

## Built with

Svelte 5 (runes) and Vite 7. The UI talks to the host through the `window.dbxPlugin` bridge.

See the [DBX plugin development guide](https://dbxio.com/en/docs/plugin-development) for the manifest format, Host API and packaging.

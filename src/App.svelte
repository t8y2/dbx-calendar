<script>
  import { onMount } from "svelte";
  // Imported statically on purpose. As a dynamic import Vite emits a relative
  // specifier, and the DBX host inlines the entry script, so it resolves against
  // the document root instead of /assets/ and the chunk 404s. Bundling it costs
  // size up front but cannot fail at runtime.
  //
  // The full build is used rather than the mini one because only it can write
  // and read the legacy .xls format; mini strips the CFB codec entirely.
  import XLSX from "xlsx/dist/xlsx.full.min.js";
  // Both icons are under Vite's inline threshold, so they become data URIs and
  // never turn into a runtime fetch.
  import xlsIcon from "../assets/xls.svg";
  import txtIcon from "../assets/txt.svg";
  import previousIcon from "../assets/previous.svg";
  import nextIcon from "../assets/next.svg";
  import { dayInfo } from "./lunar.js";
  import { invoke, whenReady, hostLocale } from "./dbx.js";

  /** Vite inlines small SVGs as data URIs whose payload contains single quotes,
   *  and an unquoted CSS url() rejects quote characters outright. Quoting here
   *  keeps the value legal wherever it ends up. */
  const maskVar = (url) => `--icon: url("${url}")`;

  const copy = {
    en: {
      prev: "Previous month",
      next: "Next month",
      today: "Today",
      weekdays: ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"],
      editorTitle: "Edit entry",
      placeholder: "Write something for this day…",
      save: "Save",
      cancel: "Cancel",
      clear: "Clear",
      hint: "Ctrl + Enter saves, Esc closes",
      hasNote: "has an entry",
      noNote: "no entry",
      dayOff: "Off",
      workday: "Work",
      edit: "double-click to edit",
      menu: "Menu",
      colDate: "Date",
      colContent: "Note",
      sheetName: "Notes",
      exported: (count) => `Exported ${count} ${count === 1 ? "entry" : "entries"}`,
      imported: (count, total, skipped) =>
        [`Imported ${count} ${count === 1 ? "entry" : "entries"}`, `${total} total`, skipped ? `${skipped} rows skipped` : null]
          .filter(Boolean)
          .join(", "),
      exportFailed: "Export failed",
      savedTo: (path) => `Saved to ${path}`,
      savedNoPath: "Saved, but the host did not report where",
      importFailed: "Could not read that file",
      saveFailed: "Could not save",
      fileExcel: "Excel sheet",
      fileText: "Text file",
      rangeLabel: "Date range",
      ranges: {
        "1m": "Past month",
        "3m": "Past 3 months",
        "6m": "Past 6 months",
        "1y": "Past year",
        all: "All dates",
        custom: "Custom",
      },
      entries: (count) => `${count} ${count === 1 ? "entry" : "entries"}`,
      nothingInRange: "Nothing in this range",
      export: "Export",
      chooseFile: "Choose a file",
      unsupportedFile: "Only .xls and .xlsx files can be imported",
      noStorage: "Notes cannot be saved here, and will be lost when the page closes. Export to keep them.",
      conflicts: (count) => `${count} conflicting ${count === 1 ? "date" : "dates"}`,
      modeOverwrite: "Overwrite",
      modeMerge: "Merge",
      modeSkip: "Skip",
      import: "Import",
    },
    zh: {
      prev: "上个月",
      next: "下个月",
      today: "今天",
      weekdays: ["周一", "周二", "周三", "周四", "周五", "周六", "周日"],
      editorTitle: "编辑内容",
      placeholder: "写点什么…",
      save: "保存",
      cancel: "取消",
      clear: "清除",
      hint: "Ctrl + Enter 保存，Esc 关闭",
      hasNote: "已有内容",
      noNote: "暂无内容",
      dayOff: "休",
      workday: "班",
      edit: "双击编辑",
      menu: "菜单",
      colDate: "日期",
      colContent: "内容",
      sheetName: "备忘",
      exported: (count) => `已导出 ${count} 条`,
      imported: (count, total, skipped) =>
        [`已导入 ${count} 条`, `共 ${total} 条`, skipped ? `跳过 ${skipped} 行` : null].filter(Boolean).join("，"),
      exportFailed: "导出失败",
      savedTo: (path) => `已保存到 ${path}`,
      savedNoPath: "已保存，但宿主未回报路径",
      importFailed: "无法读取该文件",
      saveFailed: "保存失败",
      fileExcel: "Excel 表格",
      fileText: "文本文件",
      rangeLabel: "日期范围",
      ranges: {
        "1m": "近1个月",
        "3m": "近3个月",
        "6m": "近半年",
        "1y": "近一年",
        all: "全部范围",
        custom: "自定义",
      },
      entries: (count) => `共 ${count} 条`,
      nothingInRange: "该范围内没有内容",
      export: "导出",
      chooseFile: "选择文件",
      unsupportedFile: "只支持 .xls 和 .xlsx 文件",
      noStorage: "此处无法保存备忘，页面关闭后会丢失，请先导出保存。",
      conflicts: (count) => `冲突 ${count} 条`,
      modeOverwrite: "覆盖",
      modeMerge: "合并",
      modeSkip: "跳过",
      import: "导入",
    },
  };

  let lang = $state("en");
  let text = $derived(copy[lang]);

  const now = new Date();
  let viewYear = $state(now.getFullYear());
  let viewMonth = $state(now.getMonth());

  /** date key ("YYYY-MM-DD") -> note text */
  let notes = $state({});
  let editingKey = $state(null);
  let draft = $state("");

  let menuOpen = $state(false);
  let menuEl = $state(null);
  let fileInput = $state(null);
  let status = $state("");
  let statusTimer;
  /** Set when the sidecar refuses to store notes, so the UI stops implying it did. */
  let notesError = $state("");

  let exportOpen = $state(false);
  let exportType = $state("xls");
  let exportRange = $state("all");
  let exportFrom = $state("");
  let exportTo = $state("");

  let importOpen = $state(false);
  let importFileName = $state("");
  let importRows = $state([]);
  let importSkipped = $state(0);
  let importError = $state("");
  let importMode = $state("overwrite");

  const todayKey = keyOf(now.getFullYear(), now.getMonth(), now.getDate());

  const monthTitle = $derived(
    new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en-US", {
      year: "numeric",
      month: "long",
    }).format(new Date(viewYear, viewMonth, 1)),
  );

  const weeks = $derived.by(() => buildWeeks(viewYear, viewMonth));

  /** Lunar date, solar term and holiday marker for the visible grid, resolved once per month. */
  const dayInfos = $derived.by(() => {
    const map = new Map();
    for (const week of weeks) for (const cell of week) map.set(cell.key, dayInfo(cell.key, lang));
    return map;
  });

  function keyOf(year, month, day) {
    return `${year}-${String(month + 1).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
  }

  /** Six-column week rows starting on Monday, padded with the neighbouring months' days. */
  function buildWeeks(year, month) {
    const first = new Date(year, month, 1);
    const lead = (first.getDay() + 6) % 7;
    const rows = Math.ceil((lead + new Date(year, month + 1, 0).getDate()) / 7);

    return Array.from({ length: rows }, (_, row) =>
      Array.from({ length: 7 }, (_, col) => {
        const date = new Date(year, month, 1 - lead + row * 7 + col);
        return {
          key: keyOf(date.getFullYear(), date.getMonth(), date.getDate()),
          day: date.getDate(),
          inMonth: date.getMonth() === month,
          weekend: col >= 5,
        };
      }),
    );
  }

  function step(delta) {
    const next = new Date(viewYear, viewMonth + delta, 1);
    viewYear = next.getFullYear();
    viewMonth = next.getMonth();
  }

  function goToday() {
    viewYear = now.getFullYear();
    viewMonth = now.getMonth();
  }

  function openEditor(cell) {
    editingKey = cell.key;
    draft = notes[cell.key] ?? "";
  }

  function closeEditor() {
    editingKey = null;
    draft = "";
  }

  async function saveNote() {
    await writeNote(editingKey, draft.trim());
    closeEditor();
  }

  async function removeNote() {
    await writeNote(editingKey, "");
    closeEditor();
  }

  function deleteNote(key) {
    const { [key]: _removed, ...rest } = notes;
    notes = rest;
  }

  /**
   * The sidecar owns the note set: it stores the day and answers with the whole
   * map, so the grid never diverges from what is actually persisted. An empty
   * value clears the day.
   */
  async function writeNote(key, value) {
    const result = await invoke("dbx-calendar/notes/set", { date: key, value });
    if (!result.ok) {
      // A failure leaves the grid untouched rather than showing a note that was
      // never stored.
      notesError = result.error;
      flash(`${text.saveFailed}: ${result.error}`);
      return;
    }
    notesError = "";
    notes = result.value?.notes ?? {};
  }

  /**
   * Replaces the whole note set at once, which is what an import does. Posting
   * every date separately would be one round trip per row and could leave a
   * half-applied import behind if one of them failed.
   */
  async function replaceNotes(next) {
    const result = await invoke("dbx-calendar/notes/replace", { notes: next });
    if (!result.ok) {
      notesError = result.error;
      flash(`${text.saveFailed}: ${result.error}`);
      return false;
    }
    notesError = "";
    notes = result.value?.notes ?? {};
    return true;
  }

  async function loadNotes() {
    const result = await invoke("dbx-calendar/notes/list");
    if (!result.ok) {
      notesError = result.error;
      notes = {};
      return;
    }
    notesError = "";
    notes = result.value?.notes ?? {};
  }

  /** A `data:` URI is not a thing the user can open, so only a real path is shown. */
  function displayPath(path) {
    return typeof path === "string" && !path.startsWith("data:") ? path : "";
  }

  function flash(message) {
    status = message;
    clearTimeout(statusTimer);
    statusTimer = setTimeout(() => (status = ""), 4000);
  }

  function toggleMenu() {
    menuOpen = !menuOpen;
  }

  function handleWindowClick(event) {
    if (menuOpen && menuEl && !menuEl.contains(event.target)) menuOpen = false;
  }

  function handleKeydown(event) {
    if (event.key !== "Escape") return;
    // The editor and the export dialog are modal, so they take Escape first.
    if (editingKey) closeEditor();
    else if (exportOpen) exportOpen = false;
    else if (importOpen) closeImport();
    else if (menuOpen) menuOpen = false;
  }

  /**
   * Reads the date column as it can realistically come back: the "YYYY-MM-DD"
   * text we write, a real date cell (Excel converts a typed date into one), or
   * the serial number behind such a cell. Returns null for anything else, which
   * is also what makes the header row and blank rows fall away on import.
   */
  function dateKeyOf(value) {
    if (value instanceof Date && !Number.isNaN(+value)) {
      return keyOf(value.getFullYear(), value.getMonth(), value.getDate());
    }

    if (typeof value === "number") {
      // Excel day 1 is 1900-01-01, offset by its imaginary 1900-02-29.
      const serial = new Date(Date.UTC(1899, 11, 30) + value * 86_400_000);
      return keyOf(serial.getUTCFullYear(), serial.getUTCMonth(), serial.getUTCDate());
    }

    const match = /^(\d{4})-(\d{1,2})-(\d{1,2})$/.exec(String(value ?? "").trim());
    if (!match) return null;

    const [year, month, day] = match.slice(1).map(Number);
    const date = new Date(year, month - 1, day);
    // Date rolls impossible values over (2026-02-30 becomes 2026-03-02), so
    // compare the parts back to reject them.
    return date.getFullYear() === year && date.getMonth() === month - 1 && date.getDate() === day
      ? keyOf(year, month - 1, day)
      : null;
  }

  /** Subtracts whole months, clamping to the target month's length so that
   *  31 March minus one month lands on 28/29 February instead of rolling into March. */
  function shiftMonths(date, months) {
    const shifted = new Date(date.getFullYear(), date.getMonth() - months, 1);
    const lastDay = new Date(shifted.getFullYear(), shifted.getMonth() + 1, 0).getDate();
    shifted.setDate(Math.min(date.getDate(), lastDay));
    return shifted;
  }

  function boundsFor(range) {
    if (range === "all") {
      const keys = Object.keys(notes).sort();
      return keys.length ? [keys[0], keys[keys.length - 1]] : [todayKey, todayKey];
    }
    const start = shiftMonths(now, { "1m": 1, "3m": 3, "6m": 6, "1y": 12 }[range]);
    return [keyOf(start.getFullYear(), start.getMonth(), start.getDate()), todayKey];
  }

  function openExport() {
    menuOpen = false;
    exportType = "xls";
    exportRange = "all";
    const [from, to] = boundsFor("all");
    exportFrom = from;
    exportTo = to;
    exportOpen = true;
  }

  function pickRange(range) {
    exportRange = range;
    // Custom leaves the pickers alone so they keep whatever the user last saw.
    if (range === "custom") return;
    const [from, to] = boundsFor(range);
    exportFrom = from;
    exportTo = to;
  }

  /** Notes inside the closed range, oldest first. Date keys sort chronologically
   *  as plain strings, so the comparison needs no parsing. */
  const exportRows = $derived.by(() => {
    if (!exportOpen) return [];
    return Object.keys(notes)
      .filter((key) => key >= (exportFrom || "0000-00-00") && key <= (exportTo || "9999-99-99"))
      .sort()
      .map((key) => [key, notes[key]]);
  });

  /**
   * Hands the file to the user.
   *
   * The host renders this UI in a sandboxed frame, so a plain anchor download is
   * silently dropped: the click does not throw, but no file arrives. The chain
   * below therefore tries the browser route first and falls back to the sidecar,
   * which is an ordinary process and can always write the file.
   */
  async function saveExport(blob, filename) {
    if (await saveViaBrowser(blob, filename)) return true;
    return saveViaSidecar(blob, filename);
  }

  async function saveViaBrowser(blob, filename) {
    // A save picker is the only browser download that works without the frame's
    // allow-downloads flag, because the user names the file themselves.
    if (typeof window.showSaveFilePicker === "function") {
      try {
        const handle = await window.showSaveFilePicker({ suggestedName: filename });
        const writable = await handle.createWritable();
        await writable.write(blob);
        await writable.close();
        return true;
      } catch (error) {
        // Dismissing the picker is a decision, not a failure: stop here so the
        // file is not also written to the data directory behind the user's back.
        if (error?.name === "AbortError") return true;
        console.warn("[calendar] save picker unavailable, falling back to the sidecar:", error);
      }
    }

    // The classic anchor route still works in a normal browser tab, which is how
    // the UI behaves when opened outside the host.
    try {
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = filename;
      link.click();
      // Revoking straight away can cancel the download before it starts.
      setTimeout(() => URL.revokeObjectURL(url), 10_000);
      return true;
    } catch (error) {
      console.warn("[calendar] anchor download unavailable, falling back to the sidecar:", error);
      return false;
    }
  }

  async function saveViaSidecar(blob, filename) {
    const bytes = new Uint8Array(await blob.arrayBuffer());
    // Chunked so a large workbook cannot blow the argument limit of from().
    let binary = "";
    for (let index = 0; index < bytes.length; index += 0x8000) {
      binary += String.fromCharCode(...bytes.subarray(index, index + 0x8000));
    }

    const result = await invoke("dbx-calendar/export/write", {
      name: filename,
      data: btoa(binary),
    });
    if (!result.ok) {
      notesError = result.error;
      flash(`${text.exportFailed}: ${result.error}`);
      return false;
    }

    notesError = "";
    const saved = displayPath(result.value?.path);
    flash(saved ? text.savedTo(saved) : text.savedNoPath);
    return true;
  }

  async function runExport() {
    const rows = exportRows;
    if (!rows.length) return;

    try {
      const stem = `calendar-notes-${todayKey}`;
      let blob;
      let filename;
      if (exportType === "txt") {
        // Named `body` rather than `text`: `text` is the copy table.
        const body = rows.map(([key, content]) => `${key}\t${content}`).join("\n");
        blob = new Blob([`${body}\n`], { type: "text/plain;charset=utf-8" });
        filename = `${stem}.txt`;
      } else {
        const sheet = XLSX.utils.aoa_to_sheet([[text.colDate, text.colContent], ...rows]);
        sheet["!cols"] = [{ wch: 12 }, { wch: 60 }];
        const book = XLSX.utils.book_new();
        XLSX.utils.book_append_sheet(book, sheet, text.sheetName);
        blob = new Blob([XLSX.write(book, { bookType: "xls", type: "array" })], {
          type: "application/vnd.ms-excel",
        });
        filename = `${stem}.xls`;
      }

      if (await saveExport(blob, filename)) {
        exportOpen = false;
        flash(text.exported(rows.length));
      }
    } catch (error) {
      flash(`${text.exportFailed}: ${error.message}`);
    }
  }

  function openImport() {
    menuOpen = false;
    importFileName = "";
    importRows = [];
    importSkipped = 0;
    importError = "";
    importMode = "overwrite";
    importOpen = true;
  }

  function closeImport() {
    importOpen = false;
    importFileName = "";
    importRows = [];
    importSkipped = 0;
    importError = "";
  }

  /** Reads the first sheet into [dateKey, content] pairs, dropping rows whose
   *  first cell is not a date — which is how the header and blanks fall away. */
  async function readSheet(file) {
    const book = XLSX.read(await file.arrayBuffer(), { type: "array", cellDates: true });
    const sheet = book.Sheets[book.SheetNames[0]];
    const rows = XLSX.utils.sheet_to_json(sheet, { header: 1, raw: true, defval: "" });

    const parsed = [];
    let skipped = 0;
    for (const [rawDate, rawContent] of rows) {
      const key = dateKeyOf(rawDate);
      const content = String(rawContent ?? "").trim();
      if (!key) {
        if (String(rawDate ?? "").trim() || content) skipped += 1;
        continue;
      }
      if (content) parsed.push([key, content]);
    }
    return { parsed, skipped };
  }

  /** The accept attribute only filters the picker; a user can still switch to
   *  "All files", so the extension is checked again here. */
  function isImportable(file) {
    const extension = file.name.split(".").pop()?.toLowerCase();
    return extension === "xls" || extension === "xlsx";
  }

  async function pickImportFile(event) {
    const file = event.target.files?.[0];
    // Clear the input so picking the same file twice still fires a change.
    event.target.value = "";
    if (!file) return;

    importFileName = file.name;
    importRows = [];
    importSkipped = 0;
    importMode = "overwrite";
    importError = "";

    if (!isImportable(file)) {
      importError = text.unsupportedFile;
      return;
    }

    try {
      const { parsed, skipped } = await readSheet(file);
      importRows = parsed;
      importSkipped = skipped;
    } catch (error) {
      importError = `${text.importFailed}: ${error.message}`;
    }
  }

  /** Dates the file and the calendar both have an entry for. */
  const importConflicts = $derived.by(
    () => importRows.filter(([key]) => notes[key] !== undefined).length,
  );

  async function runImport() {
    if (!importRows.length) return;

    const next = { ...notes };
    for (const [key, content] of importRows) {
      if (next[key] === undefined) {
        // Nothing to conflict with, so every mode adds it.
        next[key] = content;
        continue;
      }
      if (importMode === "skip") continue;
      next[key] = importMode === "merge" ? `${next[key]}\n${content}` : content;
    }

    const count = importRows.length;
    // Report only what the sidecar actually accepted.
    if (!(await replaceNotes(next))) return;
    closeImport();
    flash(text.imported(count, Object.keys(notes).length, importSkipped));
  }

  /**
   * Notes live in the Go sidecar, not in the page: the plugin frame runs on an
   * opaque origin, so localStorage throws on every access. The bridge is absent
   * when the page is opened directly in a browser, which leaves the calendar
   * functional but unable to store anything.
   */
  async function init() {
    if (hostLocale().toLowerCase().startsWith("zh")) lang = "zh";

    const context = await whenReady();
    const locale = context?.locale ?? hostLocale();
    if (locale?.toLowerCase().startsWith("zh")) lang = "zh";

    await loadNotes();
  }

  onMount(() => {
    void init();
  });
</script>

<svelte:head><title>DBX Calendar</title></svelte:head>
<svelte:window onkeydown={handleKeydown} onclick={handleWindowClick} />

<main>
  <header class="toolbar">
    <h1>{monthTitle}</h1>
    <div class="actions">
      <div class="menu" bind:this={menuEl}>
        <button
          type="button"
          class="menu-button"
          aria-label={text.menu}
          aria-expanded={menuOpen}
          onclick={toggleMenu}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
            <path
              d="M2 4h12M2 8h12M2 12h12"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linecap="round"
            />
          </svg>
        </button>
        {#if menuOpen}
          <div class="dropdown">
            <button type="button" onclick={openExport}>{text.export}</button>
            <button type="button" onclick={openImport}>{text.import}</button>
          </div>
        {/if}
      </div>

      <div class="nav">
        <button
          type="button"
          class="arrow"
          style={maskVar(previousIcon)}
          onclick={() => step(-1)}
          aria-label={text.prev}
        ></button>
        <button type="button" class="today" onclick={goToday}>{text.today}</button>
        <button
          type="button"
          class="arrow"
          style={maskVar(nextIcon)}
          onclick={() => step(1)}
          aria-label={text.next}
        ></button>
      </div>

      <input
        type="file"
        accept=".xls,.xlsx"
        bind:this={fileInput}
        onchange={pickImportFile}
        hidden
      />
    </div>
  </header>

  {#if notesError}
    <p class="notice">{text.noStorage}: {notesError}</p>
  {/if}

  <div
    class="grid"
    style="grid-template-rows: auto repeat({weeks.length}, minmax(0, 1fr));"
  >
    {#each text.weekdays as label, index (label)}
      <div class="weekday" class:weekend={index >= 5}>{label}</div>
    {/each}

    {#each weeks as week, weekIndex (weekIndex)}
      {#each week as cell (cell.key)}
        {@const info = dayInfos.get(cell.key)}
        <button
          type="button"
          class="cell"
          class:outside={!cell.inMonth}
          class:weekend={cell.weekend}
          class:today={cell.key === todayKey}
          aria-label="{cell.key}, {info.text}{info.status
            ? `, ${info.status === 'off' ? text.dayOff : text.workday}`
            : ''}, {notes[cell.key] ? text.hasNote : text.noNote}, {text.edit}"
          ondblclick={() => openEditor(cell)}
          onclick={(event) => {
            // A button reports detail 0 only when activated from the keyboard
            // (Enter/Space), which fires click but never dblclick.
            if (event.detail === 0) openEditor(cell);
          }}
        >
          <span class="cell-head">
            <span class="date">{cell.day}</span>
            <span class="lunar">{info.text}</span>
            {#if info.status}
              <span class="badge" class:off={info.status === "off"}>
                {info.status === "off" ? text.dayOff : text.workday}
              </span>
            {/if}
          </span>
          {#if notes[cell.key]}
            <span class="note" title={notes[cell.key]}>{notes[cell.key]}</span>
          {/if}
        </button>
      {/each}
    {/each}
  </div>
</main>

{#if status}
  <div class="status" role="status">{status}</div>
{/if}

{#if editingKey}
  <!-- 蒙版只做视觉遮罩，点击不关闭；关闭走「取消」按钮或 Esc，避免误触丢失正在编辑的内容 -->
  <div class="backdrop">
    <div
      class="editor"
      role="dialog"
      aria-modal="true"
      aria-label={text.editorTitle}
      tabindex="-1"
    >
      <div class="editor-head">
        <span class="editor-date">{editingKey}</span>
        <span class="editor-hint">{text.hint}</span>
      </div>
      <!-- svelte-ignore a11y_autofocus -->
      <textarea
        bind:value={draft}
        placeholder={text.placeholder}
        autofocus
        onkeydown={(event) => {
          if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            saveNote();
          }
        }}
      ></textarea>
      <div class="editor-actions">
        {#if notes[editingKey]}
          <button type="button" class="danger" onclick={removeNote}
            >{text.clear}</button
          >
        {/if}
        <span class="spacer"></span>
        <button type="button" class="ghost" onclick={closeEditor}
          >{text.cancel}</button
        >
        <button type="button" class="primary" onclick={saveNote}
          >{text.save}</button
        >
      </div>
    </div>
  </div>
{/if}

{#if exportOpen}
  <div class="backdrop">
    <div class="dialog" role="dialog" aria-modal="true" aria-label={text.export} tabindex="-1">
      <div class="types" role="radiogroup" aria-label={text.export}>
        <button
          type="button"
          class="type"
          class:active={exportType === "xls"}
          role="radio"
          aria-checked={exportType === "xls"}
          onclick={() => (exportType = "xls")}
        >
          <img src={xlsIcon} alt="" />
          <span>{text.fileExcel}</span>
        </button>
        <button
          type="button"
          class="type"
          class:active={exportType === "txt"}
          role="radio"
          aria-checked={exportType === "txt"}
          onclick={() => (exportType = "txt")}
        >
          <img src={txtIcon} alt="" />
          <span>{text.fileText}</span>
        </button>
      </div>

      <div class="range-inputs">
        <input
          type="date"
          aria-label={text.rangeLabel}
          bind:value={exportFrom}
          disabled={exportRange !== "custom"}
        />
        <span class="dash">–</span>
        <input
          type="date"
          aria-label={text.rangeLabel}
          bind:value={exportTo}
          disabled={exportRange !== "custom"}
        />
      </div>

      <div class="ranges" role="radiogroup" aria-label={text.rangeLabel}>
        {#each Object.entries(text.ranges) as [key, label] (key)}
          <label class:active={exportRange === key}>
            <input
              type="radio"
              name="export-range"
              value={key}
              checked={exportRange === key}
              onchange={() => pickRange(key)}
            />
            {label}
          </label>
        {/each}
      </div>

      <div class="dialog-actions">
        <span class="count">
          {#if exportRows.length}
            {text.entries(exportRows.length)}
          {:else if notesError}
            {text.nothingInRange} — {text.saveFailed}: {notesError}
          {:else}
            {text.nothingInRange}
          {/if}
        </span>
        <button type="button" class="ghost" onclick={() => (exportOpen = false)}>{text.cancel}</button>
        <button type="button" class="primary" disabled={!exportRows.length} onclick={runExport}>
          {text.export}
        </button>
      </div>
    </div>
  </div>
{/if}

{#if importOpen}
  <div class="backdrop">
    <div class="dialog" role="dialog" aria-modal="true" aria-label={text.import} tabindex="-1">
      <div class="import-icon">
        <img src={xlsIcon} alt="" />
      </div>

      <button type="button" class="file-button" onclick={() => fileInput?.click()}>
        {importFileName || text.chooseFile}
      </button>

      {#if importError}
        <p class="error">{importError}</p>
      {:else if importFileName}
        <p class="summary">
          {text.entries(importRows.length)}{importConflicts
            ? ` · ${text.conflicts(importConflicts)}`
            : ""}
        </p>

        {#if importConflicts}
          <div class="modes" role="radiogroup" aria-label={text.conflicts(importConflicts)}>
            {#each [["overwrite", text.modeOverwrite], ["merge", text.modeMerge], ["skip", text.modeSkip]] as [key, label] (key)}
              <label class:active={importMode === key}>
                <input
                  type="radio"
                  name="import-mode"
                  value={key}
                  checked={importMode === key}
                  onchange={() => (importMode = key)}
                />
                {label}
              </label>
            {/each}
          </div>
        {/if}
      {/if}

      <div class="dialog-actions">
        <button type="button" class="ghost" onclick={closeImport}>{text.cancel}</button>
        <button type="button" class="primary" disabled={!importRows.length} onclick={runImport}>
          {text.import}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  :global(*) {
    box-sizing: border-box;
  }
  :global(html),
  :global(body),
  :global(#app) {
    height: 100%;
  }
  :global(body) {
    --accent: #6d5dfc;
    --rule: color-mix(in srgb, CanvasText 14%, transparent);
    --muted: color-mix(in srgb, CanvasText 45%, transparent);
    --tint: color-mix(in srgb, CanvasText 4%, transparent);
    margin: 0;
    color: CanvasText;
    background: Canvas;
    font-family: ui-sans-serif, system-ui, "Segoe UI", "PingFang SC",
      "Microsoft YaHei", sans-serif;
    font-variant-numeric: tabular-nums;
    -webkit-font-smoothing: antialiased;
  }

  main {
    display: flex;
    flex-direction: column;
    gap: 10px;
    height: 100%;
    padding: clamp(12px, 2vw, 20px);
  }

  /* Toolbar stays quiet: the grid is the interface. */
  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 0 2px;
  }
  h1 {
    margin: 0;
    font-size: 19px;
    font-weight: 600;
    letter-spacing: -0.01em;
  }
  .nav {
    display: flex;
    gap: 4px;
  }
  .nav button {
    min-width: 32px;
    height: 30px;
    padding: 0 10px;
    border: 1px solid var(--rule);
    border-radius: 7px;
    color: inherit;
    background: transparent;
    font: inherit;
    font-size: 14px;
    line-height: 1;
    cursor: pointer;
  }
  .nav button:hover {
    background: var(--tint);
  }
  /* The arrow SVGs are filled with a fixed dark navy, which would disappear on
     a dark theme as a plain <img>. Masking them instead paints the shape with
     currentColor, so they follow the host theme. Drawn on a pseudo-element so
     the button's own hover background stays independent of the icon. */
  .nav .arrow {
    display: grid;
    place-items: center;
    padding: 0;
  }
  .nav .arrow::before {
    content: "";
    width: 13px;
    height: 13px;
    background-color: currentColor;
    -webkit-mask: var(--icon) center / contain no-repeat;
    mask: var(--icon) center / contain no-repeat;
  }
  .nav .today {
    font-size: 12.5px;
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .notice {
    margin: 0;
    padding: 7px 10px;
    border: 1px solid color-mix(in srgb, #e5484d 40%, transparent);
    border-radius: 8px;
    color: color-mix(in srgb, #e5484d 80%, CanvasText);
    font-size: 12px;
    line-height: 1.5;
  }

  .menu {
    position: relative;
  }
  .menu-button {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    padding: 0;
    border: 1px solid var(--rule);
    border-radius: 7px;
    color: inherit;
    background: transparent;
    cursor: pointer;
  }
  .menu-button:hover {
    background: var(--tint);
  }
  .menu-button svg {
    width: 15px;
    height: 15px;
  }

  /* Anchored to the button's right edge so it opens leftward, away from the
     panel edge. */
  .dropdown {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    z-index: 10;
    display: flex;
    flex-direction: column;
    min-width: 150px;
    padding: 4px;
    border: 1px solid var(--rule);
    border-radius: 10px;
    background: Canvas;
    box-shadow: 0 12px 30px rgba(0, 0, 0, 0.18);
  }
  .dropdown button {
    padding: 7px 10px;
    border: 0;
    border-radius: 6px;
    color: inherit;
    background: transparent;
    font: inherit;
    font-size: 13px;
    text-align: left;
    white-space: nowrap;
    cursor: pointer;
  }
  .dropdown button:hover {
    background: var(--tint);
  }

  /* Floating so that reporting a result never shifts the grid. */
  .status {
    position: fixed;
    bottom: 18px;
    left: 50%;
    z-index: 20;
    padding: 8px 14px;
    transform: translateX(-50%);
    border-radius: 8px;
    color: Canvas;
    background: color-mix(in srgb, CanvasText 85%, Canvas);
    font-size: 12.5px;
  }

  .grid {
    display: grid;
    flex: 1;
    min-height: 0;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    gap: 1px;
    overflow: hidden;
    border: 1px solid var(--rule);
    border-radius: 10px;
    background: var(--rule);
  }

  .weekday {
    padding: 7px 10px;
    background: Canvas;
    color: var(--muted);
    font-size: 11.5px;
    font-weight: 500;
  }

  .cell {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-height: 0;
    padding: 7px 9px;
    overflow: hidden;
    border: 0;
    border-radius: 0;
    background: Canvas;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    /* Double-click is the edit gesture, so suppress the text selection it would otherwise make. */
    user-select: none;
  }
  .cell:hover {
    background: color-mix(in srgb, var(--accent) 6%, Canvas);
  }
  .cell:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  /* Days from the neighbouring months recede so the month reads as one block. */
  .cell.outside {
    color: color-mix(in srgb, CanvasText 35%, transparent);
  }
  .cell.weekend {
    background: color-mix(in srgb, var(--accent) 5%, Canvas);
  }
  .cell.weekend:hover {
    background: color-mix(in srgb, var(--accent) 10%, Canvas);
  }
  /* Today is the one cell allowed to break the hairline grid, so it carries a
     solid ring plus a tint. Drawn inset because the 1px gaps leave no room for
     a real border without shifting the row, and left square-cornered because
     rounding would expose the rule colour behind the cell's own background. */
  .cell.today {
    background: color-mix(in srgb, var(--accent) 11%, Canvas);
    box-shadow: inset 0 0 0 2px var(--accent);
  }
  .cell.today:hover {
    background: color-mix(in srgb, var(--accent) 16%, Canvas);
  }

  .cell-head {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
  }

  .date {
    display: grid;
    place-items: center;
    min-width: 20px;
    height: 20px;
    padding: 0 4px;
    border-radius: 5px;
    font-size: 12px;
    font-weight: 500;
  }

  /* Lunar date, solar term or festival name, sitting right of the day number. */
  .lunar {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--muted);
    font-size: 11px;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .cell.outside .lunar {
    color: color-mix(in srgb, CanvasText 25%, transparent);
  }

  /* 休 / 班 marker. Rendered only for years with a published holiday schedule. */
  .badge {
    flex: none;
    display: grid;
    place-items: center;
    min-width: 15px;
    height: 15px;
    padding: 0 3px;
    border-radius: 4px;
    color: CanvasText;
    background: color-mix(in srgb, CanvasText 16%, Canvas);
    font-size: 10px;
    font-weight: 600;
  }
  .badge.off {
    color: #fff;
    background: #e5484d;
  }
  .cell.today .date {
    background: var(--accent);
    color: #fff;
  }

  .note {
    overflow: hidden;
    font-size: 12px;
    line-height: 1.45;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    opacity: 0.78;
    display: -webkit-box;
    -webkit-line-clamp: 4;
    line-clamp: 4;
    -webkit-box-orient: vertical;
  }

  .backdrop {
    position: fixed;
    inset: 0;
    display: grid;
    place-items: center;
    padding: 20px;
    background: color-mix(in srgb, #000 45%, transparent);
  }
  .editor {
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: min(460px, 100%);
    padding: 16px;
    border: 1px solid var(--rule);
    border-radius: 14px;
    background: Canvas;
    box-shadow: 0 18px 48px rgba(0, 0, 0, 0.3);
  }
  .editor-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
  }
  .editor-date {
    font-size: 14px;
    font-weight: 600;
  }
  .editor-hint {
    color: var(--muted);
    font-size: 11.5px;
  }
  textarea {
    min-height: 150px;
    padding: 10px 12px;
    resize: vertical;
    border: 1px solid var(--rule);
    border-radius: 9px;
    color: inherit;
    background: color-mix(in srgb, CanvasText 3%, Canvas);
    font: inherit;
    font-size: 13.5px;
    line-height: 1.55;
  }
  textarea:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }
  .editor-actions,
  .dialog-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  /* Keeps cancel/confirm on the right in both dialogs. The export dialog has a
     leading count, but the import one does not, so it needs this explicitly. */
  .dialog-actions {
    justify-content: flex-end;
  }
  .spacer {
    flex: 1;
  }
  .editor-actions button,
  .dialog-actions button {
    height: 32px;
    padding: 0 14px;
    border: 1px solid transparent;
    border-radius: 8px;
    font: inherit;
    font-size: 13px;
    cursor: pointer;
  }
  .editor-actions .primary,
  .dialog-actions .primary {
    color: #fff;
    background: var(--accent);
  }
  .dialog-actions .primary:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .editor-actions .ghost,
  .editor-actions .danger,
  .dialog-actions .ghost {
    border-color: var(--rule);
    color: inherit;
    background: transparent;
  }
  .editor-actions .ghost:hover,
  .editor-actions .danger:hover,
  .dialog-actions .ghost:hover {
    background: var(--tint);
  }
  .editor-actions .danger {
    color: color-mix(in srgb, #e5484d 80%, CanvasText);
  }

  /* Export dialog */
  .dialog {
    display: flex;
    flex-direction: column;
    gap: 14px;
    width: min(420px, 100%);
    padding: 16px;
    border: 1px solid var(--rule);
    border-radius: 14px;
    background: Canvas;
    box-shadow: 0 18px 48px rgba(0, 0, 0, 0.3);
  }

  .types {
    display: flex;
    gap: 10px;
  }
  .type {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 12px 8px;
    border: 1px solid var(--rule);
    border-radius: 10px;
    color: inherit;
    background: transparent;
    font: inherit;
    font-size: 12.5px;
    cursor: pointer;
  }
  .type img {
    width: 32px;
    height: 32px;
  }
  .type:hover {
    background: var(--tint);
  }
  .type.active {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 8%, Canvas);
  }

  .range-inputs {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .range-inputs input {
    flex: 1;
    min-width: 0;
    height: 32px;
    padding: 0 8px;
    border: 1px solid var(--rule);
    border-radius: 8px;
    color: inherit;
    background: color-mix(in srgb, CanvasText 3%, Canvas);
    font: inherit;
    font-size: 12.5px;
  }
  .range-inputs input:disabled {
    opacity: 0.55;
  }
  .dash {
    color: var(--muted);
  }

  .ranges,
  .modes {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .ranges label,
  .modes label {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 5px 11px;
    border: 1px solid var(--rule);
    border-radius: 20px;
    font-size: 12.5px;
    cursor: pointer;
  }
  .ranges label:hover,
  .modes label:hover {
    background: var(--tint);
  }
  .ranges label.active,
  .modes label.active {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 8%, Canvas);
  }
  .ranges input,
  .modes input {
    margin: 0;
    accent-color: var(--accent);
  }

  /* Import dialog */
  .import-icon {
    display: grid;
    place-items: center;
  }
  .import-icon img {
    width: 44px;
    height: 44px;
  }
  .file-button {
    height: 34px;
    padding: 0 12px;
    overflow: hidden;
    border: 1px solid var(--rule);
    border-radius: 8px;
    color: inherit;
    background: transparent;
    font: inherit;
    font-size: 12.5px;
    white-space: nowrap;
    text-overflow: ellipsis;
    cursor: pointer;
  }
  .file-button:hover {
    background: var(--tint);
  }
  .summary,
  .error {
    margin: 0;
    font-size: 12.5px;
  }
  .summary {
    color: var(--muted);
  }
  .error {
    color: color-mix(in srgb, #e5484d 80%, CanvasText);
  }

  .count {
    margin-right: auto;
    color: var(--muted);
    font-size: 12px;
  }

  @media (prefers-reduced-motion: reduce) {
    * {
      transition: none !important;
    }
  }
</style>

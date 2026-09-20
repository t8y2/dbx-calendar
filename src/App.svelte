<script>
  import { onMount } from "svelte";
  import { dayInfo } from "./lunar.js";

  const STORAGE_KEY = "dbx-calendar.notes.v1";

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

  function saveNote() {
    const value = draft.trim();
    if (value) notes = { ...notes, [editingKey]: value };
    else deleteNote(editingKey);
    persist();
    closeEditor();
  }

  function removeNote() {
    deleteNote(editingKey);
    persist();
    closeEditor();
  }

  function deleteNote(key) {
    const { [key]: _removed, ...rest } = notes;
    notes = rest;
  }

  function persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(notes));
    } catch {
      // Sandboxed host without storage access: notes stay in memory for this session.
    }
  }

  function handleKeydown(event) {
    if (event.key === "Escape" && editingKey) closeEditor();
  }

  onMount(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored) notes = JSON.parse(stored) ?? {};
    } catch {
      notes = {};
    }

    // The DBX bridge is absent when the page is opened directly in a browser.
    const host = window.dbxPlugin;
    if (!host) return;
    if (host.locale?.toLowerCase().startsWith("zh")) lang = "zh";
    host.ready?.then((context) => {
      const locale = context?.locale ?? host.locale;
      if (locale?.toLowerCase().startsWith("zh")) lang = "zh";
    });
  });
</script>

<svelte:head><title>DBX Calendar</title></svelte:head>
<svelte:window onkeydown={handleKeydown} />

<main>
  <header class="toolbar">
    <h1>{monthTitle}</h1>
    <div class="nav">
      <button type="button" onclick={() => step(-1)} aria-label={text.prev}
        >‹</button
      >
      <button type="button" class="today" onclick={goToday}>{text.today}</button
      >
      <button type="button" onclick={() => step(1)} aria-label={text.next}
        >›</button
      >
    </div>
  </header>

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
    align-items: baseline;
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
  .nav .today {
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
  .editor-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .spacer {
    flex: 1;
  }
  .editor-actions button {
    height: 32px;
    padding: 0 14px;
    border: 1px solid transparent;
    border-radius: 8px;
    font: inherit;
    font-size: 13px;
    cursor: pointer;
  }
  .editor-actions .primary {
    color: #fff;
    background: var(--accent);
  }
  .editor-actions .ghost,
  .editor-actions .danger {
    border-color: var(--rule);
    color: inherit;
    background: transparent;
  }
  .editor-actions .ghost:hover,
  .editor-actions .danger:hover {
    background: var(--tint);
  }
  .editor-actions .danger {
    color: color-mix(in srgb, #e5484d 80%, CanvasText);
  }

  @media (prefers-reduced-motion: reduce) {
    * {
      transition: none !important;
    }
  }
</style>

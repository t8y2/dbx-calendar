/**
 * Holiday schedules bundled from json/holiday-<year>.json.
 *
 * The npm library only carries the published schedule up to 2026, so later
 * years come from these files — they can be added or corrected by hand, with
 * no dependency upgrade and no code change. Vite inlines every match at build
 * time, which keeps the packaged plugin self-contained and avoids runtime file
 * access from inside the sandbox.
 *
 * Entry shape, one file per year, keyed by "MM-DD":
 *   { holiday: true,  name: "春节" }                    // a day off
 *   { holiday: false, name: "春节后补班", target: "春节" } // a makeup workday
 */
const calendars = import.meta.glob("../json/holiday-*.json", { eager: true, import: "default" });

const byDate = new Map();

for (const [path, data] of Object.entries(calendars)) {
  const year = path.match(/(\d{4})\.json$/)?.[1];
  if (!year) continue;
  for (const [monthDay, entry] of Object.entries(data?.holiday ?? {})) {
    byDate.set(`${year}-${monthDay}`, entry);
  }
}

/** The bundled schedule entry for a date, or null when none is on file. */
export function bundledHoliday(dateKey) {
  return byDate.get(dateKey) ?? null;
}

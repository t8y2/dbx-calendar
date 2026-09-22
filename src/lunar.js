import { getDayDetail, getLunarDate, getLunarFestivals, getSolarTerms } from "chinese-days";

/**
 * The library ships roughly 70 lunar festivals a year, most of them almanac
 * trivia (官家送灶, 接玉皇, 封井, 祭井神…), and stacks up to five names on a
 * single day. Only the widely recognised festivals belong on a calendar cell.
 */
const LUNAR_FESTIVALS = new Set([
  "除夕",
  "春节",
  "元宵节",
  "龙头节",
  "端午节",
  "七夕节",
  "中元节",
  "中秋节",
  "重阳节",
  "腊八节",
  "小年",
]);

const EN_FESTIVALS = {
  除夕: "Lunar New Year's Eve",
  春节: "Lunar New Year",
  元宵节: "Lantern Festival",
  龙头节: "Dragon Head Day",
  端午节: "Dragon Boat Festival",
  七夕节: "Qixi Festival",
  中元节: "Ghost Festival",
  中秋节: "Mid-Autumn Festival",
  重阳节: "Double Ninth Festival",
  腊八节: "Laba Festival",
  小年: "Little New Year",
};

/** Standard translations of the 24 solar terms, kept short enough for a cell. */
const EN_TERMS = {
  小寒: "Minor Cold",
  大寒: "Major Cold",
  立春: "Start of Spring",
  雨水: "Rain Water",
  惊蛰: "Insects Awaken",
  春分: "Spring Equinox",
  清明: "Pure Brightness",
  谷雨: "Grain Rain",
  立夏: "Start of Summer",
  小满: "Grain Buds",
  芒种: "Grain in Ear",
  夏至: "Summer Solstice",
  小暑: "Minor Heat",
  大暑: "Major Heat",
  立秋: "Start of Autumn",
  处暑: "End of Heat",
  白露: "White Dew",
  秋分: "Autumn Equinox",
  寒露: "Cold Dew",
  霜降: "Frost's Descent",
  立冬: "Start of Winter",
  小雪: "Minor Snow",
  大雪: "Major Snow",
  冬至: "Winter Solstice",
};

const EN_HOLIDAYS = {
  元旦: "New Year's Day",
  劳动节: "Labour Day",
  国庆节: "National Day",
  清明节: "Tomb-Sweeping Day",
};

const years = new Map();

/**
 * A holiday period runs for days but names only one day. Both data sources label
 * every day of the window with the same string, so 国庆节 would otherwise
 * announce itself across all nine days. These three are the fixed-date holidays
 * whose day can be matched by month and day alone; 春节, 端午, 中秋 and 清明 need
 * no entry because the lunar-festival and solar-term lookups already name their
 * day, and would otherwise repeat the label across the whole window too.
 */
const FIXED_HOLIDAYS = { 元旦: "01-01", 劳动节: "05-01", 国庆节: "10-01" };

/**
 * Festivals and solar terms are computed, so they cover any year. The statutory
 * holiday schedule is not a computation but a published document, and the npm
 * library carries it only up to 2026.
 *
 * 元旦 and 国庆 are statutory holidays every year, so their presence is a cheap,
 * dependable probe for whether the library actually carries this year's
 * schedule — it reports nothing for years it has no data for. Verified against
 * a full 365-day scan for 2018–2035: identical verdict.
 *
 * For a year the library does not carry, the calendar shows lunar dates and
 * solar terms only, with no 休 / 班 markers. There is deliberately no fallback
 * data source: a year without a schedule simply has none.
 */
function yearIndex(year) {
  const cached = years.get(year);
  if (cached) return cached;

  const festivals = new Map();
  for (const { date, name } of getLunarFestivals(`${year}-01-01`, `${year}-12-31`)) {
    const festival = name.find((entry) => LUNAR_FESTIVALS.has(entry));
    if (festival) festivals.set(date, festival);
  }

  const terms = new Map();
  for (const { date, name } of getSolarTerms(`${year}-01-01`, `${year}-12-31`)) {
    terms.set(date, name);
  }

  const hasSchedule = [`${year}-01-01`, `${year}-10-01`].some((key) =>
    getDayDetail(key)?.name?.includes(","),
  );

  const result = { festivals, terms, hasSchedule };
  years.set(year, result);
  return result;
}

/**
 * The published schedule for a date, or null when the library carries no
 * schedule for that year or the day is ordinary, so a plain weekend is never
 * mistaken for a holiday.
 */
function scheduleFor(dateKey) {
  if (!yearIndex(Number(dateKey.slice(0, 4))).hasSchedule) return null;

  // A holiday reads "National Day,国庆节,3"; a plain weekend reads "Saturday".
  const detail = getDayDetail(dateKey);
  if (!detail?.name?.includes(",")) return null;
  return { name: detail.name.split(",")[1], off: !detail.work };
}

function ordinal(value) {
  const tens = value % 100;
  // 11, 12 and 13 take "th" despite ending in 1, 2 and 3.
  if (tens >= 11 && tens <= 13) return `${value}th`;
  return `${value}${["th", "st", "nd", "rd"][value % 10] ?? "th"}`;
}

/**
 * What to show beside a day number, plus whether that day is a day off or a
 * makeup workday.
 *
 * Priority: lunar festival > statutory holiday > solar term > plain lunar date.
 * A makeup workday (调休) keeps its lunar date rather than borrowing the holiday
 * name — 2026-09-20 is a Sunday worked for 国庆节, and labelling it 国庆节 would
 * read as if the holiday fell on that day.
 */
export function dayInfo(dateKey, lang = "zh") {
  const en = lang === "en";
  const { festivals, terms } = yearIndex(Number(dateKey.slice(0, 4)));

  const schedule = scheduleFor(dateKey);
  // Name only the day the holiday falls on; the rest of the window keeps its
  // lunar date so the cell still says something specific about that day.
  const holiday =
    schedule?.off && FIXED_HOLIDAYS[schedule.name] === dateKey.slice(5) ? schedule.name : null;

  const festival = festivals.get(dateKey);
  const term = terms.get(dateKey);

  let text;
  if (festival) text = en ? (EN_FESTIVALS[festival] ?? festival) : festival;
  else if (holiday) text = en ? (EN_HOLIDAYS[holiday] ?? holiday) : holiday;
  else if (term) text = en ? (EN_TERMS[term] ?? term) : term;
  else {
    const { lunarDay, lunarDayCN, lunarMon, lunarMonCN, isLeap } = getLunarDate(dateKey);
    if (en) {
      text = lunarDay === 1 ? `${isLeap ? "Leap " : ""}${ordinal(lunarMon)} Moon` : ordinal(lunarDay);
    } else {
      // The library reports a leap month as plain 六月, which would collide with
      // the ordinary sixth month earlier in the year.
      text = lunarDay === 1 ? (isLeap ? `闰${lunarMonCN}` : lunarMonCN) : lunarDayCN;
    }
  }

  return { text, status: schedule ? (schedule.off ? "off" : "work") : null };
}

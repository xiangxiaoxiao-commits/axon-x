// Relative time formatting for archive results.
// Timestamps are unix milliseconds (number).

const MINUTE = 60 * 1000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;
const MONTH = 30 * DAY;
const YEAR = 365 * DAY;

// relativeTime returns a compact Chinese relative label like "2天前".
export function relativeTime(ms: number, now: number = Date.now()): string {
  if (!ms || ms <= 0) return "";
  const diff = now - ms;
  if (diff < 0) return "刚刚";
  if (diff < MINUTE) return "刚刚";
  if (diff < HOUR) return `${Math.floor(diff / MINUTE)}分钟前`;
  if (diff < DAY) return `${Math.floor(diff / HOUR)}小时前`;
  if (diff < WEEK) return `${Math.floor(diff / DAY)}天前`;
  if (diff < MONTH) return `${Math.floor(diff / WEEK)}周前`;
  if (diff < YEAR) return `${Math.floor(diff / MONTH)}个月前`;
  return `${Math.floor(diff / YEAR)}年前`;
}

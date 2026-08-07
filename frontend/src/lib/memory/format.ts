// Helpers for the Memory view. Timestamps from the backend are unix milliseconds.

// Normalize a timestamp that may be in seconds or milliseconds to ms.
// Mirrors the tolerance used by SessionList so mixed sources render consistently.
export function toMs(ts: number): number {
  return ts < 1e12 ? ts * 1000 : ts;
}

// Compact relative time in Chinese, e.g. "刚刚" / "3 分钟前" / "2 天前".
export function relativeTime(ts: number): string {
  const ms = toMs(ts);
  if (!ms) return "";
  const diff = Date.now() - ms;
  if (diff < 60_000) return "刚刚";
  const min = Math.floor(diff / 60_000);
  if (min < 60) return `${min} 分钟前`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr} 小时前`;
  const day = Math.floor(hr / 24);
  if (day < 30) return `${day} 天前`;
  const mon = Math.floor(day / 30);
  if (mon < 12) return `${mon} 个月前`;
  return `${Math.floor(mon / 12)} 年前`;
}

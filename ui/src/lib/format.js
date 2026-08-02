/** Formatting helpers, ported from the previous vanilla UI so numbers, sizes
 * and durations read identically to before. */

export function fmtBytes(n) {
  n = Number(n) || 0;
  if (n < 1024) return n + " B";
  const u = ["KB", "MB", "GB", "TB", "PB"];
  let i = -1;
  do {
    n /= 1024;
    i++;
  } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(n < 10 ? 2 : 1) + " " + u[i];
}

export function fmtDur(sec) {
  sec = Number(sec) || 0;
  if (sec < 60) return sec.toFixed(1) + "s";
  const m = Math.floor(sec / 60);
  const s = Math.round(sec % 60);
  if (m < 60) return m + "m " + s + "s";
  return Math.floor(m / 60) + "h " + (m % 60) + "m";
}

export function fmtTime(iso) {
  if (!iso) return "—";
  const d = new Date(iso);
  return isNaN(d.getTime()) ? String(iso) : d.toLocaleString();
}

/** fmtRelative renders "4m ago" / "in 3s", falling back to a date past a week. */
export function fmtRelative(iso) {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return String(iso);
  const diff = (Date.now() - d.getTime()) / 1000;
  const abs = Math.abs(diff);
  if (abs < 10) return "just now";
  if (abs < 60) return Math.round(abs) + "s ago";
  if (abs < 3600) return Math.round(abs / 60) + "m ago";
  if (abs < 86400) return Math.round(abs / 3600) + "h ago";
  if (abs < 7 * 86400) return Math.round(abs / 86400) + "d ago";
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

/** runDuration is how long a run took, or has been running so far. */
export function runDuration(run) {
  if (!run || !run.startedAt) return "—";
  const start = new Date(run.startedAt).getTime();
  const end = run.finishedAt ? new Date(run.finishedAt).getTime() : Date.now();
  return fmtDur(Math.max(0, (end - start) / 1000));
}

export function shortId(id) {
  return id ? String(id).slice(0, 8) : "";
}

export function fmtCount(n) {
  return (Number(n) || 0).toLocaleString();
}

/** Where a repository actually stores data, for display in lists. */
export function repoLocation(repo) {
  if (!repo) return "";
  return repo.backendType === "S3"
    ? [repo.endpoint, repo.bucket].filter(Boolean).join("/")
    : repo.localPath || "";
}

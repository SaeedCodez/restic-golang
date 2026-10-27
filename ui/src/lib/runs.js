/** Shared vocabulary for run status and kind: one place decides what a status
 * is called, which colour it wears and whether it counts as active. */

export const ACTIVE_STATUSES = new Set(["starting", "running"]);

export function isActive(status) {
  return ACTIVE_STATUSES.has(status);
}

export const STATUS_LABEL = {
  starting: "Starting",
  running: "Running",
  success: "Success",
  success_warnings: "Warnings",
  failed: "Failed",
  canceled: "Stopped",
  interrupted: "Interrupted",
};

/** Badge variant per status — success/running/warning/destructive/secondary. */
export const STATUS_VARIANT = {
  starting: "running",
  running: "running",
  success: "success",
  success_warnings: "warning",
  failed: "destructive",
  interrupted: "destructive",
  canceled: "secondary",
};

/** Solid fill per status, for dots, bars and the run strip. */
export const STATUS_FILL = {
  starting: "bg-running",
  running: "bg-running",
  success: "bg-success",
  success_warnings: "bg-warning",
  failed: "bg-destructive",
  interrupted: "bg-destructive",
  canceled: "bg-muted-foreground/45",
};

/** Text colour per status, matching STATUS_FILL. */
export const STATUS_TEXT = {
  starting: "text-running",
  running: "text-running",
  success: "text-success",
  success_warnings: "text-warning",
  failed: "text-destructive",
  interrupted: "text-destructive",
  canceled: "text-muted-foreground",
};

/** Phase text shown next to the progress bar on the run screen. */
export const STATUS_PHASE = {
  starting: "Starting…",
  running: "Running…",
  success: "Complete",
  success_warnings: "Complete, with warnings",
  failed: "Failed",
  canceled: "Stopped",
  interrupted: "Interrupted",
};

export const KIND_LABEL = {
  backup: "Backup",
  restore: "Restore",
  init: "Initialize",
  download: "Download",
  unlock: "Unlock",
  retention: "Retention",
  forget: "Forget",
  check: "Check",
  restore_db: "Database restore",
  speedtest: "Speed test",
};

export function statusLabel(status) {
  return STATUS_LABEL[status] || status || "—";
}

export function kindLabel(kind) {
  return KIND_LABEL[kind] || kind || "—";
}

/** A finished-successfully run reads 100%, even though restic's last status
 * tick before the summary is usually a little short of it. */
export function displayPercent(run) {
  if (!run) return 0;
  if (run.status === "success" || run.status === "success_warnings") return 100;
  const p = run.progress ? Number(run.progress.percent) || 0 : 0;
  return Math.max(0, Math.min(100, p * 100));
}

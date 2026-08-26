/** Helpers for job automatic-backup schedules (API JobSchedule + derived fields). */

export const WEEKDAYS = [
  { value: 0, label: "Sun", short: "S" },
  { value: 1, label: "Mon", short: "M" },
  { value: 2, label: "Tue", short: "T" },
  { value: 3, label: "Wed", short: "W" },
  { value: 4, label: "Thu", short: "T" },
  { value: 5, label: "Fri", short: "F" },
  { value: 6, label: "Sat", short: "S" },
];

/** Human label matching the Go JobSchedule.Describe(). */
export function describeSchedule(schedule) {
  if (!schedule) return "";
  switch (schedule.kind) {
    case "hourly":
      return "hourly";
    case "every":
      return "every " + (schedule.every || "").trim();
    case "daily":
      return "daily at " + (schedule.at || "").trim();
    case "weekly": {
      const names = WEEKDAYS.map((d) => d.label);
      const parts = (schedule.weekdays || [])
        .filter((d) => d >= 0 && d < names.length)
        .map((d) => names[d]);
      return "weekly on " + parts.join(", ") + " at " + (schedule.at || "").trim();
    }
    default:
      return schedule.kind || "";
  }
}

/** Capitalized one-liner for cards, e.g. "Daily at 02:00". */
export function describeScheduleTitle(schedule) {
  const d = describeSchedule(schedule);
  if (!d) return "";
  return d.charAt(0).toUpperCase() + d.slice(1);
}

/**
 * Default editable form state for a job's schedule. `enabled` false keeps the
 * last chosen kind/fields so pausing does not wipe the configuration.
 */
export function scheduleFormFromJob(job) {
  const s = job?.schedule;
  return {
    enabled: Boolean(s?.enabled),
    kind: s?.kind || "daily",
    every: s?.every || "6h",
    at: s?.at || "02:00",
    weekdays: Array.isArray(s?.weekdays) && s.weekdays.length ? [...s.weekdays] : [1],
  };
}

/** Build the API schedule payload (or null when never configured / cleared). */
export function schedulePayload(form) {
  if (!form) return null;
  const kind = form.kind || "daily";
  const base = { enabled: Boolean(form.enabled), kind };
  if (kind === "every") {
    return { ...base, every: (form.every || "6h").trim() };
  }
  if (kind === "daily") {
    return { ...base, at: (form.at || "02:00").trim() };
  }
  if (kind === "weekly") {
    const weekdays = [...(form.weekdays || [])].filter((d) => d >= 0 && d <= 6).sort((a, b) => a - b);
    return { ...base, at: (form.at || "02:00").trim(), weekdays };
  }
  return base; // hourly
}

/** Helpers for job snapshot retention (API JobRetention + presets). */

export const RETENTION_PRESETS = [
  {
    id: "light",
    label: "Light",
    description: "Last 24 snapshots and 7 dailies",
    values: { keepLast: 24, keepHourly: 0, keepDaily: 7, keepWeekly: 0, keepMonthly: 0, keepWithinDays: 0 },
  },
  {
    id: "balanced",
    label: "Balanced",
    description: "Last 24, 7 daily, 4 weekly — good for hourly jobs",
    values: { keepLast: 24, keepHourly: 0, keepDaily: 7, keepWeekly: 4, keepMonthly: 0, keepWithinDays: 0 },
  },
  {
    id: "long",
    label: "Long",
    description: "Last 48, 14 daily, 8 weekly, 12 monthly",
    values: { keepLast: 48, keepHourly: 0, keepDaily: 14, keepWeekly: 8, keepMonthly: 12, keepWithinDays: 0 },
  },
  {
    id: "custom",
    label: "Custom",
    description: "Set keep counts yourself",
    values: null,
  },
];

const emptyCounts = {
  keepLast: 0,
  keepHourly: 0,
  keepDaily: 0,
  keepWeekly: 0,
  keepMonthly: 0,
  keepWithinDays: 0,
};

function countsFrom(r) {
  return {
    keepLast: Number(r?.keepLast) || 0,
    keepHourly: Number(r?.keepHourly) || 0,
    keepDaily: Number(r?.keepDaily) || 0,
    keepWeekly: Number(r?.keepWeekly) || 0,
    keepMonthly: Number(r?.keepMonthly) || 0,
    keepWithinDays: Number(r?.keepWithinDays) || 0,
  };
}

/** Default editable form state. Pausing keeps the last chosen preset/counts. */
export function retentionFormFromJob(job) {
  const r = job?.retention;
  const preset = r?.preset || "balanced";
  const presetDef = RETENTION_PRESETS.find((p) => p.id === preset);
  const fromPreset = presetDef?.values;
  const counts =
    preset !== "custom" && fromPreset
      ? { ...fromPreset }
      : r
        ? countsFrom(r)
        : { ...RETENTION_PRESETS.find((p) => p.id === "balanced").values };
  return {
    enabled: Boolean(r?.enabled),
    preset,
    ...counts,
  };
}

/** Build the API retention payload (or null when never configured). */
export function retentionPayload(form) {
  if (!form) return null;
  const preset = form.preset || "custom";
  const presetDef = RETENTION_PRESETS.find((p) => p.id === preset);
  let counts = countsFrom(form);
  if (preset !== "custom" && presetDef?.values) {
    counts = { ...presetDef.values };
  }
  return {
    enabled: Boolean(form.enabled),
    preset,
    ...counts,
  };
}

/** Short human label matching Go JobRetention.Describe(). */
export function describeRetention(retention) {
  if (!retention) return "";
  const parts = [];
  if (retention.keepLast > 0) parts.push(`last ${retention.keepLast}`);
  if (retention.keepHourly > 0) parts.push(`${retention.keepHourly} hourly`);
  if (retention.keepDaily > 0) parts.push(`${retention.keepDaily} daily`);
  if (retention.keepWeekly > 0) parts.push(`${retention.keepWeekly} weekly`);
  if (retention.keepMonthly > 0) parts.push(`${retention.keepMonthly} monthly`);
  if (retention.keepWithinDays > 0) parts.push(`within ${retention.keepWithinDays}d`);
  return parts.join(", ");
}

export function describeRetentionTitle(retention) {
  const d = describeRetention(retention);
  if (!d) return "";
  return d.charAt(0).toUpperCase() + d.slice(1);
}

export { emptyCounts };

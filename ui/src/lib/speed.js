import { fmtBytes } from "@/lib/format";

/** Bytes per second, in the same units as every size in the app. */
export function fmtSpeed(bps) {
  return `${fmtBytes(Math.round(Number(bps) || 0))}/s`;
}

/** The score bands the server rates with ("Excellent" … "Very slow"), as tones. */
export function speedTone(score) {
  if (score >= 60) return "success";
  if (score >= 40) return "warning";
  return "destructive";
}

/** About how long moving `bytes` takes at a measured speed, overhead included. */
export function estimateSeconds(bytes, bps, overheadSeconds) {
  if (!bps) return null;
  return (Number(overheadSeconds) || 0) + bytes / bps;
}

/** A duration estimate in round words: "about 40 s", "about 3 min", "about 1.5 h". */
export function fmtEstimate(sec) {
  if (sec == null) return "—";
  if (sec < 60) return `about ${Math.max(1, Math.round(sec))} s`;
  if (sec < 3600) return `about ${Math.round(sec / 60)} min`;
  const h = sec / 3600;
  return `about ${h < 10 ? h.toFixed(1) : Math.round(h)} h`;
}

/** One line for a speed test result in run lists. */
export function speedSummary(sp) {
  if (!sp) return null;
  return `Score ${sp.score} · ${sp.rating} · up ${fmtSpeed(sp.uploadBytesPerSec)} · down ${fmtSpeed(sp.downloadBytesPerSec)}`;
}

import * as React from "react";
import { Status } from "@/lib/api";
import { isActive } from "@/lib/runs";

/**
 * Live state shared by the whole app, backed by one global SSE connection.
 *
 * The server sends a snapshot of every active run immediately after we
 * subscribe, then a `{type:"run"}` event on each run-level state change. So the
 * badge and the lists are seeded from the stream itself and stay correct across
 * refreshes, second tabs and server restarts — the browser never holds state the
 * server cannot rebuild.
 */
const LiveContext = React.createContext({
  status: { resticInstalled: false, resticVersion: "" },
  activeRuns: [],
  runsVersion: 0,
  refreshStatus: () => {},
});

export function LiveProvider({ children }) {
  const [status, setStatus] = React.useState({ resticInstalled: false, resticVersion: "" });
  const [activeRuns, setActiveRuns] = React.useState([]);
  // Bumped on every run-level event so list screens can refetch themselves.
  const [runsVersion, setRunsVersion] = React.useState(0);

  const refreshStatus = React.useCallback(async () => {
    const r = await Status.get();
    if (r.ok) {
      setStatus({
        resticInstalled: !!r.body.resticInstalled,
        resticVersion: r.body.resticVersion || "",
      });
    }
  }, []);

  React.useEffect(() => {
    refreshStatus();
  }, [refreshStatus]);

  React.useEffect(() => {
    const es = new EventSource("/api/events");
    es.onmessage = (e) => {
      let ev;
      try {
        ev = JSON.parse(e.data);
      } catch {
        return;
      }
      if (ev.type !== "run" || !ev.run) return;
      const run = ev.run;
      setActiveRuns((prev) => {
        const rest = prev.filter((r) => r.id !== run.id);
        return isActive(run.status) ? [...rest, run] : rest;
      });
      setRunsVersion((v) => v + 1);
    };
    // EventSource reconnects on its own, and the server re-sends the active-run
    // snapshot on each connect, so a dropped connection self-heals.
    return () => es.close();
  }, []);

  const value = React.useMemo(
    () => ({ status, activeRuns, runsVersion, refreshStatus }),
    [status, activeRuns, runsVersion, refreshStatus],
  );
  return <LiveContext.Provider value={value}>{children}</LiveContext.Provider>;
}

export function useLive() {
  return React.useContext(LiveContext);
}

// A seq past anything the server will ever assign, used to open a run stream
// without replaying its durable log backlog.
const SKIP_BACKLOG_SEQ = Number.MAX_SAFE_INTEGER;

/**
 * useRunStream subscribes to one run's live stream and returns the run record,
 * its latest progress and its log lines.
 *
 * The server replays the durable log on connect and dedupes by seq on resume,
 * so this is correct for a run that started days ago, one running right now, and
 * one that finishes while we are watching.
 *
 * `withLog: false` skips the backlog replay — used by list screens that want
 * live progress for a running job but not its whole log.
 */
export function useRunStream(runId, { withLog = true } = {}) {
  const [run, setRun] = React.useState(null);
  const [lines, setLines] = React.useState([]);
  const seenSeq = React.useRef(0);

  React.useEffect(() => {
    if (!runId) return;
    seenSeq.current = 0;
    setLines([]);
    const url = withLog
      ? `/api/runs/${runId}/events`
      : `/api/runs/${runId}/events?after=${SKIP_BACKLOG_SEQ}`;
    const es = new EventSource(url);
    es.onmessage = (e) => {
      let ev;
      try {
        ev = JSON.parse(e.data);
      } catch {
        return;
      }
      if (ev.type === "run" && ev.run) {
        setRun(ev.run);
      } else if (ev.type === "progress" && ev.progress) {
        setRun((prev) => (prev ? { ...prev, progress: ev.progress } : prev));
      } else if (ev.type === "log" && ev.line && withLog) {
        const line = ev.line;
        if (line.seq && line.seq <= seenSeq.current) return; // already delivered
        if (line.seq) seenSeq.current = line.seq;
        setLines((prev) => [...prev, line]);
      }
    };
    return () => es.close();
  }, [runId, withLog]);

  return { run, lines, setRun };
}

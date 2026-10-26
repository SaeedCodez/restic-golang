import * as React from "react";
import { Link, useNavigate } from "react-router-dom";
import { ArrowRight, CalendarClock, Eye, Play, Vault } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { IconTile, KIND } from "@/components/kind";
import { RunStrip } from "@/components/run-strip";
import { StatusBadge } from "@/components/status-badge";
import { Jobs, Runs } from "@/lib/api";
import { useLive, useRunStream } from "@/lib/live";
import { fmtBytes, fmtRelative } from "@/lib/format";
import { displayPercent, isActive } from "@/lib/runs";
import { describeScheduleTitle } from "@/lib/schedule";
import { handleStartResponse } from "@/lib/start-run";
import { cn } from "@/lib/utils";

/** How many recent backups a job card's run strip shows. */
export const STRIP_SLOTS = 24;

/**
 * useJobStrips loads the recent backup runs of each job, for its run strip.
 * Strips load after the cards themselves, and a refresh keeps the previous
 * strips on screen until the new ones arrive, so nothing flickers.
 */
export function useJobStrips(jobIds, version = 0) {
  const key = jobIds.join(",");
  const [strips, setStrips] = React.useState({});

  React.useEffect(() => {
    if (!key) {
      setStrips({});
      return undefined;
    }
    let alive = true;
    Promise.all(
      key.split(",").map(async (id) => {
        const r = await Runs.list({ jobId: id, kind: "backup", limit: STRIP_SLOTS });
        return [id, r.body?.runs || []];
      }),
    ).then((entries) => {
      if (alive) setStrips(Object.fromEntries(entries));
    });
    return () => {
      alive = false;
    };
  }, [key, version]);

  return strips;
}

/** What the last run stored, in one short phrase. */
export function lastRunSummary(run) {
  if (!run) return null;
  if (run.error) return run.error;
  const s = run.summary;
  if (!s) return null;
  return s.dataAdded ? `${fmtBytes(s.dataAdded)} added` : "nothing changed";
}

export function scheduleText(job) {
  if (!job.schedule?.enabled) return "Manual only";
  const title = describeScheduleTitle(job.schedule);
  if (job.scheduleState === "overdue") return `Overdue · ${title}`;
  if (job.nextDueAt) return `${title} · next ${fmtRelative(job.nextDueAt)}`;
  return title;
}

/**
 * JobCard answers, at a glance: what does this job back up, where to, and is
 * it healthy? The run strip shows its recent backups, newest on the right.
 *
 * A job that is running right now shows live progress inline — the card
 * subscribes to that run's stream (without replaying its log), so the number
 * moves without having to open the run.
 *
 * The whole card opens the job; with `actions` it also carries Run backup and
 * View job buttons, which sit above the card's link.
 */
export function JobCard({ job, runs = [], actions = false, onRan }) {
  const navigate = useNavigate();
  const { activeRuns } = useLive();
  const [starting, setStarting] = React.useState(false);

  const activeRun = activeRuns.find((r) => r.jobId === job.id);
  const { run: liveRun } = useRunStream(activeRun?.id, { withLog: false });
  const live = liveRun || activeRun;
  const running = Boolean(live && isActive(live.status));
  const pct = running ? displayPercent(live) : 0;
  const indeterminate = running && pct === 0 && !live.progress?.totalBytes;

  const last = job.lastRun;
  const summary = lastRunSummary(last);
  const source = job.folderName || job.databaseName || job.sourceLabel;
  const SourceIcon = job.sourceKind === "database" ? KIND.database.icon : KIND.folder.icon;
  const overdue = job.scheduleState === "overdue";

  const runNow = async () => {
    setStarting(true);
    const res = await Jobs.run(job.id);
    setStarting(false);
    handleStartResponse(res, navigate, "Could not start the backup.");
    onRan?.();
  };

  return (
    <Card
      className={cn(
        "group relative flex h-full flex-col gap-5 p-5 transition-colors hover:border-input sm:p-6",
        running && "border-running/35 hover:border-running/50",
      )}
    >
      {/* Name and badge share the first row; the route below them gets the full width. */}
      <div className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-x-4">
        <IconTile icon={KIND.job.icon} tone={KIND.job.tone} className="row-span-2" />
        <Link
          to={`/jobs/${job.id}`}
          className="display min-w-0 truncate text-md font-semibold outline-none after:absolute after:inset-0 after:rounded-2xl focus-visible:after:outline-2 focus-visible:after:outline-ring"
        >
          {job.name}
        </Link>
        <div className="shrink-0">
          {running ? (
            <StatusBadge status={live.status} />
          ) : last ? (
            <StatusBadge status={last.status} />
          ) : (
            <span className="rounded-full bg-secondary px-2.5 py-0.5 text-xs font-medium text-muted-foreground">
              Never run
            </span>
          )}
        </div>
        {/* Source and repository wrap onto two lines rather than both truncating. */}
        <p className="col-span-2 mt-1 flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5 text-sm text-muted-foreground">
          <span className="flex min-w-0 max-w-full items-center gap-1.5" title={job.sourceLabel}>
            <SourceIcon className="size-3.5 shrink-0 text-fuchsia" />
            <span className="truncate">{source || "—"}</span>
          </span>
          <ArrowRight className="size-3.5 shrink-0 opacity-50" />
          <span className="flex min-w-0 max-w-full items-center gap-1.5">
            <Vault className="size-3.5 shrink-0 text-cyan" />
            <span className="truncate">{job.repoName || "—"}</span>
          </span>
        </p>
      </div>

      {/* Strip bars and buttons are links of their own, so they sit above the card link. */}
      <div className="pointer-events-none relative [&_a]:pointer-events-auto">
        <RunStrip runs={runs} slots={STRIP_SLOTS} />
      </div>

      {running ? (
        <div className="space-y-2">
          <div className="flex items-baseline justify-between gap-3 text-sm">
            <span className="min-w-0 truncate font-mono text-xs text-muted-foreground">
              {live.progress?.currentFile || "working…"}
            </span>
            <span className="display tabular font-semibold text-running">
              {indeterminate ? "—" : `${Math.round(pct)}%`}
            </span>
          </div>
          <Progress value={pct} indeterminate={indeterminate} />
        </div>
      ) : null}

      <div className="mt-auto space-y-1.5 text-sm">
        {!running ? (
          <p className="truncate text-muted-foreground">
            {last ? (
              <>
                Last run <span className="text-foreground">{fmtRelative(last.startedAt)}</span>
                {summary ? (
                  <>
                    {" · "}
                    <span className={last.error ? "text-destructive" : undefined}>{summary}</span>
                  </>
                ) : null}
              </>
            ) : (
              "No backups yet"
            )}
          </p>
        ) : null}
        <p
          className={cn(
            "flex min-w-0 items-center gap-1.5",
            overdue ? "text-warning" : "text-muted-foreground",
          )}
        >
          <CalendarClock className="size-3.5 shrink-0" />
          <span className="truncate">{scheduleText(job)}</span>
        </p>
      </div>

      {actions ? (
        <div className="relative -mx-5 -mb-5 flex items-center gap-2 border-t border-border px-5 py-3.5 sm:-mx-6 sm:-mb-6 sm:px-6">
          {running ? (
            <Button variant="outline" size="sm" asChild>
              <Link to={`/runs/${live.id}`}>
                <Eye />
                View progress
              </Link>
            </Button>
          ) : (
            <Button variant="outline" size="sm" onClick={runNow} disabled={starting}>
              <Play />
              {starting ? "Starting…" : "Run backup"}
            </Button>
          )}
          <Button variant="ghost" size="sm" asChild className="ml-auto text-muted-foreground">
            <Link to={`/jobs/${job.id}`}>
              View job
              <ArrowRight />
            </Link>
          </Button>
        </div>
      ) : null}
    </Card>
  );
}

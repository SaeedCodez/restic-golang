import * as React from "react";
import { Link } from "react-router-dom";
import {
  AlertTriangle,
  FolderOpen,
  HardDrive,
  Play,
  CalendarClock,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardDescription, CardFooter } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Page, PageHeader } from "@/components/page";
import { ActiveRunsCard } from "@/components/active-runs";
import { GettingStarted } from "@/components/getting-started";
import { RunHistory } from "@/components/run-history";
import { StatusBadge } from "@/components/status-badge";
import { Folders, Jobs, Repositories, Runs, Status } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";
import { fmtRelative } from "@/lib/format";
import { describeScheduleTitle } from "@/lib/schedule";
import { cn } from "@/lib/utils";

const ATTENTION_CAP = 5;
const UPCOMING_CAP = 5;
const FAILURES_CAP = 5;

function lastRunSummary(run) {
  if (!run) return null;
  if (run.error) return run.error;
  return null;
}

/** Jobs whose last run failed/interrupted, or whose schedule is overdue. */
export function selectAttention(jobs) {
  const list = [];
  for (const job of jobs) {
    const last = job.lastRun;
    const failedLast = last && (last.status === "failed" || last.status === "interrupted");
    const overdue = job.scheduleState === "overdue";
    if (!failedLast && !overdue) continue;
    list.push({ job, failedLast, overdue });
  }
  list.sort((a, b) => {
    const aFail = a.failedLast ? 0 : 1;
    const bFail = b.failedLast ? 0 : 1;
    if (aFail !== bFail) return aFail - bFail;
    const aTime = a.job.lastRun?.startedAt ? new Date(a.job.lastRun.startedAt).getTime() : Infinity;
    const bTime = b.job.lastRun?.startedAt ? new Date(b.job.lastRun.startedAt).getTime() : Infinity;
    return aTime - bTime;
  });
  return list;
}

export function selectUpcoming(jobs) {
  return jobs
    .filter((j) => j.schedule?.enabled)
    .slice()
    .sort((a, b) => {
      if (!a.nextDueAt && !b.nextDueAt) return 0;
      if (!a.nextDueAt) return 1;
      if (!b.nextDueAt) return -1;
      return new Date(a.nextDueAt) - new Date(b.nextDueAt);
    })
    .slice(0, UPCOMING_CAP);
}

function InventoryStrip({ repoCount, folderCount, jobCount, scheduledCount }) {
  const tiles = [
    { label: "Repositories", value: repoCount, to: "/repositories", icon: HardDrive },
    { label: "Folders", value: folderCount, to: "/folders", icon: FolderOpen },
    { label: "Jobs", value: jobCount, to: "/jobs", icon: Play },
    { label: "Scheduled", value: scheduledCount, to: "/jobs", icon: CalendarClock },
  ];

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {tiles.map(({ label, value, to, icon: Icon }) => (
        <Link key={label} to={to} className="group">
          <Card className="h-full transition-colors hover:border-input">
            <div className="flex flex-col gap-1 px-4 py-3.5">
              <div className="flex items-center justify-between gap-2">
                <span className="text-[13px] text-muted-foreground">{label}</span>
                <Icon className="size-3.5 text-muted-foreground/60 transition-colors group-hover:text-muted-foreground" />
              </div>
              <span className="display text-2xl font-medium tabular tracking-tight">{value}</span>
            </div>
          </Card>
        </Link>
      ))}
    </div>
  );
}

function HealthBanner({ attention }) {
  const failedCount = attention.filter((a) => a.failedLast).length;
  const overdueCount = attention.filter((a) => a.overdue).length;
  const parts = [];
  if (failedCount > 0) {
    parts.push(
      `${failedCount} job${failedCount === 1 ? "" : "s"} need${failedCount === 1 ? "s" : ""} attention after their last run`,
    );
  }
  if (overdueCount > 0) {
    parts.push(
      `${overdueCount} scheduled backup${overdueCount === 1 ? "" : "s"} overdue`,
    );
  }
  if (parts.length === 0) return null;

  const destructive = failedCount > 0;

  return (
    <div
      className={cn(
        "flex items-start gap-2.5 rounded-lg border px-4 py-3 text-[13px]",
        destructive
          ? "border-destructive/30 bg-destructive/10 text-destructive"
          : "border-warning/30 bg-warning/10 text-warning",
      )}
    >
      <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
      <p className="min-w-0 font-medium leading-relaxed">{parts.join(" · ")}</p>
    </div>
  );
}

function AttentionCard({ attention }) {
  const shown = attention.slice(0, ATTENTION_CAP);
  const more = attention.length - shown.length;

  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1">
          <CardTitle>Needs attention</CardTitle>
          <CardDescription className="mt-1">
            Failed or interrupted last runs, and overdue schedules.
          </CardDescription>
        </div>
      </CardHeader>
      <div className="divide-y divide-border border-t border-border">
        {shown.map(({ job, failedLast, overdue }) => {
          const last = job.lastRun;
          const summary = lastRunSummary(last);
          return (
            <Link
              key={job.id}
              to={`/jobs/${job.id}`}
              className="block px-5 py-3.5 transition-colors hover:bg-accent/40"
            >
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  <span className="truncate text-sm font-medium">{job.name}</span>
                  {failedLast && last ? (
                    <StatusBadge status={last.status} />
                  ) : overdue ? (
                    <span className="rounded-full bg-warning/15 px-2 py-0.5 text-[11px] font-medium text-warning">
                      Overdue
                    </span>
                  ) : null}
                </div>
              </div>
              <p className="mt-1 truncate text-[13px] text-muted-foreground">
                {failedLast && last ? (
                  <>
                    Last run {fmtRelative(last.startedAt)}
                    {summary ? (
                      <>
                        {" · "}
                        <span className="text-destructive">{summary}</span>
                      </>
                    ) : null}
                  </>
                ) : overdue ? (
                  describeScheduleTitle(job.schedule)
                ) : null}
              </p>
            </Link>
          );
        })}
      </div>
      {more > 0 || attention.length > 0 ? (
        <CardFooter>
          <Link
            to="/jobs"
            className="text-[13px] text-muted-foreground hover:text-foreground hover:underline"
          >
            {more > 0 ? `View all jobs (${more} more) →` : "View all jobs →"}
          </Link>
        </CardFooter>
      ) : null}
    </Card>
  );
}

function RecentFailuresCard({ runs }) {
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1">
          <CardTitle>Recent backup failures</CardTitle>
          <CardDescription className="mt-1">
            Failed or interrupted backups, newest first.
          </CardDescription>
        </div>
      </CardHeader>
      <div className="border-t border-border">
        <RunHistory runs={runs} showContext />
      </div>
      <CardFooter>
        <Link
          to="/activity"
          className="text-[13px] text-muted-foreground hover:text-foreground hover:underline"
        >
          Open Activity →
        </Link>
      </CardFooter>
    </Card>
  );
}

function UpcomingCard({ jobs }) {
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1">
          <CardTitle>Upcoming</CardTitle>
          <CardDescription className="mt-1">Next automatic backups.</CardDescription>
        </div>
      </CardHeader>
      <div className="divide-y divide-border border-t border-border">
        {jobs.map((job) => (
          <Link
            key={job.id}
            to={`/jobs/${job.id}`}
            className="flex flex-wrap items-center justify-between gap-2 px-5 py-3.5 transition-colors hover:bg-accent/40"
          >
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{job.name}</p>
              <p
                className={cn(
                  "mt-0.5 text-[13px]",
                  job.scheduleState === "overdue" ? "text-warning" : "text-muted-foreground",
                )}
              >
                {describeScheduleTitle(job.schedule)}
              </p>
            </div>
            <span
              className={cn(
                "tabular shrink-0 text-[13px]",
                job.scheduleState === "overdue" ? "text-warning" : "text-muted-foreground",
              )}
            >
              {job.scheduleState === "overdue"
                ? "Overdue"
                : job.nextDueAt
                  ? fmtRelative(job.nextDueAt)
                  : "—"}
            </span>
          </Link>
        ))}
      </div>
    </Card>
  );
}

function InlineLoadError({ onRetry }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/10 px-4 py-3 text-[13px] text-destructive">
      <span>Could not load dashboard data.</span>
      <Button size="sm" variant="outline" onClick={onRetry}>
        Retry
      </Button>
    </div>
  );
}

async function mergeRecentBackupFailures() {
  const [failedR, interruptedR] = await Promise.all([
    Runs.list({ status: "failed", kind: "backup", limit: FAILURES_CAP }),
    Runs.list({ status: "interrupted", kind: "backup", limit: FAILURES_CAP }),
  ]);
  const merged = [...(failedR.body?.runs || []), ...(interruptedR.body?.runs || [])];
  merged.sort((a, b) => new Date(b.startedAt) - new Date(a.startedAt));
  // Dedupe by id in case of overlap, then cap.
  const seen = new Set();
  const unique = [];
  for (const run of merged) {
    if (seen.has(run.id)) continue;
    seen.add(run.id);
    unique.push(run);
    if (unique.length >= FAILURES_CAP) break;
  }
  return unique;
}

export default function Dashboard() {
  const { activeRuns, runsVersion } = useLive();

  const loadStatus = React.useCallback(async () => {
    const res = await Status.get();
    return res.body || {};
  }, []);
  const { data: statusData, reload: reloadStatus } = useFetch(loadStatus);

  const loadJobs = React.useCallback(async () => {
    const jobsR = await Jobs.list();
    const jobs = jobsR.body?.jobs || [];
    const attention = selectAttention(jobs);

    let folders = [];
    let repositories = [];
    let recentFailures = [];

    if (jobs.length === 0) {
      const [foldersR, reposR] = await Promise.all([Folders.list(), Repositories.list()]);
      folders = foldersR.body?.folders || [];
      repositories = reposR.body?.repositories || [];
    } else if (attention.length > 0) {
      recentFailures = await mergeRecentBackupFailures();
    }

    return { jobs, folders, repositories, recentFailures };
  }, []);
  const { data, loading, error, reload: reloadJobs } = useFetch(loadJobs);

  React.useEffect(() => {
    if (runsVersion > 0) reloadJobs();
  }, [runsVersion, reloadJobs]);

  React.useEffect(() => {
    document.title = "Dashboard · restic backup manager";
  }, []);

  const jobs = data?.jobs ?? [];
  const attention = React.useMemo(() => selectAttention(jobs), [jobs]);
  const upcoming = React.useMemo(() => selectUpcoming(jobs), [jobs]);
  const scheduledTotal = React.useMemo(
    () => jobs.filter((j) => j.schedule?.enabled).length,
    [jobs],
  );
  const empty = !loading && data && jobs.length === 0;
  const needsAttention = attention.length > 0;
  const recentFailures = data?.recentFailures || [];

  const repoCount = statusData?.counts?.repositories ?? 0;
  const folderCount = statusData?.counts?.folders ?? 0;
  const jobCount = loading && !data ? (statusData?.counts?.jobs ?? 0) : jobs.length;

  const hasFolder =
    (data?.folders?.length ?? 0) > 0 || (statusData?.counts?.folders ?? 0) > 0;
  const hasRepo =
    (data?.repositories?.length ?? 0) > 0 || (statusData?.counts?.repositories ?? 0) > 0;

  return (
    <Page>
      <PageHeader
        title="Dashboard"
        description="What you have configured, and what is happening."
      />

      {error ? (
        <div className="mb-4">
          <InlineLoadError
            onRetry={() => {
              reloadStatus();
              reloadJobs();
            }}
          />
        </div>
      ) : null}

      {loading && !data && !statusData ? (
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <Skeleton className="h-20 rounded-lg" />
            <Skeleton className="h-20 rounded-lg" />
            <Skeleton className="h-20 rounded-lg" />
            <Skeleton className="h-20 rounded-lg" />
          </div>
          <Skeleton className="h-40 w-full rounded-lg" />
        </div>
      ) : (
        <div className="space-y-4">
          <InventoryStrip
            repoCount={repoCount}
            folderCount={folderCount}
            jobCount={jobCount}
            scheduledCount={scheduledTotal}
          />

          {empty ? (
            <GettingStarted hasFolder={hasFolder} hasRepo={hasRepo} />
          ) : null}

          {!empty && needsAttention ? <HealthBanner attention={attention} /> : null}

          <ActiveRunsCard
            runs={activeRuns}
            showActivityLink
            emptyDescription="Start a backup from a job when you are ready."
            emptyAction={
              <Button size="sm" variant="outline" asChild>
                <Link to="/jobs">Open Jobs</Link>
              </Button>
            }
          />

          {!empty && needsAttention ? <AttentionCard attention={attention} /> : null}

          {!empty && needsAttention && recentFailures.length > 0 ? (
            <RecentFailuresCard runs={recentFailures} />
          ) : null}

          {!empty && upcoming.length > 0 ? <UpcomingCard jobs={upcoming} /> : null}
        </div>
      )}
    </Page>
  );
}

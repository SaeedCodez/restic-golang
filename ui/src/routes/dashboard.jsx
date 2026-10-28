import * as React from "react";
import { Link } from "react-router-dom";
import {
  ArrowUpRight,
  CalendarClock,
  CircleDashed,
  Clock,
  FolderOpen,
  ShieldAlert,
  ShieldCheck,
  Vault,
  Workflow,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardDescription, CardFooter } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Page, PageHeader, Section, SeeAll } from "@/components/page";
import { ActiveRunsCard } from "@/components/active-runs";
import { ErrorState } from "@/components/empty";
import { GettingStarted } from "@/components/getting-started";
import { IconTile, repositoryIcon } from "@/components/kind";
import { JobCard, STRIP_SLOTS, useJobStrips } from "@/components/job-card";
import { RunHistory } from "@/components/run-history";
import { RunStripLegend } from "@/components/run-strip";
import { StatusBadge } from "@/components/status-badge";
import { Folders, Jobs, Repositories, Runs, Status } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";
import { fmtRelative, repoLocation } from "@/lib/format";
import { describeScheduleTitle } from "@/lib/schedule";
import { Callout } from "@/components/fields";
import { cn } from "@/lib/utils";

const ATTENTION_CAP = 5;
const UPCOMING_CAP = 5;
const FAILURES_CAP = 5;
const JOB_CARDS_CAP = 6;
const LATEST_CAP = 6;

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

/** Job cards lead with the ones that need attention, then the most recently run. */
function orderJobCards(jobs, attention) {
  const rank = new Map(attention.map((a, i) => [a.job.id, i]));
  return jobs.slice().sort((a, b) => {
    const ra = rank.has(a.id) ? rank.get(a.id) : Infinity;
    const rb = rank.has(b.id) ? rank.get(b.id) : Infinity;
    if (ra !== rb) return ra - rb;
    const ta = a.lastRun?.startedAt ? new Date(a.lastRun.startedAt).getTime() : 0;
    const tb = b.lastRun?.startedAt ? new Date(b.lastRun.startedAt).getTime() : 0;
    return tb - ta;
  });
}

const HEALTH_TONE = {
  destructive: "border-destructive/30 bg-destructive/6",
  warning: "border-warning/35 bg-warning/7",
  success: "border-success/30 bg-success/6",
  neutral: "",
};

/**
 * HealthHero answers the one question the dashboard exists for — are my
 * backups OK? — in a sentence, coloured by the answer.
 */
function HealthHero({ jobs, attention, strips, upcoming, running, failedChecks = 0 }) {
  const failed = attention.filter((a) => a.failedLast).length;
  const overdue = attention.filter((a) => a.overdue).length;
  const neverRun = jobs.filter((j) => !j.lastRun).length;

  let lastBackup = null;
  for (const runs of Object.values(strips)) {
    const done = runs.find((r) => r.status === "success" || r.status === "success_warnings");
    if (done && (!lastBackup || new Date(done.startedAt) > new Date(lastBackup.startedAt))) {
      lastBackup = done;
    }
  }
  const next = upcoming.find((j) => j.nextDueAt && j.scheduleState !== "overdue");

  let tone, icon, headline, detail;
  if (failed > 0) {
    tone = "destructive";
    icon = ShieldAlert;
    headline = `${failed} job${failed === 1 ? " needs" : "s need"} attention`;
    detail = `The last run ${failed === 1 ? "of this job" : "of these jobs"} failed or was interrupted.${
      overdue > 0 ? ` ${overdue} scheduled backup${overdue === 1 ? " is" : "s are"} overdue as well.` : ""
    }`;
  } else if (failedChecks > 0) {
    tone = "destructive";
    icon = ShieldAlert;
    headline = `${failedChecks} repositor${failedChecks === 1 ? "y is" : "ies are"} damaged`;
    detail = "The last runs succeeded, but a repository check found errors in stored backups.";
  } else if (overdue > 0) {
    tone = "warning";
    icon = Clock;
    headline = `${overdue} scheduled backup${overdue === 1 ? " is" : "s are"} overdue`;
    detail = "They missed their last slot. They run as soon as the repository is free.";
  } else if (neverRun === jobs.length) {
    tone = "neutral";
    icon = CircleDashed;
    headline = "No backups have run yet";
    detail = "Open a job and run it once to take the first snapshot.";
  } else {
    tone = "success";
    icon = ShieldCheck;
    headline = "All backups are healthy";
    detail =
      neverRun > 0
        ? `Every job that has run succeeded. ${neverRun} job${neverRun === 1 ? " has" : "s have"} not run yet.`
        : "The last run of every job succeeded.";
  }

  const facts = [
    {
      label: "Last backup",
      value: lastBackup ? fmtRelative(lastBackup.startedAt) : "—",
      sub: lastBackup?.jobName,
    },
    {
      label: "Next backup",
      value: next ? fmtRelative(next.nextDueAt) : "—",
      sub: next ? next.name : "No schedules",
    },
    {
      label: running > 0 ? "Running now" : "Healthy jobs",
      value: running > 0 ? `${running}` : `${jobs.length - failed} of ${jobs.length}`,
      sub: running > 0 ? "live" : null,
    },
  ];

  return (
    <Card className={cn("overflow-hidden", HEALTH_TONE[tone])}>
      <div className="flex items-start gap-4 p-5 sm:items-center sm:gap-5 sm:p-7">
        <IconTile icon={icon} tone={tone} size="lg" />
        <div className="min-w-0">
          <h2 className="display text-xl font-semibold leading-tight sm:text-2xl">{headline}</h2>
          <p className="mt-1.5 text-base text-muted-foreground">{detail}</p>
        </div>
      </div>
      <dl className="grid grid-cols-1 border-t border-inherit sm:grid-cols-3">
        {facts.map((f, i) => (
          <div
            key={f.label}
            className={cn(
              "flex min-w-0 items-baseline justify-between gap-4 px-5 py-3.5 sm:block sm:px-7 sm:py-4",
              i > 0 && "border-t border-inherit sm:border-l sm:border-t-0",
            )}
          >
            <dt className="shrink-0 text-sm text-muted-foreground">{f.label}</dt>
            <dd className="min-w-0 text-right sm:mt-1 sm:text-left">
              <span className="display tabular block truncate font-semibold sm:text-lg">{f.value}</span>
              {f.sub ? (
                <span className="hidden truncate text-sm text-muted-foreground sm:block">{f.sub}</span>
              ) : null}
            </dd>
          </div>
        ))}
      </dl>
    </Card>
  );
}

function InventoryStrip({ repoCount, folderCount, jobCount, scheduledCount }) {
  const tiles = [
    { label: "Repositories", value: repoCount, to: "/repositories", icon: Vault, tone: "cyan" },
    { label: "Sources", value: folderCount, to: "/folders", icon: FolderOpen, tone: "fuchsia" },
    { label: "Jobs", value: jobCount, to: "/jobs", icon: Workflow, tone: "violet" },
    { label: "Scheduled", value: scheduledCount, to: "/jobs", icon: CalendarClock, tone: "brand" },
  ];

  return (
    <div className="grid grid-cols-2 gap-3 sm:gap-4 md:grid-cols-4">
      {tiles.map(({ label, value, to, icon, tone }) => (
        <Link key={label} to={to} className="group rounded-2xl">
          <Card className="h-full p-4 transition-colors group-hover:border-input sm:p-5">
            <div className="flex items-start justify-between">
              <IconTile icon={icon} tone={tone} />
              <ArrowUpRight className="size-4 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
            </div>
            <p className="display tabular mt-4 text-2xl font-semibold leading-none sm:text-3xl">{value}</p>
            <p className="mt-1.5 truncate text-sm text-muted-foreground">{label}</p>
          </Card>
        </Link>
      ))}
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
              className="flex items-start gap-4 px-6 py-4 transition-colors hover:bg-accent/50"
            >
              <IconTile
                icon={failedLast ? ShieldAlert : Clock}
                tone={failedLast ? "destructive" : "warning"}
                size="sm"
                className="mt-0.5"
              />
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 flex-wrap items-center gap-2.5">
                  <span className="truncate font-medium">{job.name}</span>
                  {failedLast && last ? (
                    <StatusBadge status={last.status} />
                  ) : overdue ? (
                    <span className="rounded-full bg-warning/14 px-2.5 py-0.5 text-xs font-medium text-warning">
                      Overdue
                    </span>
                  ) : null}
                </div>
                <p className="mt-1 truncate text-sm text-muted-foreground">
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
              </div>
            </Link>
          );
        })}
      </div>
      <CardFooter>
        <SeeAll to="/jobs">{more > 0 ? `View all jobs (${more} more)` : "View all jobs"}</SeeAll>
      </CardFooter>
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
        <SeeAll to="/activity">Open Activity</SeeAll>
      </CardFooter>
    </Card>
  );
}

function LatestRunsCard({ runs }) {
  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1">
          <CardTitle>Latest runs</CardTitle>
          <CardDescription className="mt-1">Every kind of operation, newest first.</CardDescription>
        </div>
      </CardHeader>
      <div className="border-t border-border">
        <RunHistory runs={runs} showContext emptyMessage="Finished operations appear here." />
      </div>
      <CardFooter>
        <SeeAll to="/activity">All activity</SeeAll>
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
        {jobs.map((job) => {
          const overdue = job.scheduleState === "overdue";
          return (
            <Link
              key={job.id}
              to={`/jobs/${job.id}`}
              className="flex items-center gap-4 px-6 py-4 transition-colors hover:bg-accent/50"
            >
              <IconTile icon={CalendarClock} tone={overdue ? "warning" : "brand"} size="sm" />
              <div className="min-w-0 flex-1">
                <p className="truncate font-medium">{job.name}</p>
                <p
                  className={cn(
                    "mt-0.5 truncate text-sm",
                    overdue ? "text-warning" : "text-muted-foreground",
                  )}
                >
                  {describeScheduleTitle(job.schedule)}
                </p>
              </div>
              <span
                className={cn(
                  "tabular shrink-0 text-sm font-medium",
                  overdue ? "text-warning" : "text-foreground",
                )}
              >
                {overdue ? "Overdue" : job.nextDueAt ? fmtRelative(job.nextDueAt) : "—"}
              </span>
            </Link>
          );
        })}
      </div>
    </Card>
  );
}

function RepositoryCards({ repositories, jobs }) {
  const jobsPerRepo = React.useMemo(() => {
    const m = {};
    for (const j of jobs) m[j.repositoryId] = (m[j.repositoryId] || 0) + 1;
    return m;
  }, [jobs]);

  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 lg:grid-cols-3">
      {repositories.map((repo) => {
        const n = jobsPerRepo[repo.id] || 0;
        return (
          <Link key={repo.id} to={`/repositories/${repo.id}`} className="group rounded-2xl">
            <Card className="flex h-full items-center gap-4 p-5 transition-colors group-hover:border-input">
              <IconTile icon={repositoryIcon(repo)} tone="cyan" />
              <div className="min-w-0 flex-1">
                <p className="truncate font-semibold">{repo.name}</p>
                <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">
                  {repoLocation(repo) || "—"}
                </p>
              </div>
              <span className="shrink-0 rounded-full bg-secondary px-2.5 py-0.5 text-xs font-medium tabular text-muted-foreground">
                {n} job{n === 1 ? "" : "s"}
              </span>
            </Card>
          </Link>
        );
      })}
    </div>
  );
}

/** FailedChecksCallout names the repositories whose last check found errors. */
function FailedChecksCallout({ repositories }) {
  return (
    <Callout
      tone="destructive"
      icon={ShieldAlert}
      title={
        repositories.length === 1
          ? "A repository check found errors"
          : `${repositories.length} repository checks found errors`
      }
    >
      Backups stored there may not restore completely. Open the check's log to see what is
      damaged:{" "}
      {repositories.map((repo, i) => (
        <React.Fragment key={repo.id}>
          {i > 0 ? ", " : null}
          <Link to={`/runs/${repo.lastCheck.runId}`} className="font-medium underline">
            {repo.name}
          </Link>
        </React.Fragment>
      ))}
      .
    </Callout>
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

function DashboardSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-52 w-full rounded-2xl" />
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-[136px] rounded-2xl" />
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {[0, 1].map((i) => (
          <Skeleton key={i} className="h-48 rounded-2xl" />
        ))}
      </div>
    </div>
  );
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
    if (!jobsR.ok) throw new Error(jobsR.body?.error || "Could not load jobs");
    const jobs = jobsR.body?.jobs || [];
    const attention = selectAttention(jobs);

    let folders = [];
    let repositories = [];
    let recentFailures = [];
    let latest = [];

    if (jobs.length === 0) {
      const [foldersR, reposR] = await Promise.all([Folders.list(), Repositories.list()]);
      folders = foldersR.body?.folders || [];
      repositories = reposR.body?.repositories || [];
    } else {
      const [reposR, latestR, failures] = await Promise.all([
        Repositories.list(),
        Runs.list({ status: "finished", limit: LATEST_CAP }),
        attention.length > 0 ? mergeRecentBackupFailures() : Promise.resolve([]),
      ]);
      repositories = reposR.body?.repositories || [];
      latest = latestR.body?.runs || [];
      recentFailures = failures;
    }

    return { jobs, folders, repositories, recentFailures, latest };
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
  const jobCards = React.useMemo(
    () => orderJobCards(jobs, attention).slice(0, JOB_CARDS_CAP),
    [jobs, attention],
  );
  const scheduledTotal = React.useMemo(
    () => jobs.filter((j) => j.schedule?.enabled).length,
    [jobs],
  );
  const empty = !loading && data && jobs.length === 0;
  const needsAttention = attention.length > 0;
  const recentFailures = data?.recentFailures || [];
  const strips = useJobStrips(
    jobCards.map((j) => j.id),
    runsVersion,
  );
  const repositories = data?.repositories || [];
  const failedChecks = repositories.filter((r) => r.lastCheck?.status === "failed");

  const repoCount = statusData?.counts?.repositories ?? 0;
  // Folders and databases are both sources a job can back up.
  const folderCount = (statusData?.counts?.folders ?? 0) + (statusData?.counts?.databases ?? 0);
  const jobCount = loading && !data ? (statusData?.counts?.jobs ?? 0) : jobs.length;

  const hasFolder = (data?.folders?.length ?? 0) > 0 || folderCount > 0;
  const hasRepo =
    (data?.repositories?.length ?? 0) > 0 || (statusData?.counts?.repositories ?? 0) > 0;

  return (
    <Page>
      <PageHeader
        title="Dashboard"
        description="What you have configured, and what is happening."
      />

      {error ? (
        <ErrorState
          className="mb-6"
          title="Could not load dashboard data"
          description="The jobs list did not load, so health and history below may be missing. Check that the server is running, then retry."
          onRetry={() => {
            reloadStatus();
            reloadJobs();
          }}
        />
      ) : null}

      {loading && !data && !statusData ? (
        <DashboardSkeleton />
      ) : (
        <div className="space-y-10">
          <div className="space-y-4">
            {empty ? (
              <GettingStarted hasFolder={hasFolder} hasRepo={hasRepo} />
            ) : data ? (
              <HealthHero
                jobs={jobs}
                attention={attention}
                strips={strips}
                upcoming={upcoming}
                running={activeRuns.length}
                failedChecks={failedChecks.length}
              />
            ) : null}

            <InventoryStrip
              repoCount={repoCount}
              folderCount={folderCount}
              jobCount={jobCount}
              scheduledCount={scheduledTotal}
            />
          </div>

          {failedChecks.length > 0 ? <FailedChecksCallout repositories={failedChecks} /> : null}

          <ActiveRunsCard
            runs={activeRuns}
            showActivityLink
            emptyDescription="Start a backup from a job when you are ready."
            emptyAction={
              <Button variant="outline" asChild>
                <Link to="/jobs">Open Jobs</Link>
              </Button>
            }
          />

          {!empty && needsAttention ? (
            <div className={cn("grid grid-cols-1 items-start gap-4", recentFailures.length > 0 && "lg:grid-cols-2")}>
              <AttentionCard attention={attention} />
              {recentFailures.length > 0 ? <RecentFailuresCard runs={recentFailures} /> : null}
            </div>
          ) : null}

          {!empty && jobCards.length > 0 ? (
            <Section
              title="Backup jobs"
              description={`The last ${STRIP_SLOTS} backups of each job, newest on the right.`}
              action={<SeeAll to="/jobs">All jobs</SeeAll>}
            >
              <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                {jobCards.map((job) => (
                  <JobCard key={job.id} job={job} runs={strips[job.id] || []} />
                ))}
              </div>
              <RunStripLegend />
            </Section>
          ) : null}

          {!empty && data ? (
            <div className={cn("grid grid-cols-1 items-start gap-4", upcoming.length > 0 && "lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]")}>
              <LatestRunsCard runs={data.latest || []} />
              {upcoming.length > 0 ? <UpcomingCard jobs={upcoming} /> : null}
            </div>
          ) : null}

          {!empty && repositories.length > 0 ? (
            <Section
              title="Repositories"
              description="Where the snapshots are stored."
              action={<SeeAll to="/repositories">All repositories</SeeAll>}
            >
              <RepositoryCards repositories={repositories} jobs={jobs} />
            </Section>
          ) : null}
        </div>
      )}
    </Page>
  );
}

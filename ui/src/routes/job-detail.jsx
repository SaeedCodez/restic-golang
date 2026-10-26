import * as React from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { ArrowRight, Database, FolderOpen, Play, Trash2, Vault } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { Breadcrumb, Mono, Page, PageHeader } from "@/components/page";
import { ActiveRunsCard } from "@/components/active-runs";
import { ErrorState } from "@/components/empty";
import { CheckOption } from "@/components/fields";
import { STRIP_SLOTS, lastRunSummary, scheduleText, useJobStrips } from "@/components/job-card";
import { KindTile } from "@/components/kind";
import { ListPagination } from "@/components/pagination";
import { RunHistory } from "@/components/run-history";
import { RunStrip, RunStripLegend } from "@/components/run-strip";
import { ScheduleEditor } from "@/components/schedule-editor";
import { RetentionEditor } from "@/components/retention-editor";
import { SnapshotsPanel } from "@/components/snapshots-panel";
import { StatusBadge, StatusDot } from "@/components/status-badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import NotFound from "@/routes/not-found";
import { Jobs, errorOf } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";
import { fmtRelative, fmtTime } from "@/lib/format";
import { PAGE_SIZE } from "@/lib/pagination";
import { describeRetentionTitle } from "@/lib/retention";
import { statusLabel } from "@/lib/runs";
import { handleStartResponse } from "@/lib/start-run";
import { cn } from "@/lib/utils";

/** Route is the job's "this → there" line: its source, then its repository. */
function Route({ job }) {
  const SourceIcon = job.sourceKind === "database" ? Database : FolderOpen;
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 text-base">
      <span className="flex min-w-0 items-center gap-2">
        <SourceIcon className="size-4 shrink-0 text-fuchsia" />
        <span className="min-w-0 truncate" title={job.sourceLabel}>
          {job.sourceLabel || job.folderPath || job.folderName || "?"}
        </span>
      </span>
      <ArrowRight className="size-4 shrink-0 opacity-50" />
      <Link
        to={`/repositories/${job.repositoryId}`}
        className="flex min-w-0 items-center gap-2 transition-colors hover:text-foreground"
      >
        <Vault className="size-4 shrink-0 text-cyan" />
        <span className="truncate">{job.repoName || "?"}</span>
      </Link>
    </span>
  );
}

/** One overview fact: a label/value row on phones, a column of its own from sm up. */
const FACT = "flex min-w-0 items-start justify-between gap-4 px-5 py-3.5 sm:block sm:px-6 sm:py-4";

/**
 * Overview is the job at a glance: its recent backups as a run strip, then
 * when it last ran, when it runs next and what it keeps.
 */
function Overview({ job, strip, totalRuns }) {
  const last = strip?.[0];
  const overdue = job.scheduleState === "overdue";
  const retention = job.retention?.enabled ? describeRetentionTitle(job.retention) : "";

  return (
    <Card className="mb-6">
      <div className="px-5 pt-5 sm:px-6">
        <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
          <h2 className="font-medium">Recent backups</h2>
          <p className="text-sm text-muted-foreground">
            The last {STRIP_SLOTS}, newest on the right
          </p>
        </div>
        <div className="mt-4 flex flex-wrap items-end justify-between gap-x-8 gap-y-4">
          <RunStrip runs={strip || []} slots={STRIP_SLOTS} className="h-10 w-full max-w-[25rem] gap-1 [&>*]:max-w-3" />
          <RunStripLegend />
        </div>
      </div>
      <dl className="mt-5 grid grid-cols-1 divide-y divide-border border-t border-border sm:grid-cols-3 sm:divide-x sm:divide-y-0">
        <div className={FACT}>
          <dt className="text-sm text-muted-foreground">Last backup</dt>
          <dd className="min-w-0 text-right sm:mt-1 sm:text-left">
            {last ? (
              <>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span className="inline-flex items-center gap-2 font-medium">
                      <StatusDot status={last.status} />
                      {fmtRelative(last.startedAt)}
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>
                    {statusLabel(last.status)} · {fmtTime(last.startedAt)}
                  </TooltipContent>
                </Tooltip>
                <p
                  className={cn(
                    "mt-0.5 truncate text-sm",
                    last.error ? "text-destructive" : "text-muted-foreground",
                  )}
                >
                  {lastRunSummary(last) || statusLabel(last.status)}
                </p>
              </>
            ) : (
              <span className="font-medium text-muted-foreground">Never</span>
            )}
          </dd>
        </div>
        <div className={FACT}>
          <dt className="text-sm text-muted-foreground">Automatic backup</dt>
          <dd className={cn("min-w-0 text-right font-medium sm:mt-1 sm:text-left", overdue && "text-warning")}>
            {scheduleText(job)}
          </dd>
        </div>
        <div className={FACT}>
          <dt className="text-sm text-muted-foreground">Retention</dt>
          <dd className="min-w-0 text-right sm:mt-1 sm:text-left">
            <p className="font-medium">{retention || "Keeps every snapshot"}</p>
            <p className="mt-0.5 text-sm text-muted-foreground tabular">
              {totalRuns} operation{totalRuns === 1 ? "" : "s"} so far
            </p>
          </dd>
        </div>
      </dl>
    </Card>
  );
}

export default function JobDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { activeRuns, runsVersion } = useLive();
  const [starting, setStarting] = React.useState(false);
  const [deleting, setDeleting] = React.useState(false);
  const [page, setPage] = React.useState(1);

  const loader = React.useCallback(async () => {
    const offset = (page - 1) * PAGE_SIZE;
    const [jobR, runsR] = await Promise.all([
      Jobs.get(id),
      Jobs.runs(id, { limit: PAGE_SIZE, offset }),
    ]);
    if (jobR.status === 404) return { missing: true };
    if (!jobR.ok) throw new Error(errorOf(jobR, "Could not load the job."));
    return {
      job: jobR.body?.job,
      runs: runsR.body?.runs || [],
      total: runsR.body?.total || 0,
    };
  }, [id, page]);
  const { data, loading, error, reload } = useFetch(loader);
  // Declared before any early return: hooks must not sit behind a conditional.
  const loadSnapshots = React.useCallback(() => Jobs.snapshots(id), [id]);
  const strips = useJobStrips([id], runsVersion);

  React.useEffect(() => {
    if (runsVersion > 0) reload();
  }, [runsVersion, reload]);

  React.useEffect(() => {
    if (data?.job) document.title = `${data.job.name} · restic backup manager`;
  }, [data]);

  // Keep page in range when history shrinks (e.g. after deletes elsewhere).
  React.useEffect(() => {
    if (!data || data.missing) return;
    const totalPages = Math.max(1, Math.ceil((data.total || 0) / PAGE_SIZE));
    if (page > totalPages) setPage(totalPages);
  }, [data, page]);

  if (data?.missing) return <NotFound />;

  if (error && !data) {
    return (
      <Page>
        <Breadcrumb to="/jobs" label="Jobs" current="Job" />
        <ErrorState
          title="Could not load this job"
          description={`${error.message} Check that the server is running, then retry.`}
          onRetry={reload}
        />
      </Page>
    );
  }

  if (loading && !data) {
    return (
      <Page>
        <Skeleton className="mb-4 h-5 w-40" />
        <Skeleton className="mb-10 h-10 w-80" />
        <Skeleton className="mb-6 h-44 w-full rounded-2xl" />
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
          <Skeleton className="h-72 rounded-2xl" />
          <Skeleton className="h-72 rounded-2xl" />
        </div>
      </Page>
    );
  }

  const job = data.job;
  const runs = data.runs;
  const total = data.total || 0;
  const jobActiveRuns = activeRuns.filter((r) => r.jobId === id);
  const activeRun = jobActiveRuns[0] || runs.find((r) => r.status === "running" || r.status === "starting");

  const runNow = async () => {
    setStarting(true);
    const res = await Jobs.run(id);
    setStarting(false);
    if (handleStartResponse(res, navigate, "Could not start the backup.", { stay: true })) {
      reload();
    }
  };

  const remove = async (forgetSnapshots) => {
    if (forgetSnapshots) {
      const res = await Jobs.forget(id, { deleteJob: true });
      // Job is going away — navigate to the new run (or jobs list on failure).
      return handleStartResponse(res, navigate, "Could not delete the job and its snapshots.");
    }
    const res = await Jobs.remove(id);
    if (res.ok) {
      toast.success(`Job “${job.name}” deleted.`);
      navigate("/jobs");
      return true;
    }
    toast.error(errorOf(res, "Could not delete the job."));
    return false;
  };

  return (
    <Page>
      <PageHeader
        breadcrumb={<Breadcrumb to="/jobs" label="Jobs" current={job.name} />}
        icon={<KindTile kind="job" size="lg" />}
        title={job.name}
        description={<Route job={job} />}
        actions={
          <>
            {activeRun ? (
              <Button variant="outline" asChild>
                <Link to={`/runs/${activeRun.id}`}>
                  <StatusBadge status={activeRun.status} />
                  View progress
                </Link>
              </Button>
            ) : (
              <Button onClick={runNow} disabled={starting}>
                <Play />
                {starting ? "Starting…" : "Run backup"}
              </Button>
            )}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="icon"
                  variant="outline"
                  onClick={() => setDeleting(true)}
                  className="text-muted-foreground hover:text-destructive"
                  aria-label="Delete job"
                >
                  <Trash2 />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Delete this job</TooltipContent>
            </Tooltip>
          </>
        }
      />

      {jobActiveRuns.length > 0 ? (
        <div className="mb-6">
          <ActiveRunsCard
            runs={jobActiveRuns}
            title="Running for this job"
            description="Live operations started from this job."
          />
        </div>
      ) : null}

      <Overview job={job} strip={strips[id]} totalRuns={total} />

      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <div className="min-w-0 space-y-6">
          <Card>
            <CardHeader>
              <div className="min-w-0 flex-1">
                <CardTitle className="flex items-center gap-2.5">
                  Activity
                  <span className="rounded-full bg-secondary px-2 py-0.5 text-xs font-semibold tabular text-muted-foreground">
                    {total}
                  </span>
                </CardTitle>
                <CardDescription className="mt-1">
                  Finished and in-progress operations for this job. Open a row for the full log.
                </CardDescription>
              </div>
            </CardHeader>
            <div className="border-t border-border">
              <RunHistory
                runs={runs}
                emptyMessage="This job has never run. Start a backup and it will appear here."
              />
            </div>
            {total > PAGE_SIZE ? (
              <div className="border-t border-border px-5 sm:px-6">
                <ListPagination
                  page={page}
                  pageSize={PAGE_SIZE}
                  total={total}
                  onPageChange={setPage}
                />
              </div>
            ) : null}
          </Card>

          <SnapshotsPanel
            title="Snapshots"
            description={
              <>
                Matched by this job's permanent restic tag <Mono>{job.tag}</Mono>, so they stay
                findable even if this app's state is lost.
              </>
            }
            load={loadSnapshots}
            repositoryId={job.repositoryId}
            reloadKey={runsVersion}
            defaultRestoreTarget={job.sourceKind === "database" ? undefined : job.folderPath}
            databaseSource={
              job.sourceKind === "database"
                ? { id: job.databaseId, name: job.databaseName, database: job.database, engine: job.engine }
                : undefined
            }
            jobId={id}
            stay
          />
        </div>

        <div className="space-y-6">
          <ScheduleEditor job={job} onSaved={() => reload()} />
          <RetentionEditor job={job} onSaved={() => reload()} stay />
        </div>
      </div>

      <JobDeleteDialog
        open={deleting}
        onOpenChange={setDeleting}
        jobName={job.name}
        onConfirm={remove}
      />
    </Page>
  );
}

function JobDeleteDialog({ open, onOpenChange, jobName, onConfirm }) {
  const [forgetSnapshots, setForgetSnapshots] = React.useState(false);
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => {
    if (!open) return;
    setForgetSnapshots(false);
    setBusy(false);
  }, [open]);

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    const ok = await onConfirm(forgetSnapshots);
    setBusy(false);
    if (ok) onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="contents">
          <DialogHeader>
            <DialogTitle>Delete “{jobName}”?</DialogTitle>
            <DialogDescription>
              The job is removed from the app. Run history is kept. Snapshots stay in the
              repository unless you choose otherwise below.
            </DialogDescription>
          </DialogHeader>
          <CheckOption
            checked={forgetSnapshots}
            onChange={setForgetSnapshots}
            disabled={busy}
            title="Also delete this job's snapshots"
            description="Forget every snapshot tagged for this job and prune unused data. This cannot be undone."
            className="has-[:checked]:border-destructive/40 has-[:checked]:bg-destructive/5"
          />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" variant="destructive" disabled={busy}>
              {busy ? "Working…" : forgetSnapshots ? "Delete job and snapshots" : "Delete job"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

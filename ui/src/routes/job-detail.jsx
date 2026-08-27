import * as React from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { ArrowRight, Play, Trash2 } from "lucide-react";
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
import { ListPagination } from "@/components/pagination";
import { RunHistory } from "@/components/run-history";
import { ScheduleEditor } from "@/components/schedule-editor";
import { RetentionEditor } from "@/components/retention-editor";
import { SnapshotsPanel } from "@/components/snapshots-panel";
import { StatusBadge } from "@/components/status-badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import NotFound from "@/routes/not-found";
import { Jobs, errorOf } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";
import { PAGE_SIZE } from "@/lib/pagination";
import { handleStartResponse } from "@/lib/start-run";

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
    return {
      job: jobR.body?.job,
      runs: runsR.body?.runs || [],
      total: runsR.body?.total || 0,
    };
  }, [id, page]);
  const { data, loading, reload } = useFetch(loader);
  // Declared before any early return: hooks must not sit behind a conditional.
  const loadSnapshots = React.useCallback(() => Jobs.snapshots(id), [id]);

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

  if (loading && !data) {
    return (
      <Page>
        <Skeleton className="mb-4 h-7 w-52" />
        <Skeleton className="h-32 w-full rounded-xl" />
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
        title={job.name}
        description={
          <span className="flex items-center gap-2 font-mono text-xs">
            <span className="min-w-0 flex-1 truncate" title={job.folderPath}>
              {job.folderPath || job.folderName || "?"}
            </span>
            <ArrowRight className="size-3 shrink-0 opacity-60" />
            <span className="shrink-0">{job.repoName || "?"}</span>
          </span>
        }
        actions={
          <>
            {activeRun ? (
              <Button size="sm" variant="outline" asChild>
                <Link to={`/runs/${activeRun.id}`}>
                  <StatusBadge status={activeRun.status} />
                  View progress
                </Link>
              </Button>
            ) : (
              <Button size="sm" onClick={runNow} disabled={starting}>
                <Play />
                {starting ? "Starting…" : "Run backup"}
              </Button>
            )}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="icon-sm"
                  variant="ghost"
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

      <div className="space-y-4">
        <ScheduleEditor job={job} onSaved={() => reload()} />
        <RetentionEditor job={job} onSaved={() => reload()} stay />

        <div className="space-y-4">
          <ActiveRunsCard
            runs={jobActiveRuns}
            title="Running for this job"
            description="Live operations started from this job."
            emptyDescription="Start a backup, restore, or retention run and it will appear here."
          />

          <Card>
            <CardHeader>
              <div className="min-w-0 flex-1">
                <CardTitle>
                  Activity{" "}
                  <span className="ml-1 tabular text-xs font-normal text-muted-foreground">
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
              <div className="border-t border-border px-4">
                <ListPagination
                  page={page}
                  pageSize={PAGE_SIZE}
                  total={total}
                  onPageChange={setPage}
                />
              </div>
            ) : null}
          </Card>
        </div>

        <SnapshotsPanel
          title="Snapshots from this job"
          description={
            <>
              Matched by this job's permanent restic tag <Mono>{job.tag}</Mono>, so they stay
              findable even if this app's state is lost.
            </>
          }
          load={loadSnapshots}
          repositoryId={job.repositoryId}
          reloadKey={runsVersion}
          defaultRestoreTarget={job.folderPath}
          jobId={id}
          stay
        />
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
          <label className="flex cursor-pointer items-start justify-between gap-3 rounded-md border border-border px-3 py-2.5">
            <div>
              <p className="text-sm font-medium">Also delete this job's snapshots</p>
              <p className="text-[13px] text-muted-foreground">
                Forget every snapshot tagged for this job and prune unused data. This cannot
                be undone.
              </p>
            </div>
            <input
              type="checkbox"
              className="mt-0.5 size-4 accent-foreground"
              checked={forgetSnapshots}
              onChange={(e) => setForgetSnapshots(e.target.checked)}
              disabled={busy}
            />
          </label>
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

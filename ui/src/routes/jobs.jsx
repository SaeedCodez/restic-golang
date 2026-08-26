import * as React from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { ArrowRight, Play, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Progress } from "@/components/ui/progress";
import { Page, PageHeader } from "@/components/page";
import { StatusBadge } from "@/components/status-badge";
import { EmptyState } from "@/components/empty";
import { Folders, Jobs, Repositories, errorOf } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive, useRunStream } from "@/lib/live";
import { fmtBytes, fmtRelative } from "@/lib/format";
import { displayPercent, isActive } from "@/lib/runs";
import { describeScheduleTitle } from "@/lib/schedule";
import { handleStartResponse } from "@/lib/start-run";
import { cn } from "@/lib/utils";

/** What the last run stored, in one short phrase. */
function lastRunSummary(run) {
  if (!run) return null;
  if (run.error) return run.error;
  const s = run.summary;
  if (!s) return null;
  return s.dataAdded ? `${fmtBytes(s.dataAdded)} added` : "nothing changed";
}

/**
 * JobCard answers, at a glance: what does this job back up, where to, and is
 * it healthy?
 *
 * A job that is running right now shows live progress inline — the card
 * subscribes to that run's stream (without replaying its log), so the number
 * moves without having to open the run.
 */
function JobCard({ job, onRan }) {
  const navigate = useNavigate();
  const { activeRuns } = useLive();
  const [starting, setStarting] = React.useState(false);

  const activeRun = activeRuns.find((r) => r.jobId === job.id);
  const { run: liveRun } = useRunStream(activeRun?.id, { withLog: false });
  const live = liveRun || activeRun;
  const running = Boolean(live && isActive(live.status));

  const last = job.lastRun;
  const summary = lastRunSummary(last);

  const runNow = async () => {
    setStarting(true);
    const res = await Jobs.run(job.id);
    setStarting(false);
    handleStartResponse(res, navigate, "Could not start the backup.");
    onRan?.();
  };

  return (
    <Card className="transition-colors hover:border-input">
      <div className="flex flex-wrap items-start justify-between gap-3 p-4">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Link
              to={`/jobs/${job.id}`}
              className="display truncate text-sm font-medium hover:underline"
            >
              {job.name}
            </Link>
            {running ? (
              <StatusBadge status={live.status} />
            ) : last ? (
              <StatusBadge status={last.status} />
            ) : (
              <span className="text-xs text-muted-foreground">never run</span>
            )}
          </div>

          <div className="mt-1.5 flex items-center gap-2 font-mono text-xs text-muted-foreground">
            <span className="max-w-fit flex-1 truncate" title={job.folderPath}>
              {job.folderPath || job.folderName || "?"}
            </span>
            <ArrowRight className="size-3 shrink-0 opacity-60" />
            {/* The repository name keeps its full width; the long path gives way. */}
            <span className="shrink-0 truncate">{job.repoName || "?"}</span>
          </div>

          {!running && last ? (
            <p className="mt-2 text-[13px] text-muted-foreground">
              Last run {fmtRelative(last.startedAt)}
              {summary ? (
                <>
                  {" · "}
                  <span className={last.error ? "text-destructive" : undefined}>{summary}</span>
                </>
              ) : null}
            </p>
          ) : null}

          {job.schedule?.enabled ? (
            <p
              className={cn(
                "mt-1.5 text-[13px]",
                job.scheduleState === "overdue" ? "text-warning" : "text-muted-foreground",
              )}
            >
              {job.scheduleState === "overdue"
                ? `Overdue · ${describeScheduleTitle(job.schedule)}`
                : job.nextDueAt
                  ? `${describeScheduleTitle(job.schedule)} · next ${fmtRelative(job.nextDueAt)}`
                  : describeScheduleTitle(job.schedule)}
            </p>
          ) : null}
        </div>

        <div className="flex shrink-0 items-center gap-2">
          {running ? (
            <Button variant="outline" size="sm" asChild>
              <Link to={`/runs/${live.id}`}>View progress</Link>
            </Button>
          ) : (
            <Button size="sm" onClick={runNow} disabled={starting}>
              <Play />
              {starting ? "Starting…" : "Run backup"}
            </Button>
          )}
        </div>
      </div>

      {running ? (
        <div className="space-y-1.5 border-t border-border px-4 py-3">
          <div className="flex items-baseline justify-between gap-3 text-[13px]">
            <span className="truncate font-mono text-xs text-muted-foreground">
              {live.progress?.currentFile || "working…"}
            </span>
            <span className="tabular font-medium">{Math.round(displayPercent(live))}%</span>
          </div>
          <Progress
            value={displayPercent(live)}
            indeterminate={displayPercent(live) === 0 && !live.progress?.totalBytes}
          />
        </div>
      ) : null}
    </Card>
  );
}

function NewJobDialog({ open, onOpenChange, folders, repositories, onCreated }) {
  const [name, setName] = React.useState("");
  const [folderId, setFolderId] = React.useState("");
  const [repositoryId, setRepositoryId] = React.useState("");
  const [autoDaily, setAutoDaily] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    if (!open) return;
    setName("");
    setError("");
    setAutoDaily(false);
    setFolderId(folders[0]?.id || "");
    setRepositoryId(repositories[0]?.id || "");
  }, [open, folders, repositories]);

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    const body = { name: name.trim(), folderId, repositoryId };
    if (autoDaily) {
      body.schedule = { enabled: true, kind: "daily", at: "02:00" };
    }
    const res = await Jobs.create(body);
    setBusy(false);
    if (res.ok) {
      toast.success(`Job “${res.body.job.name}” created.`);
      onOpenChange(false);
      onCreated();
    } else {
      setError(errorOf(res, "Could not create the job."));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="contents">
          <DialogHeader>
            <DialogTitle>New backup job</DialogTitle>
            <DialogDescription>
              A job pairs one folder with one repository. Run it, and its whole history
              lives on its page.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="job-name">Name</Label>
              <Input
                id="job-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Documents → offsite"
                autoFocus
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="job-folder">Back up this folder</Label>
              <Select value={folderId} onValueChange={setFolderId}>
                <SelectTrigger id="job-folder">
                  <SelectValue placeholder="Choose a folder" />
                </SelectTrigger>
                <SelectContent>
                  {folders.map((f) => (
                    <SelectItem key={f.id} value={f.id}>
                      {f.name} — {f.path}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="job-repo">Store it in</Label>
              <Select value={repositoryId} onValueChange={setRepositoryId}>
                <SelectTrigger id="job-repo">
                  <SelectValue placeholder="Choose a repository" />
                </SelectTrigger>
                <SelectContent>
                  {repositories.map((r) => (
                    <SelectItem key={r.id} value={r.id}>
                      {r.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <label className="flex cursor-pointer items-start gap-3 rounded-md border border-border px-3 py-2.5">
              <input
                type="checkbox"
                className="mt-0.5 size-4 accent-foreground"
                checked={autoDaily}
                onChange={(e) => setAutoDaily(e.target.checked)}
              />
              <span>
                <span className="block text-sm font-medium">Also run automatically</span>
                <span className="block text-[13px] text-muted-foreground">
                  Daily at 02:00 while the app is running. You can change this later on the job
                  page.
                </span>
              </span>
            </label>

            {error ? (
              <p className="rounded-md bg-destructive/10 px-3 py-2 text-[13px] text-destructive">
                {error}
              </p>
            ) : null}
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !name.trim() || !folderId || !repositoryId}>
              {busy ? "Creating…" : "Create job"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export default function JobsRoute() {
  const { runsVersion } = useLive();
  const location = useLocation();
  const navigate = useNavigate();
  const [dialogOpen, setDialogOpen] = React.useState(false);

  const loader = React.useCallback(async () => {
    const [jobsR, foldersR, reposR] = await Promise.all([
      Jobs.list(),
      Folders.list(),
      Repositories.list(),
    ]);
    return {
      jobs: jobsR.body?.jobs || [],
      folders: foldersR.body?.folders || [],
      repositories: reposR.body?.repositories || [],
    };
  }, []);
  const { data, loading, reload } = useFetch(loader);

  // A run finishing anywhere changes a job's health, so refresh on run events.
  React.useEffect(() => {
    if (runsVersion > 0) reload();
  }, [runsVersion, reload]);

  // Open New job from GettingStarted (Dashboard) or other deep links.
  // Effect + clear so same-route state.create works without a remount.
  React.useEffect(() => {
    if (location.state?.create) {
      setDialogOpen(true);
      navigate(location.pathname, { replace: true, state: {} });
    }
  }, [location.state, location.pathname, navigate]);

  React.useEffect(() => {
    document.title = "Jobs · restic backup manager";
  }, []);

  const jobs = data?.jobs || [];
  const folders = data?.folders || [];
  const repositories = data?.repositories || [];
  const canCreate = folders.length > 0 && repositories.length > 0;

  return (
    <Page>
      <PageHeader
        title="Backup jobs"
        description="A job is a saved pairing of a folder and a repository — the thing you run, and where its history lives."
        actions={
          jobs.length > 0 ? (
            <Button onClick={() => setDialogOpen(true)} disabled={!canCreate}>
              <Plus />
              New job
            </Button>
          ) : null
        }
      />

      {loading && !data ? (
        <div className="space-y-3">
          <Skeleton className="h-24 w-full rounded-xl" />
          <Skeleton className="h-24 w-full rounded-xl" />
        </div>
      ) : jobs.length === 0 ? (
        <EmptyState
          icon={Play}
          title="No jobs yet"
          description={
            canCreate
              ? "You have a folder and a repository — pair them into a job and you can back up in one click."
              : "Finish setup on the Dashboard, then come back here to create a job."
          }
          action={
            canCreate ? (
              <Button onClick={() => setDialogOpen(true)}>
                <Plus />
                Create a job
              </Button>
            ) : (
              <Button variant="outline" asChild>
                <Link to="/dashboard">Finish setup on the Dashboard</Link>
              </Button>
            )
          }
        />
      ) : (
        <div className="space-y-3">
          {jobs.map((job) => (
            <JobCard key={job.id} job={job} onRan={reload} />
          ))}
        </div>
      )}

      <NewJobDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        folders={folders}
        repositories={repositories}
        onCreated={reload}
      />
    </Page>
  );
}

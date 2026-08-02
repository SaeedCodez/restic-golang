import * as React from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { ArrowRight, FolderOpen, HardDrive, Play, Plus } from "lucide-react";
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
import { handleStartResponse } from "@/lib/start-run";

/** What the last run stored, in one short phrase. */
function lastRunSummary(run) {
  if (!run) return null;
  if (run.error) return run.error;
  const s = run.summary;
  if (!s) return null;
  return s.dataAdded ? `${fmtBytes(s.dataAdded)} added` : "nothing changed";
}

/**
 * JobCard is the unit of the home screen. It answers, at a glance: what does
 * this job back up, where to, and is it healthy?
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
              className="truncate text-[15px] font-semibold tracking-tight hover:underline"
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
            <span className="min-w-0 flex-1 truncate" title={job.folderPath}>
              {job.folderPath || job.folderName || "?"}
            </span>
            <ArrowRight className="size-3 shrink-0 opacity-60" />
            <span className="max-w-[40%] shrink-0 truncate">{job.repoName || "?"}</span>
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
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    if (!open) return;
    setName("");
    setError("");
    setFolderId(folders[0]?.id || "");
    setRepositoryId(repositories[0]?.id || "");
  }, [open, folders, repositories]);

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    const res = await Jobs.create({ name: name.trim(), folderId, repositoryId });
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

/** GettingStarted walks a brand-new install through the three things it needs. */
function GettingStarted({ hasFolder, hasRepo, onNewJob }) {
  const steps = [
    {
      done: hasFolder,
      title: "Add a folder to back up",
      body: "Name the directory you want protected, so you never retype the path.",
      to: "/folders",
      cta: "Add a folder",
      icon: FolderOpen,
    },
    {
      done: hasRepo,
      title: "Add a storage repository",
      body: "Where restic keeps the encrypted backups. A local directory works with no credentials.",
      to: "/repositories",
      cta: "Add a repository",
      icon: HardDrive,
    },
    {
      done: false,
      title: "Create a job",
      body: "Pair the two. The job is the thing you run and come back to.",
      cta: "Create a job",
      icon: Play,
      action: onNewJob,
      disabled: !hasFolder || !hasRepo,
    },
  ];

  return (
    <Card className="overflow-hidden">
      <div className="border-b border-border px-5 py-4">
        <h2 className="text-[15px] font-semibold tracking-tight">Get set up</h2>
        <p className="mt-1 text-[13px] text-muted-foreground">
          Three steps, once. After that, backing up is one click.
        </p>
      </div>
      <ol className="divide-y divide-border">
        {steps.map((step, i) => (
          <li key={step.title} className="flex flex-wrap items-center gap-4 px-5 py-4">
            <span
              className={
                step.done
                  ? "grid size-7 shrink-0 place-items-center rounded-full bg-success/15 text-xs font-semibold text-success"
                  : "grid size-7 shrink-0 place-items-center rounded-full bg-muted text-xs font-semibold text-muted-foreground"
              }
            >
              {step.done ? "✓" : i + 1}
            </span>
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">{step.title}</p>
              <p className="text-[13px] text-muted-foreground">{step.body}</p>
            </div>
            {step.done ? null : step.action ? (
              <Button size="sm" onClick={step.action} disabled={step.disabled}>
                {step.cta}
              </Button>
            ) : (
              <Button size="sm" variant="outline" asChild>
                <Link to={step.to}>{step.cta}</Link>
              </Button>
            )}
          </li>
        ))}
      </ol>
    </Card>
  );
}

export default function JobsRoute() {
  const { runsVersion } = useLive();
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
        <div className="space-y-4">
          <GettingStarted
            hasFolder={folders.length > 0}
            hasRepo={repositories.length > 0}
            onNewJob={() => setDialogOpen(true)}
          />
          {canCreate ? (
            <EmptyState
              icon={Play}
              title="No jobs yet"
              description="You have a folder and a repository — pair them into a job and you can back up in one click."
              action={
                <Button onClick={() => setDialogOpen(true)}>
                  <Plus />
                  Create a job
                </Button>
              }
            />
          ) : null}
        </div>
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

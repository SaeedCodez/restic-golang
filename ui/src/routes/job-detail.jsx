import * as React from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { ArrowRight, Play, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Breadcrumb, Mono, Page, PageHeader } from "@/components/page";
import { RunHistory } from "@/components/run-history";
import { ScheduleEditor } from "@/components/schedule-editor";
import { RetentionEditor } from "@/components/retention-editor";
import { SnapshotsPanel } from "@/components/snapshots-panel";
import { StatusBadge } from "@/components/status-badge";
import { useConfirm } from "@/components/confirm";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import NotFound from "@/routes/not-found";
import { Jobs, errorOf } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";
import { handleStartResponse } from "@/lib/start-run";

export default function JobDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const { runsVersion } = useLive();
  const [starting, setStarting] = React.useState(false);

  const loader = React.useCallback(async () => {
    const [jobR, runsR] = await Promise.all([Jobs.get(id), Jobs.runs(id)]);
    if (jobR.status === 404) return { missing: true };
    return { job: jobR.body?.job, runs: runsR.body?.runs || [] };
  }, [id]);
  const { data, loading, reload } = useFetch(loader);
  // Declared before any early return: hooks must not sit behind a conditional.
  const loadSnapshots = React.useCallback(() => Jobs.snapshots(id), [id]);

  React.useEffect(() => {
    if (runsVersion > 0) reload();
  }, [runsVersion, reload]);

  React.useEffect(() => {
    if (data?.job) document.title = `${data.job.name} · restic backup manager`;
  }, [data]);

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
  const activeRun = runs.find((r) => r.status === "running" || r.status === "starting");

  const runNow = async () => {
    setStarting(true);
    const res = await Jobs.run(id);
    setStarting(false);
    handleStartResponse(res, navigate, "Could not start the backup.");
    reload();
  };

  const remove = async () => {
    const ok = await confirm({
      title: `Delete “${job.name}”?`,
      description:
        "The job stops existing, but its run history and the snapshots already in the repository are kept.",
      confirmLabel: "Delete job",
      destructive: true,
    });
    if (!ok) return;
    const res = await Jobs.remove(id);
    if (res.ok) {
      toast.success(`Job “${job.name}” deleted.`);
      navigate("/jobs");
    } else {
      toast.error(errorOf(res, "Could not delete the job."));
    }
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
                  onClick={remove}
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
        <RetentionEditor job={job} onSaved={() => reload()} />

        <Card>
          <CardHeader>
            <CardTitle>
              Run history{" "}
              <span className="ml-1 tabular text-xs font-normal text-muted-foreground">
                {runs.length}
              </span>
            </CardTitle>
          </CardHeader>
          <div className="border-t border-border">
            <RunHistory
              runs={runs}
              emptyMessage="This job has never run. Start a backup and it will appear here."
            />
          </div>
        </Card>

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
        />
      </div>
    </Page>
  );
}

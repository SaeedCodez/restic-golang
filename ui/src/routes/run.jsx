import * as React from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { ArrowDownToLine, Play, Square } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { Breadcrumb, Facts, Page, PageHeader } from "@/components/page";
import { Callout } from "@/components/fields";
import { IconTile } from "@/components/kind";
import { StatusBadge } from "@/components/status-badge";
import { LogPanel } from "@/components/log-panel";
import { KIND_ICON, STATUS_TONE } from "@/components/run-history";
import { useConfirm } from "@/components/confirm";
import NotFound from "@/routes/not-found";
import { Runs } from "@/lib/api";
import { errorOf } from "@/lib/api";
import { useRunStream } from "@/lib/live";
import { fmtBytes, fmtCount, fmtDur, fmtTime, runDuration, shortId } from "@/lib/format";
import { STATUS_PHASE, STATUS_TEXT, displayPercent, isActive, kindLabel } from "@/lib/runs";
import { fmtSpeed } from "@/lib/speed";
import { cn } from "@/lib/utils";

function Stat({ label, value, mono }) {
  return (
    <div className="min-w-0 rounded-xl bg-secondary/60 px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div
        className={cn(
          "mt-1 truncate",
          mono ? "font-mono text-sm font-medium" : "display tabular text-lg font-semibold leading-tight",
        )}
      >
        {value}
      </div>
    </div>
  );
}

function SummaryStats({ run }) {
  const s = run.summary;
  if (!s) return null;
  const sp = s.speed;
  const stats =
    run.kind === "speedtest" && sp
      ? [
          { label: "Score", value: `${sp.score} · ${sp.rating}` },
          { label: "Upload", value: fmtSpeed(sp.uploadBytesPerSec) },
          { label: "Download", value: fmtSpeed(sp.downloadBytesPerSec) },
          { label: "Overhead", value: `${Number(sp.overheadSeconds).toFixed(1)} s` },
        ]
      : run.kind === "restore_db"
      ? [
          { label: "Data imported", value: fmtBytes(s.bytesRestored) },
          run.params?.safetySnapshot
            ? { label: "Safety snapshot", value: shortId(run.params.safetySnapshot), mono: true }
            : null,
          s.totalDuration ? { label: "Duration", value: fmtDur(s.totalDuration) } : null,
        ]
      : run.kind === "restore" || run.kind === "download"
      ? [
          { label: "Files restored", value: fmtCount(s.filesRestored) },
          { label: "Data restored", value: fmtBytes(s.bytesRestored) },
          s.totalDuration ? { label: "Duration", value: fmtDur(s.totalDuration) } : null,
        ]
      : [
          { label: "New files", value: fmtCount(s.filesNew) },
          { label: "Changed", value: fmtCount(s.filesChanged) },
          { label: "Unchanged", value: fmtCount(s.filesUnmodified) },
          { label: "Data added", value: fmtBytes(s.dataAdded) },
          s.snapshotId ? { label: "Snapshot", value: shortId(s.snapshotId), mono: true } : null,
          s.totalDuration ? { label: "Duration", value: fmtDur(s.totalDuration) } : null,
        ];
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
      {stats.filter(Boolean).map((st) => (
        <Stat key={st.label} {...st} />
      ))}
    </div>
  );
}

/** A run's bar wears its outcome once it is over; while live it is the running blue. */
const BAR_FILL = {
  success: "bg-success",
  success_warnings: "bg-warning",
  failed: "bg-destructive",
  interrupted: "bg-destructive",
  canceled: "bg-muted-foreground",
};

/** ProgressHero is the "what is happening right now" block. */
function ProgressHero({ run }) {
  const active = isActive(run.status);
  const pct = displayPercent(run);

  // Once a run has a summary, that is the truth about what it processed. restic's
  // last status tick always lands a little short of the total, so a finished run
  // must not read "6 / 8 files" next to 100%. A speed test counts its own
  // progress (test data moved), which its summary does not restate.
  const speed = run.kind === "speedtest";
  const s = speed ? null : run.summary;
  const p = s
    ? {
        ...run.progress,
        filesDone:
          run.kind === "restore" || run.kind === "download"
            ? s.filesRestored
            : s.totalFilesProcessed,
        totalFiles: s.totalFilesProcessed,
        bytesDone:
          run.kind === "restore" || run.kind === "download"
            ? s.bytesRestored
            : s.totalBytesProcessed,
        totalBytes: s.totalBytesProcessed,
        currentFile: "",
      }
    : run.progress || {};
  // restic reports no percentage while it is still scanning, so an active run
  // with nothing to show gets a sweep rather than a stuck-looking empty bar.
  const indeterminate = active && pct === 0 && !p.totalBytes;

  return (
    <div className="space-y-4">
      <div className="flex items-end justify-between gap-4">
        <div className="min-w-0">
          <p className="text-sm text-muted-foreground">Progress</p>
          <p className={cn("mt-1 font-medium", STATUS_TEXT[run.status])}>
            {STATUS_PHASE[run.status] || run.status}
          </p>
        </div>
        <span
          className={cn(
            "display tabular text-[2.75rem] font-semibold leading-none tracking-tight",
            STATUS_TEXT[run.status],
          )}
        >
          {indeterminate ? "—" : Math.round(pct) + "%"}
        </span>
      </div>

      <Progress
        value={pct}
        indeterminate={indeterminate}
        className="h-2.5"
        barClassName={BAR_FILL[run.status]}
      />

      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 tabular text-sm text-muted-foreground">
        <span>
          {run.kind === "restore_db"
            ? "database import"
            : speed
              ? "test data moved"
              : `${fmtCount(p.filesDone)} / ${fmtCount(p.totalFiles)} files`}
        </span>
        <span>
          {fmtBytes(p.bytesDone)} / {fmtBytes(p.totalBytes)}
        </span>
      </div>

      {active ? (
        <p className="h-5 truncate font-mono text-[0.8125rem] text-muted-foreground">
          {p.currentFile ? "▸ " + p.currentFile : ""}
        </p>
      ) : null}
    </div>
  );
}

export default function RunView() {
  const { id } = useParams();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const { run, lines } = useRunStream(id);
  const [initial, setInitial] = React.useState({ loading: true, run: null, missing: false });
  const [stopping, setStopping] = React.useState(false);

  // One fetch up front so a bad id renders "not found" instead of an empty page
  // waiting forever on a stream that will never open.
  React.useEffect(() => {
    let alive = true;
    setInitial({ loading: true, run: null, missing: false });
    Runs.get(id).then((res) => {
      if (!alive) return;
      if (res.status === 404) setInitial({ loading: false, run: null, missing: true });
      else setInitial({ loading: false, run: res.body?.run || null, missing: false });
    });
    return () => {
      alive = false;
    };
  }, [id]);

  const current = run || initial.run;

  React.useEffect(() => {
    if (current) document.title = `${kindLabel(current.kind)} · restic backup manager`;
  }, [current]);

  const stop = async () => {
    const ok = await confirm({
      title: "Stop this operation?",
      description:
        "restic is interrupted cleanly: it releases its lock and writes no partial snapshot. Anything already uploaded stays in the repository.",
      confirmLabel: "Stop it",
      destructive: true,
    });
    if (!ok) return;
    setStopping(true);
    const res = await Runs.stop(id);
    setStopping(false);
    if (!res.ok) toast.error(errorOf(res, "Could not stop this run."));
  };

  if (initial.missing) return <NotFound />;

  if (!current) {
    return (
      <Page>
        <Skeleton className="mb-4 h-5 w-40" />
        <Skeleton className="mb-10 h-10 w-64" />
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
          <Skeleton className="h-56 rounded-2xl" />
          <Skeleton className="h-56 rounded-2xl" />
        </div>
      </Page>
    );
  }

  const active = isActive(current.status);
  const backTo = current.jobId ? `/jobs/${current.jobId}` : "/activity";
  const backLabel = current.jobId ? current.jobName || "Job" : "Activity";
  const Icon = KIND_ICON[current.kind] || Play;

  return (
    <Page>
      <PageHeader
        breadcrumb={<Breadcrumb to={backTo} label={backLabel} current={kindLabel(current.kind)} />}
        icon={<IconTile icon={Icon} tone={STATUS_TONE[current.status] || "neutral"} size="lg" />}
        title={
          <span className="flex flex-wrap items-center gap-x-3.5 gap-y-2">
            {kindLabel(current.kind)}
            <StatusBadge status={current.status} className="text-sm" />
          </span>
        }
        description={
          <span>
            {[current.jobName, current.repoName].filter(Boolean).join(" · ") || "—"}
            <span className="mx-2" aria-hidden="true">
              ·
            </span>
            <span className="tabular">{runDuration(current)}</span>
          </span>
        }
        actions={
          <>
            {current.kind === "download" && current.status === "success" ? (
              <Button asChild>
                <a href={Runs.downloadURL(current.id)}>
                  <ArrowDownToLine />
                  Download .zip
                </a>
              </Button>
            ) : null}
            {active ? (
              <Button variant="destructive" onClick={stop} disabled={stopping}>
                <Square />
                {stopping ? "Stopping…" : "Stop"}
              </Button>
            ) : null}
          </>
        }
      />

      {current.error ? (
        <Callout tone="destructive" title="This run did not finish cleanly" className="mb-6">
          {current.error}
        </Callout>
      ) : null}

      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <Card className={cn(active && "border-running/30")}>
          <CardContent className="pt-6">
            <ProgressHero run={current} />
            {current.summary ? (
              <div className="mt-6 border-t border-border pt-6">
                <SummaryStats run={current} />
              </div>
            ) : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Details</CardTitle>
          </CardHeader>
          <CardContent>
            <Facts
              className="sm:grid-cols-1"
              items={[
                current.jobId
                  ? {
                      label: "Job",
                      value: (
                        <Link to={`/jobs/${current.jobId}`} className="text-primary hover:underline">
                          {current.jobName || "Open job"}
                        </Link>
                      ),
                    }
                  : null,
                current.repositoryId
                  ? {
                      label: "Repository",
                      value: (
                        <Link
                          to={`/repositories/${current.repositoryId}`}
                          className="text-primary hover:underline"
                        >
                          {current.repoName || "Open repository"}
                        </Link>
                      ),
                    }
                  : null,
                current.kind === "backup" && current.folderPath && !current.params?.databaseId
                  ? { label: "Source", value: current.folderPath, mono: true }
                  : null,
                current.kind === "speedtest" && current.params?.sizeBytes
                  ? { label: "Test size", value: `${fmtBytes(Number(current.params.sizeBytes))} each way` }
                  : null,
                { label: "Started", value: fmtTime(current.startedAt) },
                current.finishedAt ? { label: "Finished", value: fmtTime(current.finishedAt) } : null,
                current.params?.target
                  ? { label: "Restored into", value: current.params.target, mono: true }
                  : null,
                current.kind === "restore_db" || current.params?.databaseId
                  ? {
                      label: current.kind === "restore_db" ? "Restored into" : "Database",
                      value: current.folderPath,
                      mono: true,
                    }
                  : null,
                current.exitCode != null && current.exitCode !== 0
                  ? { label: "Exit code", value: String(current.exitCode), mono: true }
                  : null,
              ]}
            />
          </CardContent>
        </Card>
      </div>

      <Card className="mt-6 overflow-hidden">
        <LogPanel lines={lines} />
      </Card>
    </Page>
  );
}

import * as React from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { ArrowDownToLine, ChevronLeft, CircleAlert, Square } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { Page } from "@/components/page";
import { StatusBadge } from "@/components/status-badge";
import { LogPanel } from "@/components/log-panel";
import { RunKindCell } from "@/components/run-history";
import { useConfirm } from "@/components/confirm";
import NotFound from "@/routes/not-found";
import { Runs } from "@/lib/api";
import { errorOf } from "@/lib/api";
import { useRunStream } from "@/lib/live";
import { fmtBytes, fmtCount, fmtDur, fmtTime, runDuration, shortId } from "@/lib/format";
import { STATUS_PHASE, displayPercent, isActive, kindLabel } from "@/lib/runs";

function Stat({ label, value, mono }) {
  return (
    <div className="rounded-lg border border-border bg-surface px-3 py-2.5">
      <div className={mono ? "font-mono text-sm" : "tabular text-lg font-semibold leading-tight"}>
        {value}
      </div>
      <div className="mt-0.5 text-[11px] uppercase tracking-wide text-muted-foreground">
        {label}
      </div>
    </div>
  );
}

function SummaryStats({ run }) {
  const s = run.summary;
  if (!s) return null;
  const stats =
    run.kind === "restore" || run.kind === "download"
      ? [
          { label: "files restored", value: fmtCount(s.filesRestored) },
          { label: "data restored", value: fmtBytes(s.bytesRestored) },
          s.totalDuration ? { label: "duration", value: fmtDur(s.totalDuration) } : null,
        ]
      : [
          { label: "new", value: fmtCount(s.filesNew) },
          { label: "changed", value: fmtCount(s.filesChanged) },
          { label: "unchanged", value: fmtCount(s.filesUnmodified) },
          { label: "data added", value: fmtBytes(s.dataAdded) },
          s.snapshotId ? { label: "snapshot", value: shortId(s.snapshotId), mono: true } : null,
          s.totalDuration ? { label: "duration", value: fmtDur(s.totalDuration) } : null,
        ];
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
      {stats.filter(Boolean).map((st) => (
        <Stat key={st.label} {...st} />
      ))}
    </div>
  );
}

/** ProgressHero is the "what is happening right now" block. */
function ProgressHero({ run }) {
  const active = isActive(run.status);
  const pct = displayPercent(run);

  // Once a run has a summary, that is the truth about what it processed. restic's
  // last status tick always lands a little short of the total, so a finished run
  // must not read "6 / 8 files" next to 100%.
  const s = run.summary;
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
    <div className="space-y-2.5">
      <div className="flex items-baseline justify-between gap-4">
        <span className="text-[13px] text-muted-foreground">
          {STATUS_PHASE[run.status] || run.status}
        </span>
        <span className="tabular text-2xl font-semibold leading-none">
          {indeterminate ? "—" : Math.round(pct) + "%"}
        </span>
      </div>

      <Progress
        value={pct}
        indeterminate={indeterminate}
        barClassName={
          run.status === "failed" || run.status === "interrupted"
            ? "bg-destructive"
            : run.status === "canceled"
              ? "bg-muted-foreground"
              : undefined
        }
      />

      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 tabular text-[13px] text-muted-foreground">
        <span>
          {fmtCount(p.filesDone)} / {fmtCount(p.totalFiles)} files
        </span>
        <span>
          {fmtBytes(p.bytesDone)} / {fmtBytes(p.totalBytes)}
        </span>
      </div>

      <p className="h-4 truncate font-mono text-xs text-muted-foreground">
        {p.currentFile ? "▸ " + p.currentFile : ""}
      </p>
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
        <Skeleton className="mb-4 h-6 w-40" />
        <Card className="p-5">
          <Skeleton className="h-24 w-full" />
        </Card>
      </Page>
    );
  }

  const active = isActive(current.status);
  const backTo = current.jobId ? `/jobs/${current.jobId}` : "/activity";
  const backLabel = current.jobId ? current.jobName || "job" : "Activity";

  return (
    <Page>
      <Link
        to={backTo}
        className="mb-3 inline-flex items-center gap-1 text-[13px] text-muted-foreground transition-colors hover:text-foreground"
      >
        <ChevronLeft className="size-3.5" />
        {backLabel}
      </Link>

      <Card className="mb-4">
        <div className="flex flex-wrap items-start justify-between gap-3 px-5 pt-4">
          <div className="min-w-0">
            <h1 className="flex flex-wrap items-center gap-2.5 text-xl font-semibold tracking-tight">
              <RunKindCell kind={current.kind} />
              <StatusBadge status={current.status} />
            </h1>
            <p className="mt-1 text-[13px] text-muted-foreground">
              Started {fmtTime(current.startedAt)} · {runDuration(current)}
              {current.repoName ? ` · ${current.repoName}` : ""}
            </p>
            {current.params?.target ? (
              <p className="mt-0.5 font-mono text-xs text-muted-foreground">
                → {current.params.target}
              </p>
            ) : null}
          </div>

          <div className="flex shrink-0 items-center gap-2">
            {current.kind === "download" && current.status === "success" ? (
              <Button asChild size="sm">
                <a href={Runs.downloadURL(current.id)}>
                  <ArrowDownToLine />
                  Download .zip
                </a>
              </Button>
            ) : null}
            {active ? (
              <Button variant="destructive" size="sm" onClick={stop} disabled={stopping}>
                <Square />
                {stopping ? "Stopping…" : "Stop"}
              </Button>
            ) : null}
          </div>
        </div>

        {current.error ? (
          <div className="mx-5 mt-4 flex items-start gap-2 rounded-lg bg-destructive/10 px-3 py-2.5 text-[13px] text-destructive">
            <CircleAlert className="mt-0.5 size-4 shrink-0" />
            <span className="break-words">{current.error}</span>
          </div>
        ) : null}

        <div className="px-5 py-4">
          <ProgressHero run={current} />
        </div>

        {current.summary ? (
          <div className="border-t border-border px-5 py-4">
            <SummaryStats run={current} />
          </div>
        ) : null}
      </Card>

      <Card>
        <LogPanel lines={lines} />
      </Card>
    </Page>
  );
}

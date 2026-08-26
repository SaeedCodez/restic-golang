import { Link } from "react-router-dom";
import { Activity as ActivityIcon } from "lucide-react";
import { Card, CardHeader, CardTitle, CardDescription, CardFooter } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { EmptyState } from "@/components/empty";
import { RunKindCell } from "@/components/run-history";
import { StatusBadge } from "@/components/status-badge";
import { useRunStream } from "@/lib/live";
import { fmtRelative, runDuration } from "@/lib/format";
import { displayPercent } from "@/lib/runs";

/** A live row for something happening right now, with its own progress stream. */
export function ActiveRunRow({ run }) {
  const { run: liveRun } = useRunStream(run.id, { withLog: false });
  const live = liveRun || run;
  const pct = displayPercent(live);

  return (
    <Link
      to={`/runs/${live.id}`}
      className="block px-5 py-4 transition-colors hover:bg-accent/40"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 flex-wrap items-center gap-2.5 text-sm font-medium">
          <RunKindCell kind={live.kind} />
          <span className="truncate text-muted-foreground">
            {live.jobName || live.repoName || "—"}
          </span>
          <StatusBadge status={live.status} />
        </div>
        <span className="tabular text-sm font-medium">
          {pct === 0 && !live.progress?.totalBytes ? "—" : Math.round(pct) + "%"}
        </span>
      </div>

      <div className="mt-2.5">
        <Progress value={pct} indeterminate={pct === 0 && !live.progress?.totalBytes} />
      </div>

      <div className="mt-2 flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
        <span className="truncate font-mono">{live.progress?.currentFile || ""}</span>
        <span className="tabular shrink-0">
          started {fmtRelative(live.startedAt)} · {runDuration(live)}
        </span>
      </div>
    </Link>
  );
}

/**
 * ActiveRunsCard lists in-flight operations with live progress.
 * Shared by Activity and Dashboard.
 */
export function ActiveRunsCard({
  runs,
  title = "Running now",
  description = "Operations in progress, updating live.",
  emptyDescription = "Start a backup from a job and it will appear here, with live progress.",
  emptyAction,
  showActivityLink = false,
}) {
  const sorted = runs
    .slice()
    .sort((a, b) => new Date(b.startedAt) - new Date(a.startedAt));

  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1">
          <CardTitle>{title}</CardTitle>
          <CardDescription className="mt-1">{description}</CardDescription>
        </div>
      </CardHeader>
      {sorted.length === 0 ? (
        <div className="px-5 pb-5">
          <EmptyState
            icon={ActivityIcon}
            title="Nothing is running"
            description={emptyDescription}
            action={emptyAction}
            className="py-8"
          />
        </div>
      ) : (
        <div className="divide-y divide-border border-t border-border">
          {sorted.map((run) => (
            <ActiveRunRow key={run.id} run={run} />
          ))}
        </div>
      )}
      {showActivityLink && sorted.length > 0 ? (
        <CardFooter>
          <Link
            to="/activity"
            className="text-[13px] text-muted-foreground hover:text-foreground hover:underline"
          >
            Open Activity →
          </Link>
        </CardFooter>
      ) : null}
    </Card>
  );
}

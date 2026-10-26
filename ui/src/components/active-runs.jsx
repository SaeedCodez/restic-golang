import { Link } from "react-router-dom";
import { Activity as ActivityIcon, Play } from "lucide-react";
import { Card, CardHeader, CardTitle, CardDescription, CardFooter } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { IconTile } from "@/components/kind";
import { SeeAll } from "@/components/page";
import { KIND_ICON } from "@/components/run-history";
import { StatusBadge } from "@/components/status-badge";
import { useRunStream } from "@/lib/live";
import { fmtRelative, runDuration } from "@/lib/format";
import { displayPercent, kindLabel } from "@/lib/runs";

/** A live row for something happening right now, with its own progress stream. */
export function ActiveRunRow({ run }) {
  const { run: liveRun } = useRunStream(run.id, { withLog: false });
  const live = liveRun || run;
  const pct = displayPercent(live);
  const indeterminate = pct === 0 && !live.progress?.totalBytes;
  const Icon = KIND_ICON[live.kind] || Play;

  return (
    <Link
      to={`/runs/${live.id}`}
      className="block px-5 py-5 transition-colors hover:bg-accent/50 sm:px-6"
    >
      <div className="flex items-center gap-4">
        <IconTile icon={Icon} tone="brand" />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1">
            <span className="font-medium">{kindLabel(live.kind)}</span>
            <span className="min-w-0 truncate text-muted-foreground">
              {live.jobName || live.repoName || "—"}
            </span>
            <StatusBadge status={live.status} />
          </div>
          <p className="mt-0.5 tabular text-sm text-muted-foreground">
            started {fmtRelative(live.startedAt)} · {runDuration(live)}
          </p>
        </div>
        <span className="display tabular text-xl font-semibold text-running">
          {indeterminate ? "—" : Math.round(pct) + "%"}
        </span>
      </div>

      <div className="mt-4">
        <Progress value={pct} indeterminate={indeterminate} />
      </div>
      {live.progress?.currentFile ? (
        <p className="mt-2.5 truncate font-mono text-xs text-muted-foreground">
          {live.progress.currentFile}
        </p>
      ) : null}
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
    <Card className={sorted.length > 0 ? "border-running/30" : undefined}>
      <CardHeader>
        <div className="min-w-0 flex-1">
          <CardTitle className="flex items-center gap-2.5">
            {title}
            {sorted.length > 0 ? (
              <span className="rounded-full bg-running/12 px-2 py-0.5 text-xs font-semibold tabular text-running">
                {sorted.length}
              </span>
            ) : null}
          </CardTitle>
          <CardDescription className="mt-1">{description}</CardDescription>
        </div>
      </CardHeader>
      {sorted.length === 0 ? (
        <div className="flex flex-wrap items-center gap-4 border-t border-border px-6 py-5">
          <IconTile icon={ActivityIcon} />
          <div className="min-w-0 flex-1">
            <p className="font-medium">Nothing is running</p>
            <p className="mt-0.5 text-sm text-muted-foreground">{emptyDescription}</p>
          </div>
          {emptyAction}
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
          <SeeAll to="/activity">Open Activity</SeeAll>
        </CardFooter>
      ) : null}
    </Card>
  );
}

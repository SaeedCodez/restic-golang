import * as React from "react";
import { Link } from "react-router-dom";
import { Activity as ActivityIcon, History, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/components/empty";
import { Page, PageHeader } from "@/components/page";
import { RunHistory, RunKindCell } from "@/components/run-history";
import { StatusBadge } from "@/components/status-badge";
import { Runs } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive, useRunStream } from "@/lib/live";
import { fmtRelative, runDuration } from "@/lib/format";
import { displayPercent } from "@/lib/runs";

const HISTORY_LIMIT = 40;

/** A live row for something happening right now, with its own progress stream. */
function ActiveRunRow({ run }) {
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

const KIND_FILTERS = [
  { value: "all", label: "All operations" },
  { value: "backup", label: "Backups" },
  { value: "restore", label: "Restores" },
  { value: "download", label: "Downloads" },
  { value: "init", label: "Initializations" },
];

const STATUS_FILTERS = [
  { value: "all", label: "Any outcome" },
  { value: "success", label: "Succeeded" },
  { value: "success_warnings", label: "With warnings" },
  { value: "failed", label: "Failed" },
  { value: "canceled", label: "Stopped" },
  { value: "interrupted", label: "Interrupted" },
];

export default function ActivityRoute() {
  const { activeRuns, runsVersion } = useLive();
  const [kind, setKind] = React.useState("all");
  const [status, setStatus] = React.useState("all");

  const loader = React.useCallback(async () => {
    const res = await Runs.list({
      status: status === "all" ? "finished" : status,
      kind: kind === "all" ? "" : kind,
      limit: HISTORY_LIMIT,
    });
    return { runs: res.body?.runs || [], total: res.body?.total || 0 };
  }, [kind, status]);
  const { data, loading, reload } = useFetch(loader);

  React.useEffect(() => {
    if (runsVersion > 0) reload();
  }, [runsVersion, reload]);

  React.useEffect(() => {
    document.title = "Activity · restic backup manager";
  }, []);

  const history = data?.runs || [];
  const total = data?.total || 0;

  return (
    <Page>
      <PageHeader
        title="Activity"
        description="Everything this app has run. Live state comes from the server, so a refresh, a second tab or coming back tomorrow all show the same thing."
        actions={
          <Button variant="outline" size="sm" onClick={reload} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : undefined} />
            Refresh
          </Button>
        }
      />

      <div className="space-y-4">
        <Card>
          <CardHeader>
            <div className="min-w-0 flex-1">
              <CardTitle>Running now</CardTitle>
              <CardDescription className="mt-1">
                Operations in progress, updating live.
              </CardDescription>
            </div>
          </CardHeader>
          {activeRuns.length === 0 ? (
            <div className="px-5 pb-5">
              <EmptyState
                icon={ActivityIcon}
                title="Nothing is running"
                description="Start a backup from a job and it will appear here, with live progress."
              />
            </div>
          ) : (
            <div className="divide-y divide-border border-t border-border">
              {activeRuns
                .slice()
                .sort((a, b) => new Date(b.startedAt) - new Date(a.startedAt))
                .map((run) => (
                  <ActiveRunRow key={run.id} run={run} />
                ))}
            </div>
          )}
        </Card>

        <Card>
          <CardHeader>
            <div className="min-w-0 flex-1">
              <CardTitle>History</CardTitle>
              <CardDescription className="mt-1">
                {total > history.length
                  ? `Showing the ${history.length} most recent of ${total} finished operations.`
                  : "Every finished operation, newest first."}
              </CardDescription>
            </div>
            <div className="flex flex-wrap gap-2">
              <Select value={kind} onValueChange={setKind}>
                <SelectTrigger className="h-8 w-auto min-w-[9.5rem] text-[13px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {KIND_FILTERS.map((f) => (
                    <SelectItem key={f.value} value={f.value}>
                      {f.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={status} onValueChange={setStatus}>
                <SelectTrigger className="h-8 w-auto min-w-[9rem] text-[13px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {STATUS_FILTERS.map((f) => (
                    <SelectItem key={f.value} value={f.value}>
                      {f.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </CardHeader>

          <div className="border-t border-border">
            {loading && !data ? (
              <div className="space-y-2 p-5">
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
              </div>
            ) : history.length === 0 ? (
              <div className="p-5">
                <EmptyState
                  icon={History}
                  title="Nothing here yet"
                  description={
                    kind === "all" && status === "all"
                      ? "Finished backups, restores and downloads are listed here."
                      : "No finished operations match these filters."
                  }
                />
              </div>
            ) : (
              <RunHistory runs={history} showContext />
            )}
          </div>
        </Card>
      </div>
    </Page>
  );
}

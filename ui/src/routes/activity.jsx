import * as React from "react";
import { History, Layers, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorState } from "@/components/empty";
import { ListPagination } from "@/components/pagination";
import { Page, PageHeader } from "@/components/page";
import { ActiveRunsCard } from "@/components/active-runs";
import { KIND_ICON, RunHistory } from "@/components/run-history";
import { StatusDot } from "@/components/status-badge";
import { Runs } from "@/lib/api";
import { PAGE_SIZE } from "@/lib/pagination";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";

const KIND_FILTERS = [
  { value: "all", label: "All operations", icon: Layers },
  { value: "backup", label: "Backups", icon: KIND_ICON.backup },
  { value: "retention", label: "Retention", icon: KIND_ICON.retention },
  { value: "forget", label: "Forget", icon: KIND_ICON.forget },
  { value: "restore", label: "Restores", icon: KIND_ICON.restore },
  { value: "download", label: "Downloads", icon: KIND_ICON.download },
  { value: "init", label: "Initializations", icon: KIND_ICON.init },
  { value: "check", label: "Checks", icon: KIND_ICON.check },
  { value: "speedtest", label: "Speed tests", icon: KIND_ICON.speedtest },
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
  const [page, setPage] = React.useState(1);

  const loader = React.useCallback(async () => {
    const res = await Runs.list({
      status: status === "all" ? "finished" : status,
      kind: kind === "all" ? "" : kind,
      limit: PAGE_SIZE,
      offset: (page - 1) * PAGE_SIZE,
    });
    if (!res.ok) throw new Error(res.body?.error || "Could not load the history.");
    return { runs: res.body?.runs || [], total: res.body?.total || 0 };
  }, [kind, status, page]);
  const { data, loading, error, reload } = useFetch(loader);

  React.useEffect(() => {
    if (runsVersion > 0) reload();
  }, [runsVersion, reload]);

  // Filters change the result set — return to the first page.
  React.useEffect(() => {
    setPage(1);
  }, [kind, status]);

  React.useEffect(() => {
    if (!data) return;
    const totalPages = Math.max(1, Math.ceil((data.total || 0) / PAGE_SIZE));
    if (page > totalPages) setPage(totalPages);
  }, [data, page]);

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
          <Button variant="outline" onClick={reload} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : undefined} />
            Refresh
          </Button>
        }
      />

      <div className="space-y-6">
        <ActiveRunsCard runs={activeRuns} />

        <Card>
          <CardHeader>
            <div className="min-w-0 flex-1">
              <CardTitle className="flex items-center gap-2.5">
                History
                {data ? (
                  <span className="rounded-full bg-secondary px-2 py-0.5 text-xs font-semibold tabular text-muted-foreground">
                    {total}
                  </span>
                ) : null}
              </CardTitle>
              <CardDescription className="mt-1">
                {total > history.length
                  ? `Page ${page} · ${total} finished operations matching these filters.`
                  : "Every finished operation, newest first."}
              </CardDescription>
            </div>
            <div className="grid w-full grid-cols-2 gap-2 sm:flex sm:w-auto">
              <Select value={kind} onValueChange={setKind}>
                <SelectTrigger className="w-full text-sm sm:w-auto sm:min-w-[11.5rem]" aria-label="Operation">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {KIND_FILTERS.map((f) => (
                    <SelectItem key={f.value} value={f.value}>
                      <span className="flex items-center gap-2.5">
                        <f.icon className="size-4 shrink-0 text-muted-foreground" />
                        {f.label}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={status} onValueChange={setStatus}>
                <SelectTrigger className="w-full text-sm sm:w-auto sm:min-w-[10.5rem]" aria-label="Outcome">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {STATUS_FILTERS.map((f) => (
                    <SelectItem key={f.value} value={f.value}>
                      <span className="flex items-center gap-2.5">
                        {f.value === "all" ? (
                          <span className="size-2 rounded-full border border-muted-foreground/60" />
                        ) : (
                          <StatusDot status={f.value} />
                        )}
                        {f.label}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </CardHeader>

          <div className="border-t border-border">
            {error && !data ? (
              <div className="p-5 sm:p-6">
                <ErrorState
                  title="Could not load the history"
                  description={`${error.message} Check that the server is running, then retry.`}
                  onRetry={reload}
                />
              </div>
            ) : loading && !data ? (
              <div className="divide-y divide-border">
                {[0, 1, 2, 3].map((i) => (
                  <div key={i} className="flex items-center gap-4 px-5 py-4 sm:px-6">
                    <Skeleton className="size-10 rounded-xl" />
                    <div className="flex-1 space-y-2">
                      <Skeleton className="h-4 w-48" />
                      <Skeleton className="h-3.5 w-72 max-w-full" />
                    </div>
                  </div>
                ))}
              </div>
            ) : history.length === 0 ? (
              <EmptyState
                icon={History}
                title={kind === "all" && status === "all" ? "Nothing here yet" : "No matches"}
                description={
                  kind === "all" && status === "all"
                    ? "Finished backups, restores and downloads are listed here."
                    : "No finished operations match these filters."
                }
                action={
                  kind === "all" && status === "all" ? null : (
                    <Button
                      variant="outline"
                      onClick={() => {
                        setKind("all");
                        setStatus("all");
                      }}
                    >
                      Clear filters
                    </Button>
                  )
                }
              />
            ) : (
              <RunHistory runs={history} showContext groupByDay />
            )}
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
      </div>
    </Page>
  );
}

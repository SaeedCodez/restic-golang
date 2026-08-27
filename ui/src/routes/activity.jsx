import * as React from "react";
import { History, RefreshCw } from "lucide-react";
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
import { EmptyState } from "@/components/empty";
import { ListPagination } from "@/components/pagination";
import { Page, PageHeader } from "@/components/page";
import { ActiveRunsCard } from "@/components/active-runs";
import { RunHistory } from "@/components/run-history";
import { Runs } from "@/lib/api";
import { PAGE_SIZE } from "@/lib/pagination";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";

const KIND_FILTERS = [
  { value: "all", label: "All operations" },
  { value: "backup", label: "Backups" },
  { value: "retention", label: "Retention" },
  { value: "forget", label: "Forget" },
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
  const [page, setPage] = React.useState(1);

  const loader = React.useCallback(async () => {
    const res = await Runs.list({
      status: status === "all" ? "finished" : status,
      kind: kind === "all" ? "" : kind,
      limit: PAGE_SIZE,
      offset: (page - 1) * PAGE_SIZE,
    });
    return { runs: res.body?.runs || [], total: res.body?.total || 0 };
  }, [kind, status, page]);
  const { data, loading, reload } = useFetch(loader);

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
          <Button variant="outline" size="sm" onClick={reload} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : undefined} />
            Refresh
          </Button>
        }
      />

      <div className="space-y-4">
        <ActiveRunsCard runs={activeRuns} />

        <Card>
          <CardHeader>
            <div className="min-w-0 flex-1">
              <CardTitle>History</CardTitle>
              <CardDescription className="mt-1">
                {total > history.length
                  ? `Page ${page} · ${total} finished operations matching these filters.`
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
    </Page>
  );
}

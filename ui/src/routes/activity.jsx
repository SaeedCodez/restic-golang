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
import { Page, PageHeader } from "@/components/page";
import { ActiveRunsCard } from "@/components/active-runs";
import { RunHistory } from "@/components/run-history";
import { Runs } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";

const HISTORY_LIMIT = 40;

const KIND_FILTERS = [
  { value: "all", label: "All operations" },
  { value: "backup", label: "Backups" },
  { value: "retention", label: "Retention" },
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
        <ActiveRunsCard runs={activeRuns} />

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

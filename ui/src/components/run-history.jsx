import { Link } from "react-router-dom";
import {
  ArrowDownToLine,
  ChevronRight,
  Database,
  DatabaseBackup,
  Eraser,
  FolderInput,
  History,
  Play,
  Scissors,
  ShieldCheck,
  Unlock,
} from "lucide-react";
import { StatusBadge } from "@/components/status-badge";
import { IconTile } from "@/components/kind";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { fmtBytes, fmtRelative, fmtTime, runDuration, shortId } from "@/lib/format";
import { kindLabel } from "@/lib/runs";
import { cn } from "@/lib/utils";

export const KIND_ICON = {
  backup: Play,
  restore: FolderInput,
  init: Database,
  download: ArrowDownToLine,
  unlock: Unlock,
  retention: Scissors,
  forget: Eraser,
  check: ShieldCheck,
  restore_db: DatabaseBackup,
};

/** A run's tile wears its outcome, so a list of runs reads at a glance. */
export const STATUS_TONE = {
  starting: "brand",
  running: "brand",
  success: "success",
  success_warnings: "warning",
  failed: "destructive",
  interrupted: "destructive",
  canceled: "neutral",
};

/** What a run actually stored or restored, in one short phrase. */
function storedSummary(run) {
  const s = run.summary;
  if (!s) return null;
  if (run.kind === "restore" || run.kind === "download") {
    return `${fmtBytes(s.bytesRestored)} restored`;
  }
  const parts = [];
  if (s.dataAdded) parts.push(`${fmtBytes(s.dataAdded)} added`);
  if (s.snapshotId) parts.push(`snapshot ${shortId(s.snapshotId)}`);
  return parts.join(" · ") || null;
}

export function RunKindCell({ kind }) {
  const Icon = KIND_ICON[kind] || Play;
  return (
    <span className="flex items-center gap-2 whitespace-nowrap">
      <Icon className="size-4 shrink-0 text-muted-foreground" />
      {kindLabel(kind)}
    </span>
  );
}

/** RunRow is one run in a list: what it was, how it ended, when and what it did. */
export function RunRow({ run, showContext = false }) {
  const Icon = KIND_ICON[run.kind] || Play;
  const stored = storedSummary(run);
  const context = run.jobName || run.repoName;

  return (
    <Link
      to={`/runs/${run.id}`}
      className="group grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-4 px-5 py-4 transition-colors hover:bg-accent/50 focus-visible:bg-accent/50 focus-visible:outline-none sm:px-6"
    >
      <IconTile icon={Icon} tone={STATUS_TONE[run.status] || "neutral"} />
      <div className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1">
          <span className="font-medium">{kindLabel(run.kind)}</span>
          {showContext && context ? (
            <span className="min-w-0 truncate text-muted-foreground">{context}</span>
          ) : null}
          <StatusBadge status={run.status} />
        </div>
        <p
          className={cn(
            "mt-1 truncate text-sm",
            run.error ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {run.error ? run.error : <span className="font-mono text-[0.8125rem]">{stored || "—"}</span>}
        </p>
      </div>
      <div className="flex items-center gap-3">
        <div className="text-right text-sm">
          <Tooltip>
            <TooltipTrigger asChild>
              <p className="whitespace-nowrap text-foreground">{fmtRelative(run.startedAt)}</p>
            </TooltipTrigger>
            <TooltipContent>{fmtTime(run.startedAt)}</TooltipContent>
          </Tooltip>
          <p className="tabular whitespace-nowrap text-xs text-muted-foreground">
            {runDuration(run)}
          </p>
        </div>
        <ChevronRight className="hidden size-4 text-muted-foreground/50 transition-transform group-hover:translate-x-0.5 group-hover:text-muted-foreground sm:block" />
      </div>
    </Link>
  );
}

/**
 * RunHistory lists runs newest-first. Every row navigates to that run, where the
 * full durable log is available — including for runs that finished days ago.
 */
export function RunHistory({ runs, showContext = false, emptyMessage = "No runs yet." }) {
  if (!runs.length) {
    return (
      <div className="flex flex-col items-center gap-3 px-6 py-10 text-center">
        <IconTile icon={History} />
        <p className="text-sm text-muted-foreground">{emptyMessage}</p>
      </div>
    );
  }

  return (
    <div className="divide-y divide-border">
      {runs.map((run) => (
        <RunRow key={run.id} run={run} showContext={showContext} />
      ))}
    </div>
  );
}

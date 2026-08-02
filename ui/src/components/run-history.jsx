import { useNavigate } from "react-router-dom";
import { ArrowDownToLine, Database, FolderInput, Play, Unlock } from "lucide-react";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { StatusBadge } from "@/components/status-badge";
import { Mono } from "@/components/page";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { fmtBytes, fmtRelative, fmtTime, runDuration, shortId } from "@/lib/format";
import { kindLabel } from "@/lib/runs";
import { cn } from "@/lib/utils";

const KIND_ICON = {
  backup: Play,
  restore: FolderInput,
  init: Database,
  download: ArrowDownToLine,
  unlock: Unlock,
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
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      {kindLabel(kind)}
    </span>
  );
}

/**
 * RunHistory lists runs newest-first. Every row navigates to that run, where the
 * full durable log is available — including for runs that finished days ago.
 */
export function RunHistory({ runs, showContext = false, emptyMessage = "No runs yet." }) {
  const navigate = useNavigate();

  if (!runs.length) {
    return (
      <p className="px-5 py-8 text-center text-[13px] text-muted-foreground">{emptyMessage}</p>
    );
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Operation</TableHead>
          <TableHead>Status</TableHead>
          {showContext && <TableHead>Job / repository</TableHead>}
          <TableHead>Started</TableHead>
          <TableHead>Duration</TableHead>
          <TableHead>Result</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {runs.map((run) => (
          <TableRow
            key={run.id}
            tabIndex={0}
            role="link"
            onClick={() => navigate(`/runs/${run.id}`)}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                navigate(`/runs/${run.id}`);
              }
            }}
            className={cn(
              "cursor-pointer transition-colors hover:bg-accent/50",
              "focus-visible:bg-accent/50 focus-visible:outline-none",
            )}
          >
            <TableCell className="font-medium">
              <RunKindCell kind={run.kind} />
            </TableCell>
            <TableCell>
              <StatusBadge status={run.status} />
            </TableCell>
            {showContext && (
              <TableCell className="max-w-[16rem] truncate text-muted-foreground">
                {run.jobName || run.repoName || "—"}
              </TableCell>
            )}
            <TableCell className="whitespace-nowrap text-muted-foreground">
              <Tooltip>
                <TooltipTrigger asChild>
                  <span>{fmtRelative(run.startedAt)}</span>
                </TooltipTrigger>
                <TooltipContent>{fmtTime(run.startedAt)}</TooltipContent>
              </Tooltip>
            </TableCell>
            <TableCell className="whitespace-nowrap tabular text-muted-foreground">
              {runDuration(run)}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {run.error ? (
                <span className="text-destructive">{run.error}</span>
              ) : (
                <Mono className="text-muted-foreground">{storedSummary(run) || "—"}</Mono>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

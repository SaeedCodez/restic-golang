import { Link } from "react-router-dom";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { fmtRelative } from "@/lib/format";
import { STATUS_FILL, isActive, statusLabel } from "@/lib/runs";
import { cn } from "@/lib/utils";

/**
 * RunStrip draws a job's recent backups as a row of bars, the newest on the left
 * and older ones to the right, one colour per outcome. A run of green with one
 * red bar answers "has this been backing up?" faster than any table. Unused
 * slots trail on the right as faint placeholders, so strips line up across cards.
 *
 * `runs` is newest-first, the order the API returns, which is also the order drawn.
 */
export function RunStrip({ runs = [], slots = 24, className }) {
  const shown = runs.slice(0, slots);
  const empty = Math.max(0, slots - shown.length);

  return (
    <div
      className={cn("flex h-7 items-stretch gap-[3px]", className)}
      role="list"
      aria-label={`Last ${shown.length} backups`}
    >
      {shown.map((run) => (
        <Tooltip key={run.id}>
          <TooltipTrigger asChild>
            <Link
              to={`/runs/${run.id}`}
              role="listitem"
              aria-label={`${statusLabel(run.status)}, ${fmtRelative(run.startedAt)}`}
              className={cn(
                "w-full max-w-2 flex-1 rounded-full transition-[opacity,transform] hover:scale-y-110 hover:opacity-80",
                STATUS_FILL[run.status] || "bg-muted-foreground/45",
                isActive(run.status) && "animate-pulse",
              )}
            />
          </TooltipTrigger>
          <TooltipContent>
            {statusLabel(run.status)} · {fmtRelative(run.startedAt)}
          </TooltipContent>
        </Tooltip>
      ))}
      {Array.from({ length: empty }, (_, i) => (
        <span key={`e${i}`} className="w-full max-w-2 flex-1 rounded-full bg-muted" aria-hidden="true" />
      ))}
    </div>
  );
}

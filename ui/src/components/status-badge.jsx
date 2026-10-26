import { Badge } from "@/components/ui/badge";
import { STATUS_FILL, STATUS_VARIANT, isActive, statusLabel } from "@/lib/runs";
import { cn } from "@/lib/utils";

/** StatusDot is the coloured dot for a run status; it pulses while the run is live. */
export function StatusDot({ status, className }) {
  const fill = STATUS_FILL[status] || "bg-muted-foreground/45";
  return (
    <span className={cn("relative flex size-2 shrink-0", className)} aria-hidden="true">
      {isActive(status) ? (
        <span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-60", fill)} />
      ) : null}
      <span className={cn("relative inline-flex size-2 rounded-full", fill)} />
    </span>
  );
}

export function StatusBadge({ status, className }) {
  return (
    <Badge variant={STATUS_VARIANT[status] || "secondary"} className={className}>
      <StatusDot status={status} className="size-1.5 [&>span]:size-1.5" />
      {statusLabel(status)}
    </Badge>
  );
}

import { Badge } from "@/components/ui/badge";
import { STATUS_VARIANT, isActive, statusLabel } from "@/lib/runs";
import { cn } from "@/lib/utils";

/** A pulsing dot marks a run that is genuinely still in progress. */
function Dot({ active, className }) {
  return (
    <span className={cn("relative flex size-1.5", className)}>
      {active && (
        <span className="absolute inline-flex size-full animate-ping rounded-full bg-current opacity-60" />
      )}
      <span className="relative inline-flex size-1.5 rounded-full bg-current" />
    </span>
  );
}

export function StatusBadge({ status, className }) {
  const active = isActive(status);
  return (
    <Badge variant={STATUS_VARIANT[status] || "secondary"} className={className}>
      <Dot active={active} />
      {statusLabel(status)}
    </Badge>
  );
}

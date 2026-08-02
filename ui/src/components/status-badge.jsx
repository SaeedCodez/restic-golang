import { Badge } from "@/components/ui/badge";
import { STATUS_VARIANT, isActive, statusLabel } from "@/lib/runs";
import { cn } from "@/lib/utils";

/** Only a run that is genuinely still in progress gets a dot, and it pulses. */
function Dot({ className }) {
  return (
    <span className={cn("relative flex size-1", className)}>
      <span className="absolute inline-flex size-full animate-ping rounded-full bg-current opacity-70" />
      <span className="relative inline-flex size-1 rounded-full bg-current" />
    </span>
  );
}

export function StatusBadge({ status, className }) {
  return (
    <Badge variant={STATUS_VARIANT[status] || "secondary"} className={className}>
      {isActive(status) ? <Dot /> : null}
      {statusLabel(status)}
    </Badge>
  );
}

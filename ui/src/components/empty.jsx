import { RotateCw, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { IconTile } from "@/components/kind";
import { cn } from "@/lib/utils";

/**
 * EmptyState is what a screen shows before it has anything to show. It always
 * says what the thing is and what to do next, rather than just "nothing here".
 * `framed` draws it as a dashed drop-zone card, for a page whose whole list is
 * empty; unframed it sits quietly inside an existing card.
 */
export function EmptyState({
  icon,
  tone = "neutral",
  title,
  description,
  action,
  framed = false,
  className,
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center px-6 py-12 text-center",
        framed && "rounded-2xl border border-dashed border-input bg-card/60 py-16",
        className,
      )}
    >
      {icon ? <IconTile icon={icon} tone={tone} size="lg" className="mb-4" /> : null}
      <p className="display text-md font-semibold">{title}</p>
      {description ? (
        <p className="mx-auto mt-1.5 max-w-md text-sm text-muted-foreground">{description}</p>
      ) : null}
      {action ? <div className="mt-5 flex flex-wrap justify-center gap-2">{action}</div> : null}
    </div>
  );
}

/**
 * ErrorState reports a failed load in place of the content, with what went
 * wrong and a way to try again. It never apologizes and never hides the reason.
 */
export function ErrorState({
  title = "Could not load this page",
  description,
  onRetry,
  className,
}) {
  return (
    <div
      role="alert"
      className={cn(
        "flex flex-wrap items-center gap-4 rounded-2xl border border-destructive/25 bg-destructive/6 px-5 py-4",
        className,
      )}
    >
      <IconTile icon={TriangleAlert} tone="destructive" />
      <div className="min-w-0 flex-1">
        <p className="font-medium">{title}</p>
        <p className="mt-0.5 text-sm text-muted-foreground">
          {description || "The server did not answer. Check that restic-web is running, then retry."}
        </p>
      </div>
      {onRetry ? (
        <Button variant="outline" size="sm" onClick={onRetry}>
          <RotateCw />
          Retry
        </Button>
      ) : null}
    </div>
  );
}

/** InlineError is a form-level or action-level error message. */
export function InlineError({ children, className }) {
  if (!children) return null;
  return (
    <p
      role="alert"
      className={cn(
        "flex items-start gap-2 rounded-xl bg-destructive/8 px-3.5 py-2.5 text-sm text-destructive",
        className,
      )}
    >
      <TriangleAlert className="mt-0.5 size-4 shrink-0" />
      <span className="min-w-0">{children}</span>
    </p>
  );
}

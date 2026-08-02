import { cn } from "@/lib/utils";

/**
 * EmptyState is what a screen shows before it has anything to show. It always
 * says what the thing is and what to do next, rather than just "nothing here".
 */
export function EmptyState({ icon: Icon, title, description, action, className }) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-2 px-6 py-12 text-center",
        className,
      )}
    >
      {Icon ? <Icon className="size-4 text-muted-foreground/60" /> : null}
      <p className="text-[13px] font-medium">{title}</p>
      {description ? (
        <p className="mx-auto max-w-sm text-[13px] leading-relaxed text-muted-foreground">
          {description}
        </p>
      ) : null}
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  );
}

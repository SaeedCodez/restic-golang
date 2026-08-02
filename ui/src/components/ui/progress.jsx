import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * Progress is a determinate bar. `indeterminate` renders a sliding sheen for
 * work that has started but has not reported a percentage yet (restic emits no
 * status lines while it is scanning).
 */
const Progress = React.forwardRef(
  ({ className, value = 0, indeterminate = false, barClassName, ...props }, ref) => {
    const pct = Math.max(0, Math.min(100, Number(value) || 0));
    return (
      <div
        ref={ref}
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={indeterminate ? undefined : Math.round(pct)}
        className={cn(
          "relative h-1 w-full overflow-hidden rounded-full bg-muted",
          className,
        )}
        {...props}
      >
        {indeterminate ? (
          <div className="absolute inset-y-0 w-1/3 animate-[progress-sweep_1.4s_ease-in-out_infinite] rounded-full bg-foreground/50" />
        ) : (
          <div
            className={cn(
              "h-full rounded-full bg-foreground transition-[width] duration-300 ease-out",
              barClassName,
            )}
            style={{ width: pct + "%" }}
          />
        )}
      </div>
    );
  },
);
Progress.displayName = "Progress";

export { Progress };

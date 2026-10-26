import * as React from "react";
import { toast } from "sonner";
import { ArrowDown, Check, Copy, TerminalSquare } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

const LEVEL_CLASS = {
  error: "text-destructive",
  warn: "text-warning",
  ok: "text-success",
  system: "text-info",
  info: "text-muted-foreground",
};

/** The wall-clock time of a log line, HH:MM:SS, in the browser's time zone. */
function lineTime(ts) {
  if (!ts) return "";
  const d = new Date(ts);
  if (isNaN(d.getTime())) return "";
  return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
}

const FILTERS = [
  { key: "all", label: "All" },
  { key: "problems", label: "Problems" },
];

/**
 * LogPanel renders a run's durable log.
 *
 * Autoscroll follows the tail only while the user is already at the bottom —
 * scrolling up to read something is not fought by the next incoming line, and
 * scrolling back down resumes following.
 */
export function LogPanel({ lines, className }) {
  const [filter, setFilter] = React.useState("all");
  const [following, setFollowing] = React.useState(true);
  const [copied, setCopied] = React.useState(false);
  const scroller = React.useRef(null);

  const shown = React.useMemo(
    () =>
      filter === "problems"
        ? lines.filter((l) => l.level === "error" || l.level === "warn")
        : lines,
    [lines, filter],
  );

  // Follow the tail on each new line, but only while the user has not scrolled up.
  React.useLayoutEffect(() => {
    const el = scroller.current;
    if (!el || !following) return;
    el.scrollTop = el.scrollHeight;
  }, [shown, following]);

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    setFollowing(atBottom);
  };

  const copy = async () => {
    const text = shown.map((l) => l.message).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("Could not copy to the clipboard.");
    }
  };

  const problemCount = React.useMemo(
    () => lines.filter((l) => l.level === "error" || l.level === "warn").length,
    [lines],
  );

  return (
    <div className={cn("flex flex-col", className)}>
      <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4 sm:px-6">
        <div className="flex items-center gap-2.5">
          <TerminalSquare className="size-5 text-muted-foreground" />
          <h2 className="display text-md font-semibold">Log</h2>
          <span className="rounded-full bg-secondary px-2 py-0.5 text-xs font-semibold tabular text-muted-foreground">
            {lines.length} line{lines.length === 1 ? "" : "s"}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex gap-1 rounded-xl bg-secondary p-1" role="radiogroup" aria-label="Show">
            {FILTERS.map((f) => (
              <button
                key={f.key}
                type="button"
                role="radio"
                aria-checked={filter === f.key}
                onClick={() => setFilter(f.key)}
                className={cn(
                  "flex h-8 items-center gap-1.5 rounded-lg px-3 text-sm font-medium transition-colors",
                  filter === f.key
                    ? "bg-card text-foreground shadow-[0_1px_2px_rgb(0_0_0/0.08)]"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {f.label}
                {f.key === "problems" && problemCount > 0 ? (
                  <span className="rounded-full bg-warning/15 px-1.5 text-xs tabular text-warning">
                    {problemCount}
                  </span>
                ) : null}
              </button>
            ))}
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="icon-sm" onClick={copy} aria-label="Copy log">
                {copied ? <Check className="text-success" /> : <Copy />}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{copied ? "Copied" : "Copy the lines shown"}</TooltipContent>
          </Tooltip>
        </div>
      </div>

      <div className="relative">
        <div
          ref={scroller}
          onScroll={onScroll}
          className="scroll-thin max-h-[30rem] min-h-[9rem] overflow-auto border-t border-border bg-log-bg py-3 font-mono text-[0.8125rem] leading-relaxed"
          role="log"
          aria-live="polite"
        >
          {shown.length === 0 ? (
            <p className="px-5 text-muted-foreground sm:px-6">
              {lines.length === 0 ? "Waiting for output…" : "No warnings or errors."}
            </p>
          ) : (
            shown.map((line, i) => (
              <div
                key={line.seq || i}
                className={cn(
                  "grid grid-cols-[minmax(0,1fr)] gap-x-4 px-5 hover:bg-foreground/[0.03] sm:grid-cols-[4.75rem_minmax(0,1fr)] sm:px-6",
                  (line.level === "error" || line.level === "warn") && "bg-current/[0.04]",
                  LEVEL_CLASS[line.level],
                )}
              >
                <span className="hidden select-none text-muted-foreground/60 tabular sm:block">
                  {lineTime(line.ts)}
                </span>
                <span className="whitespace-pre-wrap break-words">{line.message}</span>
              </div>
            ))
          )}
        </div>

        {!following && (
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              setFollowing(true);
              const el = scroller.current;
              if (el) el.scrollTop = el.scrollHeight;
            }}
            className="absolute bottom-4 left-1/2 -translate-x-1/2 shadow-lg"
          >
            <ArrowDown />
            Jump to latest
          </Button>
        )}
      </div>
    </div>
  );
}

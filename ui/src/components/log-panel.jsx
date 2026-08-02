import * as React from "react";
import { toast } from "sonner";
import { ArrowDown, Check, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const LEVEL_CLASS = {
  error: "text-destructive",
  warn: "text-warning",
  ok: "text-success",
  system: "text-info",
  info: "text-muted-foreground",
};

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
      <div className="flex flex-wrap items-center justify-between gap-2 px-5 py-3">
        <div className="flex items-center gap-2">
          <h2 className="text-[15px] font-semibold tracking-tight">Log</h2>
          <span className="tabular text-xs text-muted-foreground">
            {lines.length} line{lines.length === 1 ? "" : "s"}
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="flex rounded-md border border-border p-0.5">
            {FILTERS.map((f) => (
              <button
                key={f.key}
                type="button"
                onClick={() => setFilter(f.key)}
                className={cn(
                  "rounded px-2 py-1 text-xs font-medium transition-colors",
                  filter === f.key
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {f.label}
                {f.key === "problems" && problemCount > 0 ? (
                  <span className="ml-1 tabular text-warning">{problemCount}</span>
                ) : null}
              </button>
            ))}
          </div>
          <Button variant="ghost" size="icon-sm" onClick={copy} aria-label="Copy log">
            {copied ? <Check className="text-success" /> : <Copy />}
          </Button>
        </div>
      </div>

      <div className="relative">
        <pre
          ref={scroller}
          onScroll={onScroll}
          className="scroll-thin m-0 max-h-[26rem] min-h-[8rem] overflow-auto whitespace-pre-wrap break-words border-t border-border bg-log-bg px-4 py-3 font-mono text-xs leading-relaxed"
        >
          {shown.length === 0 ? (
            <span className="text-muted-foreground">
              {lines.length === 0 ? "Waiting for output…" : "No warnings or errors."}
            </span>
          ) : (
            shown.map((line, i) => (
              <span key={line.seq || i} className={cn("block", LEVEL_CLASS[line.level])}>
                {line.message}
              </span>
            ))
          )}
        </pre>

        {!following && (
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              setFollowing(true);
              const el = scroller.current;
              if (el) el.scrollTop = el.scrollHeight;
            }}
            className="absolute bottom-3 left-1/2 -translate-x-1/2 shadow-lg"
          >
            <ArrowDown />
            Jump to latest
          </Button>
        )}
      </div>
    </div>
  );
}

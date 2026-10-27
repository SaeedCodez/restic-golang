import * as React from "react";
import { Link, useNavigate } from "react-router-dom";
import { ArrowDown, ArrowUp, Gauge, LoaderCircle, Timer } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { Segmented } from "@/components/fields";
import { IconTile } from "@/components/kind";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Repositories } from "@/lib/api";
import { useLive, useRunStream } from "@/lib/live";
import { useFetch } from "@/lib/use-fetch";
import { fmtBytes, fmtRelative, fmtTime } from "@/lib/format";
import { displayPercent } from "@/lib/runs";
import { handleStartResponse } from "@/lib/start-run";
import { estimateSeconds, fmtEstimate, fmtSpeed, speedTone } from "@/lib/speed";
import { cn } from "@/lib/utils";

const SIZES = [
  { value: 16, label: "16 MB" },
  { value: 64, label: "64 MB" },
  { value: 256, label: "256 MB" },
];

const PILL = {
  success: "bg-success/12 text-success",
  warning: "bg-warning/15 text-warning",
  destructive: "bg-destructive/12 text-destructive",
};

const DOT = {
  success: "bg-success",
  warning: "bg-warning",
  destructive: "bg-destructive",
};

const GIB = 1024 ** 3;

/**
 * SpeedScore is a result in a pill: the score and its rating, in the band's
 * colour. `rating` alone drops the number, for when it is already shown large.
 */
export function SpeedScore({ test, rating = false, className }) {
  if (!test) return null;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 py-0.5 text-xs font-semibold",
        PILL[speedTone(test.score)],
        className,
      )}
    >
      {rating ? null : <span className="tabular">{test.score}</span>}
      <span className={rating ? undefined : "font-medium opacity-80"}>{test.rating}</span>
    </span>
  );
}

/**
 * ScoreMeter places a score on the 0–100 track, tinted by the bands it is
 * rated in: slow up to 40, fair to 60, good beyond.
 */
function ScoreMeter({ score }) {
  const tone = speedTone(score);
  return (
    <div>
      <div
        className="relative"
        role="meter"
        aria-label="Speed score"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={score}
      >
        <div className="flex h-2 gap-0.5 overflow-hidden rounded-full" aria-hidden="true">
          <span className="w-[40%] bg-destructive/25" />
          <span className="w-[20%] bg-warning/30" />
          <span className="flex-1 bg-success/25" />
        </div>
        <span
          className={cn(
            "absolute top-1/2 size-4 -translate-x-1/2 -translate-y-1/2 rounded-full border-[3px] border-card shadow-[0_1px_3px_rgb(0_0_0/0.25)]",
            DOT[tone],
          )}
          style={{ left: `${Math.max(2, Math.min(98, score))}%` }}
          aria-hidden="true"
        />
      </div>
      <div className="relative mt-2 h-4 text-[0.6875rem] text-muted-foreground" aria-hidden="true">
        <span className="absolute left-[20%] -translate-x-1/2">Slow</span>
        <span className="absolute left-[50%] -translate-x-1/2">Fair</span>
        <span className="absolute left-[80%] -translate-x-1/2">Good</span>
      </div>
    </div>
  );
}

function Metric({ icon: Icon, label, value, hint }) {
  return (
    <div className="flex items-center gap-3 py-2.5">
      <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <dt className="flex-1 text-sm text-muted-foreground">
        {label}
        {hint ? <span className="block text-xs">{hint}</span> : null}
      </dt>
      <dd className="tabular font-semibold">{value}</dd>
    </div>
  );
}

/** The newest result in full: score, the three measurements, and what they mean for 1 GB. */
function Result({ test }) {
  const backup = estimateSeconds(GIB, test.uploadBytesPerSec, test.overheadSeconds);
  const restore = estimateSeconds(GIB, test.downloadBytesPerSec, test.overheadSeconds);
  return (
    <div className="space-y-5">
      <div className="flex items-end justify-between gap-3">
        <p className="leading-none">
          <span className="display tabular text-[2.75rem] font-semibold tracking-tight">{test.score}</span>
          <span className="ml-1 text-sm text-muted-foreground">/ 100</span>
        </p>
        <SpeedScore test={test} rating className="mb-1.5 px-3 py-1 text-sm" />
      </div>
      <ScoreMeter score={test.score} />
      <dl className="-my-1 divide-y divide-border">
        <Metric icon={ArrowUp} label="Upload" value={fmtSpeed(test.uploadBytesPerSec)} />
        <Metric icon={ArrowDown} label="Download" value={fmtSpeed(test.downloadBytesPerSec)} />
        <Metric
          icon={Timer}
          label="Overhead"
          hint="per backup or restore"
          value={`${Number(test.overheadSeconds).toFixed(1)} s`}
        />
      </dl>
      <p className="rounded-xl bg-secondary px-4 py-3 text-sm text-muted-foreground">
        Moving <span className="font-medium text-foreground">1 GB</span> takes{" "}
        <span className="font-medium text-foreground">{fmtEstimate(backup)}</span> to back up and{" "}
        <span className="font-medium text-foreground">{fmtEstimate(restore)}</span> to restore.
      </p>
      <p className="text-sm text-muted-foreground">
        <Tooltip>
          <TooltipTrigger asChild>
            <span>Tested {fmtRelative(test.testedAt)}</span>
          </TooltipTrigger>
          <TooltipContent>{fmtTime(test.testedAt)}</TooltipContent>
        </Tooltip>{" "}
        with {fmtBytes(test.sizeBytes)} ·{" "}
        <Link to={`/runs/${test.runId}`} className="font-medium text-primary hover:underline">
          View run
        </Link>
      </p>
    </div>
  );
}

/** Earlier results, one line each, so a change in speed shows. */
function History({ tests }) {
  if (tests.length === 0) return null;
  return (
    <div>
      <p className="text-sm text-muted-foreground">Earlier tests</p>
      <ul className="-mx-2 mt-1.5 space-y-0.5">
        {tests.map((t) => (
          <li key={t.runId}>
            <Link
              to={`/runs/${t.runId}`}
              className="flex items-center gap-3 rounded-lg px-2 py-1.5 text-sm transition-colors hover:bg-accent/60"
            >
              <span className="min-w-0 flex-1">
                <span className="block text-foreground">{fmtRelative(t.testedAt)}</span>
                <span className="block truncate tabular text-xs text-muted-foreground">
                  ↑ {fmtSpeed(t.uploadBytesPerSec)} · ↓ {fmtSpeed(t.downloadBytesPerSec)}
                </span>
              </span>
              <SpeedScore test={t} />
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}

function phaseOf(pct) {
  if (pct < 10) return "Timing a tiny backup and restore…";
  if (pct < 55) return "Uploading test data…";
  if (pct < 95) return "Downloading it back…";
  return "Forgetting the test snapshots…";
}

/** The test in progress, from its live stream. */
function LiveTest({ run }) {
  const { run: liveRun } = useRunStream(run.id, { withLog: false });
  const live = liveRun || run;
  const pct = displayPercent(live);
  return (
    <div className="rounded-xl border border-running/25 bg-running/5 px-4 py-4">
      <div className="flex items-center justify-between gap-3">
        <span className="flex items-center gap-2 font-medium text-running">
          <LoaderCircle className="size-4 animate-spin" aria-hidden="true" />
          Testing…
        </span>
        <span className="display tabular text-lg font-semibold text-running">{Math.round(pct)}%</span>
      </div>
      <Progress value={pct} className="mt-3" />
      <p className="mt-2.5 text-sm text-muted-foreground">
        {phaseOf(pct)}{" "}
        <Link to={`/runs/${run.id}`} className="font-medium text-primary hover:underline">
          View log
        </Link>
      </p>
    </div>
  );
}

/**
 * SpeedTestCard measures how fast this repository takes and returns data, and
 * keeps the results: the newest in full, earlier ones as a short history.
 */
export function SpeedTestCard({ repositoryId }) {
  const navigate = useNavigate();
  const { activeRuns, runsVersion } = useLive();
  const [size, setSize] = React.useState(64);
  const [starting, setStarting] = React.useState(false);

  const loader = React.useCallback(async () => {
    const res = await Repositories.speedTests(repositoryId);
    if (!res.ok) throw new Error("Could not load speed test results.");
    return res.body?.results || [];
  }, [repositoryId]);
  const { data: results, loading, error, reload } = useFetch(loader);

  // A test that just finished shows its result without a reload.
  const firstVersion = React.useRef(runsVersion);
  React.useEffect(() => {
    if (runsVersion !== firstVersion.current) reload();
  }, [runsVersion, reload]);

  const running = activeRuns.find((r) => r.kind === "speedtest" && r.repositoryId === repositoryId);
  const busyElsewhere = !running && activeRuns.some((r) => r.repositoryId === repositoryId);
  const latest = results?.[0];

  const start = async () => {
    setStarting(true);
    const res = await Repositories.speedTest(repositoryId, size);
    setStarting(false);
    handleStartResponse(res, navigate, "Could not start the speed test.", { stay: true });
  };

  return (
    <Card>
      <CardHeader>
        <div className="flex min-w-0 flex-1 items-start gap-3.5">
          <IconTile icon={Gauge} tone="cyan" size="sm" />
          <div className="min-w-0">
            <CardTitle>Speed</CardTitle>
            <CardDescription className="mt-1">
              How fast backups and restores move data to and from this storage.
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-6">
        {running ? <LiveTest run={running} /> : null}

        {loading && !results ? (
          <div className="space-y-3">
            <Skeleton className="h-11 w-28" />
            <Skeleton className="h-2 w-full" />
            <Skeleton className="h-24 w-full" />
          </div>
        ) : error && !results ? (
          <p className="text-sm text-destructive">
            {error.message}{" "}
            <button type="button" onClick={reload} className="font-medium underline underline-offset-2">
              Retry
            </button>
          </p>
        ) : latest ? (
          <>
            <Result test={latest} />
            <History tests={results.slice(1)} />
          </>
        ) : running ? null : (
          <p className="text-sm text-muted-foreground">
            Not tested yet. A test backs up random data, reads it back and scores the storage from
            0 to 100.
          </p>
        )}

        {running ? null : (
          <div className="space-y-3 border-t border-border pt-5">
            <Segmented label="Test size" options={SIZES} value={size} onChange={setSize} />
            <Button className="w-full" onClick={start} disabled={starting || busyElsewhere}>
              {starting ? <LoaderCircle className="animate-spin" /> : <Gauge />}
              {starting ? "Starting…" : latest ? "Test again" : "Run speed test"}
            </Button>
            <p className="text-xs text-muted-foreground">
              {busyElsewhere
                ? "Another operation is using this repository. The test can start once it finishes."
                : `Moves about ${fmtBytes(size * 2 * 1024 ** 2)} over the network. The test snapshots are forgotten afterwards; their space is freed at the next prune.`}
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

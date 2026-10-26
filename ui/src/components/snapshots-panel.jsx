import * as React from "react";
import { useNavigate } from "react-router-dom";
import {
  ArrowDownToLine,
  Camera,
  Database,
  DatabaseBackup,
  Ellipsis,
  FolderInput,
  FolderOpen,
  FolderSearch,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { EmptyState } from "@/components/empty";
import { CheckOption, Field, PathValue } from "@/components/fields";
import { IconTile } from "@/components/kind";
import { useConfirm } from "@/components/confirm";
import { DbRestoreDialog } from "@/components/db-restore-dialog";
import { SnapshotBrowser } from "@/components/snapshot-browser";
import { Jobs, Repositories } from "@/lib/api";
import { useContainersUsing } from "@/lib/docker";
import { dumpDatabaseName, engineLabel, isSafetySnapshot, snapshotEngine } from "@/lib/snapshots";
import { fmtBytes, fmtCount, fmtRelative, fmtTime, shortId } from "@/lib/format";
import { handleStartResponse } from "@/lib/start-run";

/**
 * RestoreDialog picks where files go. From a job page, that defaults to the
 * job's folder; the optional checkbox unlocks a different absolute path.
 * From a repository page (no job folder), the path field is required.
 */
function RestoreDialog({
  open,
  onOpenChange,
  snapshot,
  repositoryId,
  defaultTarget = "",
  stay = false,
}) {
  const navigate = useNavigate();
  const hasDefault = Boolean(defaultTarget && defaultTarget.trim());
  const [custom, setCustom] = React.useState(false);
  const [target, setTarget] = React.useState("");
  const [stopSite, setStopSite] = React.useState(true);
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => {
    if (!open) return;
    setCustom(!hasDefault);
    setTarget(hasDefault ? defaultTarget.trim() : "");
    setStopSite(true);
  }, [open, hasDefault, defaultTarget]);

  const destination = (custom ? target : defaultTarget).trim();
  // Restoring a site's volume in place: its containers can be stopped meanwhile.
  const containers = useContainersUsing(open && hasDefault && !custom ? destination : "");

  const start = async (e) => {
    e.preventDefault();
    if (!destination) return;
    setBusy(true);
    const res = await Repositories.restore(repositoryId, snapshot.id, destination, {
      stopContainers: stopSite && containers.length > 0,
    });
    setBusy(false);
    if (handleStartResponse(res, navigate, "Could not start the restore.", { stay })) {
      onOpenChange(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={start} className="contents">
          <DialogHeader>
            <DialogTitle>Restore snapshot {shortId(snapshot?.id)}</DialogTitle>
            <DialogDescription>
              {hasDefault
                ? "This snapshot replaces the job folder: files from the backup overwrite matching files, and anything not in the snapshot is deleted."
                : "Files are written under this folder using the snapshot's full paths. Matching files are overwritten, and anything not in the snapshot is deleted."}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            {hasDefault && !custom ? (
              <Field label="Restore into">
                <PathValue>{defaultTarget.trim()}</PathValue>
              </Field>
            ) : null}

            {hasDefault ? (
              <CheckOption
                checked={custom}
                onChange={(on) => {
                  setCustom(on);
                  if (on && !target.trim()) setTarget(defaultTarget.trim());
                }}
                disabled={busy}
                title="Restore to a different path"
                description={
                  custom
                    ? "Snapshot paths are recreated under the path you enter; extra files there are deleted"
                    : "Keep the job folder above (in-place replace)"
                }
              />
            ) : null}

            {containers.length > 0 ? (
              <CheckOption
                checked={stopSite}
                onChange={setStopSite}
                disabled={busy}
                title="Stop the site while restoring"
                description={`${containers.map((c) => c.name).join(", ")} stop first and start again afterwards, so nothing writes to the folder mid-restore.`}
              />
            ) : null}

            {!hasDefault || custom ? (
              <Field
                label={hasDefault ? "Different path (absolute)" : "Restore into (absolute path)"}
                htmlFor="restore-target"
              >
                <Input
                  id="restore-target"
                  value={target}
                  onChange={(e) => setTarget(e.target.value)}
                  placeholder="/home/you/restored-files"
                  className="font-mono text-sm"
                  autoFocus={!hasDefault || custom}
                />
              </Field>
            ) : null}
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !destination}>
              {busy ? "Starting…" : "Start restore"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Snapshots are long lists on busy repositories; they open a page at a time. */
const PAGE = 20;

/** What a snapshot holds, as an icon and hue: files, a database dump, or a safety copy. */
function snapshotLook(s) {
  if (isSafetySnapshot(s)) return { icon: DatabaseBackup, tone: "warning" };
  if (snapshotEngine(s)) return { icon: Database, tone: "fuchsia" };
  return { icon: FolderOpen, tone: "fuchsia" };
}

/**
 * SnapshotRow is one stored snapshot: when it was taken, what it holds and how
 * big it is, with Restore up front and the rarer actions in a menu.
 */
function SnapshotRow({ snapshot: s, onRestore, onBrowse, onDownload, onForget, downloading, forgetting }) {
  const engine = snapshotEngine(s);
  const look = snapshotLook(s);
  const facts = [
    s.sizeBytes != null ? fmtBytes(s.sizeBytes) : null,
    s.fileCount != null ? `${fmtCount(s.fileCount)} file${s.fileCount === 1 ? "" : "s"}` : null,
  ].filter(Boolean);

  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-3 px-5 py-4 transition-colors hover:bg-accent/30 sm:px-6">
      <IconTile icon={look.icon} tone={look.tone} />
      <div className="min-w-0 flex-1 basis-56">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1">
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="font-medium">{fmtRelative(s.time)}</span>
            </TooltipTrigger>
            <TooltipContent>{fmtTime(s.time)}</TooltipContent>
          </Tooltip>
          <span className="rounded-md bg-secondary px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
            {s.shortId || shortId(s.id)}
          </span>
          {engine ? (
            <Badge variant="outline" title={engineLabel(engine)}>
              <Database className="size-3" />
              {dumpDatabaseName(s)}
            </Badge>
          ) : null}
          {isSafetySnapshot(s) ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Badge variant="warning">safety copy</Badge>
              </TooltipTrigger>
              <TooltipContent>Taken right before a database restore replaced the data</TooltipContent>
            </Tooltip>
          ) : null}
        </div>
        <p className="mt-1 truncate text-sm text-muted-foreground">
          {/* Size first: a long path would otherwise push it out of sight. */}
          {[engine ? `${engineLabel(engine)} dump` : null, ...facts].filter(Boolean).join(" · ")}
          {!engine && s.paths?.length ? (
            <span className="font-mono text-[0.8125rem]" title={s.paths.join(", ")}>
              {facts.length ? " · " : ""}
              {s.paths.join(", ")}
            </span>
          ) : null}
        </p>
      </div>
      <div className="ml-auto flex shrink-0 items-center gap-1.5">
        <Button variant="outline" size="sm" onClick={onRestore}>
          <FolderInput />
          Restore
        </Button>
        {engine ? null : (
          <Button variant="ghost" size="sm" onClick={onBrowse}>
            <FolderSearch />
            Browse
          </Button>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={`More actions for snapshot ${s.shortId || shortId(s.id)}`}
              disabled={downloading || forgetting}
            >
              <Ellipsis />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={onDownload}>
              <ArrowDownToLine />
              {downloading ? "Starting…" : "Download as .zip"}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={onForget}>
              <Trash2 />
              {forgetting ? "Starting…" : "Delete snapshot"}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </li>
  );
}

/**
 * SnapshotsPanel lists what is actually stored in a repository. It is shared by
 * the job screen (filtered to that job's tag) and the repository screen (every
 * snapshot), so restoring works the same way from either place.
 */
export function SnapshotsPanel({
  title = "Snapshots",
  description,
  load,
  repositoryId,
  reloadKey,
  /** When set (job page), restore defaults to this absolute folder path. */
  defaultRestoreTarget,
  /** When set (database job page), database restores default to this source. */
  databaseSource,
  /** When set, allow forgetting every snapshot for this job. */
  jobId,
  /** Keep the user on this page after starting restore/download/forget. */
  stay = false,
}) {
  const navigate = useNavigate();
  const confirm = useConfirm();
  const [state, setState] = React.useState({ loading: true, payload: null });
  const [restoring, setRestoring] = React.useState(null);
  const [restoringDb, setRestoringDb] = React.useState(null);
  const [browsing, setBrowsing] = React.useState(null);
  const [downloadingId, setDownloadingId] = React.useState(null);
  const [forgettingId, setForgettingId] = React.useState(null);
  const [forgettingAll, setForgettingAll] = React.useState(false);
  const [visible, setVisible] = React.useState(PAGE);

  const refresh = React.useCallback(async () => {
    setState((s) => ({ ...s, loading: true }));
    const res = await load();
    setState({ loading: false, payload: res.body });
  }, [load]);

  React.useEffect(() => {
    refresh();
  }, [refresh, reloadKey]);

  // Listing can take minutes on slow S3 storage; say so instead of leaving a
  // bare skeleton that looks stuck.
  const [slow, setSlow] = React.useState(false);
  React.useEffect(() => {
    if (!state.loading) return undefined;
    setSlow(false);
    const t = setTimeout(() => setSlow(true), 8000);
    return () => clearTimeout(t);
  }, [state.loading]);

  const startDownload = async (snapshot) => {
    setDownloadingId(snapshot.id);
    const res = await Repositories.download(repositoryId, snapshot.id);
    setDownloadingId(null);
    handleStartResponse(res, navigate, "Could not start the download.", { stay });
  };

  const startForget = async (snapshot) => {
    const id = shortId(snapshot.id);
    const ok = await confirm({
      title: `Delete snapshot ${id}?`,
      description:
        "This forgets the snapshot and prunes unused data from the repository. Other jobs sharing the repository keep their snapshots. This cannot be undone.",
      confirmLabel: "Delete snapshot",
      destructive: true,
    });
    if (!ok) return;
    setForgettingId(snapshot.id);
    const res = await Repositories.forget(repositoryId, snapshot.id);
    setForgettingId(null);
    handleStartResponse(res, navigate, "Could not delete the snapshot.", { stay });
  };

  const startForgetAll = async () => {
    const ok = await confirm({
      title: "Delete all snapshots from this job?",
      description:
        "This forgets every snapshot tagged for this job and prunes unused data. The job itself is kept. This cannot be undone.",
      confirmLabel: "Delete snapshots",
      destructive: true,
    });
    if (!ok) return;
    setForgettingAll(true);
    const res = await Jobs.forget(jobId);
    setForgettingAll(false);
    handleStartResponse(res, navigate, "Could not delete this job's snapshots.", { stay });
  };

  const payload = state.payload;
  const snapshots = React.useMemo(() => {
    const list = (payload && payload.ok && payload.snapshots) || [];
    return [...list].sort((a, b) => new Date(b.time) - new Date(a.time));
  }, [payload]);

  const shown = snapshots.slice(0, visible);

  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1">
          <CardTitle className="flex items-center gap-2.5">
            {title}
            {snapshots.length > 0 ? (
              <span className="rounded-full bg-secondary px-2 py-0.5 text-xs font-semibold tabular text-muted-foreground">
                {snapshots.length}
              </span>
            ) : null}
          </CardTitle>
          {description ? <CardDescription className="mt-1">{description}</CardDescription> : null}
        </div>
        <div className="flex flex-wrap gap-2">
          {jobId && snapshots.length > 0 ? (
            <Button
              variant="outline"
              size="sm"
              onClick={startForgetAll}
              disabled={state.loading || forgettingAll}
              className="text-destructive hover:text-destructive"
            >
              <Trash2 />
              {forgettingAll ? "Starting…" : "Delete all"}
            </Button>
          ) : null}
          <Button variant="outline" size="sm" onClick={refresh} disabled={state.loading}>
            <RefreshCw className={state.loading ? "animate-spin" : undefined} />
            Refresh
          </Button>
        </div>
      </CardHeader>

      {state.loading && !payload ? (
        <div className="divide-y divide-border border-t border-border">
          {[0, 1, 2].map((i) => (
            <div key={i} className="flex items-center gap-4 px-5 py-4 sm:px-6">
              <Skeleton className="size-10 rounded-xl" />
              <div className="flex-1 space-y-2">
                <Skeleton className="h-4 w-40" />
                <Skeleton className="h-3.5 w-64 max-w-full" />
              </div>
            </div>
          ))}
          {slow ? (
            <p className="px-6 py-4 text-sm text-muted-foreground">
              Still waiting for the repository. On slow storage, listing snapshots can take a few minutes.
            </p>
          ) : null}
        </div>
      ) : !payload || !payload.ok ? (
        <div className="border-t border-border">
          <EmptyState
            icon={Camera}
            tone={payload?.code === "not_initialized" ? "cyan" : "warning"}
            title={
              payload?.code === "not_initialized"
                ? "This repository is not set up yet"
                : payload?.code === "timeout"
                  ? "The repository is slow to answer"
                  : "Could not list snapshots"
            }
            description={
              payload?.code === "not_initialized"
                ? "Run a backup and the repository will be initialized automatically, or initialize it now from the repository page."
                : payload?.error || "The repository could not be reached."
            }
          />
        </div>
      ) : snapshots.length === 0 ? (
        <div className="border-t border-border">
          <EmptyState
            icon={Camera}
            tone="cyan"
            title="No snapshots yet"
            description="A snapshot appears here after the first successful backup."
          />
        </div>
      ) : (
        <>
          <ul className="divide-y divide-border border-t border-border">
            {shown.map((s) => (
              <SnapshotRow
                key={s.id}
                snapshot={s}
                onRestore={() => (snapshotEngine(s) ? setRestoringDb(s) : setRestoring(s))}
                onBrowse={() => setBrowsing(s)}
                onDownload={() => startDownload(s)}
                onForget={() => startForget(s)}
                downloading={downloadingId === s.id}
                forgetting={forgettingId === s.id}
              />
            ))}
          </ul>
          {snapshots.length > PAGE ? (
            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border px-5 py-3 sm:px-6">
              <p className="text-sm text-muted-foreground">
                Showing <span className="tabular text-foreground">{shown.length}</span> of{" "}
                <span className="tabular text-foreground">{snapshots.length}</span>
              </p>
              <div className="flex gap-2">
                {visible > PAGE ? (
                  <Button variant="ghost" size="sm" onClick={() => setVisible(PAGE)}>
                    Show fewer
                  </Button>
                ) : null}
                {shown.length < snapshots.length ? (
                  <Button variant="outline" size="sm" onClick={() => setVisible((v) => v + PAGE)}>
                    Show {Math.min(PAGE, snapshots.length - shown.length)} more
                  </Button>
                ) : null}
              </div>
            </div>
          ) : null}
        </>
      )}

      {restoring ? (
        <RestoreDialog
          open={restoring !== null}
          onOpenChange={(open) => !open && setRestoring(null)}
          snapshot={restoring}
          repositoryId={repositoryId}
          defaultTarget={defaultRestoreTarget}
          stay={stay}
        />
      ) : null}
      {restoringDb ? (
        <DbRestoreDialog
          open={restoringDb !== null}
          onOpenChange={(open) => !open && setRestoringDb(null)}
          snapshot={restoringDb}
          repositoryId={repositoryId}
          defaultSource={databaseSource}
          stay={stay}
        />
      ) : null}
      {browsing ? (
        <SnapshotBrowser
          open={browsing !== null}
          onOpenChange={(open) => !open && setBrowsing(null)}
          snapshot={browsing}
          repositoryId={repositoryId}
          stay={stay}
        />
      ) : null}
    </Card>
  );
}

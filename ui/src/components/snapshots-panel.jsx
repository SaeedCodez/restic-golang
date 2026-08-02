import * as React from "react";
import { useNavigate } from "react-router-dom";
import { ArrowDownToLine, Camera, FolderInput, RefreshCw } from "lucide-react";
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
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState } from "@/components/empty";
import { Mono } from "@/components/page";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Repositories } from "@/lib/api";
import { fmtBytes, fmtCount, fmtRelative, fmtTime, shortId } from "@/lib/format";
import { handleStartResponse } from "@/lib/start-run";

/**
 * RestoreDialog collects the one thing a restore needs that the app cannot
 * infer: where to put the files. Download needs nothing, but shares the flow so
 * both actions read the same way.
 */
function RestoreDialog({ open, onOpenChange, snapshot, repositoryId }) {
  const navigate = useNavigate();
  const [target, setTarget] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => {
    if (open) setTarget("");
  }, [open]);

  const start = async (e) => {
    e.preventDefault();
    if (!target.trim()) return;
    setBusy(true);
    const res = await Repositories.restore(repositoryId, snapshot.id, target.trim());
    setBusy(false);
    if (handleStartResponse(res, navigate, "Could not start the restore.")) {
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
              restic writes the snapshot's files into this folder. Choose somewhere empty —
              existing files with the same names are overwritten.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="restore-target">Restore into (absolute path)</Label>
            <Input
              id="restore-target"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              placeholder="/home/you/restored-files"
              className="font-mono text-xs"
              autoFocus
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !target.trim()}>
              {busy ? "Starting…" : "Start restore"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
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
}) {
  const navigate = useNavigate();
  const [state, setState] = React.useState({ loading: true, payload: null });
  const [restoring, setRestoring] = React.useState(null);
  const [downloadingId, setDownloadingId] = React.useState(null);

  const refresh = React.useCallback(async () => {
    setState((s) => ({ ...s, loading: true }));
    const res = await load();
    setState({ loading: false, payload: res.body });
  }, [load]);

  React.useEffect(() => {
    refresh();
  }, [refresh, reloadKey]);

  const startDownload = async (snapshot) => {
    setDownloadingId(snapshot.id);
    const res = await Repositories.download(repositoryId, snapshot.id);
    setDownloadingId(null);
    handleStartResponse(res, navigate, "Could not start the download.");
  };

  const payload = state.payload;
  const snapshots = React.useMemo(() => {
    const list = (payload && payload.ok && payload.snapshots) || [];
    return [...list].sort((a, b) => new Date(b.time) - new Date(a.time));
  }, [payload]);

  return (
    <Card>
      <CardHeader>
        <div className="min-w-0 flex-1">
          <CardTitle>{title}</CardTitle>
          {description ? <CardDescription className="mt-1">{description}</CardDescription> : null}
        </div>
        <Button variant="outline" size="sm" onClick={refresh} disabled={state.loading}>
          <RefreshCw className={state.loading ? "animate-spin" : undefined} />
          Refresh
        </Button>
      </CardHeader>

      {state.loading && !payload ? (
        <div className="space-y-2 px-5 pb-5">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </div>
      ) : !payload || !payload.ok ? (
        <div className="px-5 pb-5">
          <EmptyState
            icon={Camera}
            title={
              payload?.code === "not_initialized"
                ? "This repository is not set up yet"
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
        <div className="px-5 pb-5">
          <EmptyState
            icon={Camera}
            title="No snapshots yet"
            description="A snapshot appears here after the first successful backup."
          />
        </div>
      ) : (
        <div className="border-t border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Snapshot</TableHead>
                <TableHead>Taken</TableHead>
                <TableHead>Size</TableHead>
                <TableHead>Files</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {snapshots.map((s) => (
                <TableRow key={s.id} className="transition-colors hover:bg-accent/40">
                  <TableCell>
                    <Mono>{s.shortId || shortId(s.id)}</Mono>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-muted-foreground">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span>{fmtRelative(s.time)}</span>
                      </TooltipTrigger>
                      <TooltipContent>{fmtTime(s.time)}</TooltipContent>
                    </Tooltip>
                  </TableCell>
                  <TableCell className="tabular text-muted-foreground">
                    {s.sizeBytes ? fmtBytes(s.sizeBytes) : "—"}
                  </TableCell>
                  <TableCell className="tabular text-muted-foreground">
                    {s.fileCount ? fmtCount(s.fileCount) : "—"}
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1.5">
                      <Button variant="outline" size="sm" onClick={() => setRestoring(s)}>
                        <FolderInput />
                        Restore
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => startDownload(s)}
                        disabled={downloadingId === s.id}
                      >
                        <ArrowDownToLine />
                        {downloadingId === s.id ? "Starting…" : "Download"}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {restoring ? (
        <RestoreDialog
          open={restoring !== null}
          onOpenChange={(open) => !open && setRestoring(null)}
          snapshot={restoring}
          repositoryId={repositoryId}
        />
      ) : null}
    </Card>
  );
}

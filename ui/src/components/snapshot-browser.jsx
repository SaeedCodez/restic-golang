import * as React from "react";
import { useNavigate } from "react-router-dom";
import { ArrowDownToLine, ChevronRight, File, Folder, FolderInput } from "lucide-react";
import { Button } from "@/components/ui/button";
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
import { Mono } from "@/components/page";
import { Repositories, errorOf } from "@/lib/api";
import { useContainersUsing } from "@/lib/docker";
import { fmtBytes, fmtRelative, shortId } from "@/lib/format";
import { handleStartResponse } from "@/lib/start-run";
import { cn } from "@/lib/utils";

const within = (p, base) => base === "/" || p === base || p.startsWith(base + "/");

/** The backed-up folder that holds every selected path, if there is one. */
function commonRoot(snapshot, selected) {
  const paths = [...selected];
  return (snapshot?.paths || []).find((root) => paths.length > 0 && paths.every((p) => within(p, root)));
}

function Breadcrumbs({ path, onOpen }) {
  const parts = path.split("/").filter(Boolean);
  return (
    <nav className="flex min-w-0 flex-wrap items-center gap-0.5 font-mono text-xs text-muted-foreground">
      <button type="button" onClick={() => onOpen("/")} className="rounded px-1 hover:text-foreground">
        /
      </button>
      {parts.map((part, i) => {
        const to = "/" + parts.slice(0, i + 1).join("/");
        const last = i === parts.length - 1;
        return (
          <React.Fragment key={to}>
            {i > 0 ? <ChevronRight className="size-3 opacity-50" /> : null}
            <button
              type="button"
              onClick={() => onOpen(to)}
              className={cn("rounded px-1 hover:text-foreground", last && "text-foreground")}
            >
              {part}
            </button>
          </React.Fragment>
        );
      })}
    </nav>
  );
}

/**
 * SnapshotBrowser walks a snapshot's files so a few of them can be restored or
 * downloaded instead of the whole thing. Restoring in place replaces only the
 * selected files and folders; everything else stays as it is now.
 */
export function SnapshotBrowser({ open, onOpenChange, snapshot, repositoryId, stay = false }) {
  const navigate = useNavigate();
  const start = snapshot?.paths?.length === 1 ? snapshot.paths[0] : "/";
  const [path, setPath] = React.useState(start);
  const [state, setState] = React.useState({ loading: true, entries: [], error: "" });
  const [selected, setSelected] = React.useState(() => new Set());
  const [step, setStep] = React.useState("browse"); // browse | restore
  const [inPlace, setInPlace] = React.useState(true);
  const [target, setTarget] = React.useState("");
  const [stopSite, setStopSite] = React.useState(true);
  const [busy, setBusy] = React.useState(false);
  const [slow, setSlow] = React.useState(false);

  React.useEffect(() => {
    if (!open) return;
    setPath(start);
    setSelected(new Set());
    setStep("browse");
    setInPlace(true);
    setTarget("");
    setStopSite(true);
  }, [open, start]);

  React.useEffect(() => {
    if (!open || !snapshot) return undefined;
    let alive = true;
    setState((s) => ({ ...s, loading: true, error: "" }));
    setSlow(false);
    const t = setTimeout(() => alive && setSlow(true), 6000);
    Repositories.files(repositoryId, snapshot.id, path).then((res) => {
      if (!alive) return;
      clearTimeout(t);
      setState(
        res.ok
          ? { loading: false, entries: res.body.entries || [], error: "" }
          : { loading: false, entries: [], error: errorOf(res, "Could not list this folder.") },
      );
    });
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [open, snapshot, repositoryId, path]);

  const root = commonRoot(snapshot, selected);
  const canInPlace = Boolean(root);
  const destination = inPlace && canInPlace ? root : target.trim();
  const containers = useContainersUsing(step === "restore" && inPlace && canInPlace ? root : "");

  const toggle = (p) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(p)) next.delete(p);
      else next.add(p);
      return next;
    });

  const download = async () => {
    setBusy(true);
    const res = await Repositories.download(repositoryId, snapshot.id, [...selected]);
    setBusy(false);
    if (handleStartResponse(res, navigate, "Could not start the download.", { stay })) onOpenChange(false);
  };

  const restore = async (e) => {
    e.preventDefault();
    if (!destination) return;
    setBusy(true);
    const res = await Repositories.restore(repositoryId, snapshot.id, destination, {
      include: [...selected],
      stopContainers: inPlace && canInPlace && stopSite && containers.length > 0,
    });
    setBusy(false);
    if (handleStartResponse(res, navigate, "Could not start the restore.", { stay })) onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        {step === "browse" ? (
          <>
            <DialogHeader>
              <DialogTitle>Files in snapshot {shortId(snapshot?.id)}</DialogTitle>
              <DialogDescription>
                Taken {fmtRelative(snapshot?.time)}. Tick files or folders to restore or download just
                those.
              </DialogDescription>
            </DialogHeader>

            <div className="min-w-0 space-y-2">
              <Breadcrumbs path={path} onOpen={setPath} />
              <div className="max-h-[50vh] overflow-y-auto rounded-md border border-border scroll-thin">
                {state.loading ? (
                  <div className="space-y-2 p-3">
                    <Skeleton className="h-6 w-full" />
                    <Skeleton className="h-6 w-full" />
                    <Skeleton className="h-6 w-2/3" />
                    {slow ? (
                      <p className="pt-1 text-[13px] text-muted-foreground">
                        Reading the snapshot's file list. The first time can take a minute on remote
                        storage; after that, folders open instantly.
                      </p>
                    ) : null}
                  </div>
                ) : state.error ? (
                  <p className="p-3 text-[13px] text-destructive">{state.error}</p>
                ) : state.entries.length === 0 ? (
                  <p className="p-3 text-[13px] text-muted-foreground">This folder is empty.</p>
                ) : (
                  <ul className="divide-y divide-border">
                    {state.entries.map((e) => {
                      const dir = e.type === "dir";
                      // A path under a selected folder is already included.
                      const covered = [...selected].some((s) => s !== e.path && within(e.path, s));
                      return (
                        <li key={e.path} className="flex items-center gap-3 px-3 py-1.5 text-[13px] hover:bg-accent/40">
                          <input
                            type="checkbox"
                            className="size-4 accent-foreground"
                            checked={selected.has(e.path) || covered}
                            disabled={covered}
                            onChange={() => toggle(e.path)}
                            aria-label={`Select ${e.name}`}
                          />
                          {dir ? (
                            <Folder className="size-4 shrink-0 text-muted-foreground" />
                          ) : (
                            <File className="size-4 shrink-0 text-muted-foreground" />
                          )}
                          {dir ? (
                            <button
                              type="button"
                              onClick={() => setPath(e.path)}
                              className="min-w-0 flex-1 truncate text-left hover:underline"
                            >
                              {e.name}
                            </button>
                          ) : (
                            <span className="min-w-0 flex-1 truncate">{e.name}</span>
                          )}
                          <span className="tabular shrink-0 text-xs text-muted-foreground">
                            {fmtBytes(e.size)}
                          </span>
                          <span className="hidden w-24 shrink-0 text-right text-xs text-muted-foreground sm:block">
                            {e.mtime ? fmtRelative(e.mtime) : ""}
                          </span>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </div>
            </div>

            <DialogFooter className="items-center">
              <span className="mr-auto text-[13px] text-muted-foreground">
                {selected.size ? `${selected.size} selected` : "Nothing selected"}
              </span>
              <Button type="button" variant="outline" onClick={download} disabled={busy || selected.size === 0}>
                <ArrowDownToLine />
                Download
              </Button>
              <Button type="button" onClick={() => setStep("restore")} disabled={selected.size === 0}>
                <FolderInput />
                Restore…
              </Button>
            </DialogFooter>
          </>
        ) : (
          <form onSubmit={restore} className="contents">
            <DialogHeader>
              <DialogTitle>Restore {selected.size} selected item(s)</DialogTitle>
              <DialogDescription>
                Each selected file or folder is replaced by its copy from the snapshot; files inside a
                selected folder that are not in the snapshot are deleted. Nothing else is touched.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-3">
              <ul className="max-h-32 overflow-y-auto rounded-md border border-border bg-muted/40 px-3 py-2 scroll-thin">
                {[...selected].map((p) => (
                  <li key={p}>
                    <Mono>{p}</Mono>
                  </li>
                ))}
              </ul>

              {canInPlace ? (
                <label className="flex cursor-pointer items-center justify-between gap-3 rounded-md border border-border px-3 py-2.5">
                  <div>
                    <p className="text-sm font-medium">Put them back where they were</p>
                    <p className="text-[13px] text-muted-foreground">
                      {inPlace ? <>In {root}</> : "Off: restore into the folder below instead"}
                    </p>
                  </div>
                  <input
                    type="checkbox"
                    className="size-4 accent-foreground"
                    checked={inPlace}
                    onChange={(e) => setInPlace(e.target.checked)}
                  />
                </label>
              ) : null}

              {!inPlace || !canInPlace ? (
                <div className="space-y-2">
                  <Label htmlFor="browse-target">Restore into (absolute path)</Label>
                  <Input
                    id="browse-target"
                    value={target}
                    onChange={(e) => setTarget(e.target.value)}
                    placeholder="/tmp/restored"
                    className="font-mono text-xs"
                  />
                  <p className="text-[13px] text-muted-foreground">
                    The snapshot's full paths are recreated under this folder.
                  </p>
                </div>
              ) : null}

              {inPlace && canInPlace && containers.length > 0 ? (
                <label className="flex cursor-pointer items-start gap-3 rounded-md border border-border px-3 py-2.5">
                  <input
                    type="checkbox"
                    className="mt-0.5 size-4 accent-foreground"
                    checked={stopSite}
                    onChange={(e) => setStopSite(e.target.checked)}
                  />
                  <span>
                    <span className="block text-sm font-medium">Stop the site while restoring</span>
                    <span className="block text-[13px] text-muted-foreground">
                      {containers.map((c) => c.name).join(", ")} stop first and start again afterwards.
                    </span>
                  </span>
                </label>
              ) : null}
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setStep("browse")}>
                Back
              </Button>
              <Button type="submit" disabled={busy || !destination}>
                {busy ? "Starting…" : "Start restore"}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

import * as React from "react";
import { useNavigate } from "react-router-dom";
import { TriangleAlert } from "lucide-react";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Mono } from "@/components/page";
import { Databases, Repositories } from "@/lib/api";
import { fmtRelative, shortId } from "@/lib/format";
import { dumpDatabaseName, engineLabel, snapshotEngine } from "@/lib/snapshots";
import { handleStartResponse } from "@/lib/start-run";
import { cn } from "@/lib/utils";

/**
 * DbRestoreDialog restores a database dump. Replacing the live database drops
 * and recreates it, so it asks for the database's name to be typed; restoring
 * as a new database next to it is the safe way to look inside a backup.
 */
export function DbRestoreDialog({ open, onOpenChange, snapshot, repositoryId, defaultSource, stay = false }) {
  const navigate = useNavigate();
  const engine = snapshotEngine(snapshot);
  const dumped = dumpDatabaseName(snapshot);
  const [sources, setSources] = React.useState(null);
  const [sourceId, setSourceId] = React.useState("");
  const [mode, setMode] = React.useState("replace");
  const [into, setInto] = React.useState("");
  const [safety, setSafety] = React.useState(true);
  const [typed, setTyped] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  // The job page passes a fresh object each render; only its id matters here.
  const defaultId = defaultSource?.id;

  React.useEffect(() => {
    if (!open) return undefined;
    let alive = true;
    setMode("replace");
    setInto(`${dumped}_restored`);
    setSafety(true);
    setTyped("");
    setSources(null);
    Databases.list().then((res) => {
      if (!alive) return;
      // A dump only restores into the engine it came from (MySQL and MariaDB
      // dumps are not interchangeable).
      const list = (res.body?.databases || []).filter((d) => d.engine === engine);
      setSources(list);
      const preferred =
        list.find((d) => d.id === defaultId) || list.find((d) => d.database === dumped) || list[0];
      setSourceId(preferred?.id || "");
    });
    return () => {
      alive = false;
    };
  }, [open, engine, dumped, defaultId]);

  const source = (sources || []).find((d) => d.id === sourceId);
  const target = mode === "replace" ? source?.database || "" : into.trim();
  const ready = source && target && (mode === "copy" || typed === source.database);

  const start = async (e) => {
    e.preventDefault();
    if (!ready) return;
    setBusy(true);
    const res = await Repositories.restoreDb(repositoryId, {
      snapshotId: snapshot.id,
      databaseId: source.id,
      into: mode === "copy" ? target : "",
      safetyBackup: safety,
    });
    setBusy(false);
    if (handleStartResponse(res, navigate, "Could not start the database restore.", { stay })) {
      onOpenChange(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={start} className="contents">
          <DialogHeader>
            <DialogTitle>Restore database from {shortId(snapshot?.id)}</DialogTitle>
            <DialogDescription>
              {engineLabel(engine)} dump of <Mono>{dumped}</Mono>, taken {fmtRelative(snapshot?.time)}.
            </DialogDescription>
          </DialogHeader>

          {sources && sources.length === 0 ? (
            <p className="rounded-md bg-warning/10 px-3 py-2 text-[13px] text-warning">
              There is no {engineLabel(engine)} database source to restore into. Add one from the
              Coolify/Docker page first.
            </p>
          ) : (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="db-restore-source">Database server</Label>
                <Select value={sourceId} onValueChange={setSourceId}>
                  <SelectTrigger id="db-restore-source">
                    <SelectValue placeholder="Choose a database" />
                  </SelectTrigger>
                  <SelectContent>
                    {(sources || []).map((d) => (
                      <SelectItem key={d.id} value={d.id}>
                        {d.name} — {d.database} on {d.container}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="flex gap-0.5 rounded-lg bg-muted p-0.5 text-[13px]">
                {[
                  ["replace", `Replace ${source?.database || "the database"}`],
                  ["copy", "Restore as a new database"],
                ].map(([value, label]) => (
                  <button
                    key={value}
                    type="button"
                    onClick={() => setMode(value)}
                    className={cn(
                      "flex-1 truncate rounded-[6px] px-2.5 py-1.5 transition-colors",
                      mode === value
                        ? "bg-background text-foreground dark:bg-accent"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {label}
                  </button>
                ))}
              </div>

              {mode === "replace" ? (
                <>
                  <div className="flex gap-2 rounded-md bg-destructive/10 px-3 py-2.5 text-[13px] text-destructive">
                    <TriangleAlert className="mt-0.5 size-4 shrink-0" />
                    <span>
                      <Mono>{source?.database}</Mono> is replaced by this snapshot: rows and tables added
                      since it was taken are gone, and apps using it lose their connections for a moment.
                      {engine === "postgres"
                        ? " The backup is imported into a temporary database first, so the live one stays untouched until the import is complete."
                        : ""}
                    </span>
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="db-restore-confirm">
                      Type <Mono>{source?.database}</Mono> to confirm
                    </Label>
                    <Input
                      id="db-restore-confirm"
                      value={typed}
                      onChange={(e) => setTyped(e.target.value)}
                      className="font-mono text-xs"
                      autoComplete="off"
                    />
                  </div>
                </>
              ) : (
                <div className="space-y-2">
                  <Label htmlFor="db-restore-into">New database name</Label>
                  <Input
                    id="db-restore-into"
                    value={into}
                    onChange={(e) => setInto(e.target.value)}
                    className="font-mono text-xs"
                  />
                  <p className="text-[13px] text-muted-foreground">
                    Created on the same server next to the live database, which is not touched. If a
                    database with this name exists, it is replaced.
                  </p>
                </div>
              )}

              <label className="flex cursor-pointer items-start gap-3 rounded-md border border-border px-3 py-2.5">
                <input
                  type="checkbox"
                  className="mt-0.5 size-4 accent-foreground"
                  checked={safety}
                  onChange={(e) => setSafety(e.target.checked)}
                />
                <span>
                  <span className="block text-sm font-medium">Take a safety backup first</span>
                  <span className="block text-[13px] text-muted-foreground">
                    The current database is saved as a snapshot before it is replaced, and put back
                    automatically if the import fails.
                  </span>
                </span>
              </label>
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant={mode === "replace" ? "destructive" : "default"}
              disabled={busy || !ready}
            >
              {busy ? "Starting…" : mode === "replace" ? "Replace database" : "Restore copy"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

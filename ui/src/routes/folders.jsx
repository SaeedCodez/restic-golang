import * as React from "react";
import { useLocation } from "react-router-dom";
import { toast } from "sonner";
import { FolderOpen, Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { EmptyState } from "@/components/empty";
import { Page, PageHeader } from "@/components/page";
import { useConfirm } from "@/components/confirm";
import { Folders, errorOf } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";

function FolderDialog({ open, onOpenChange, folder, onSaved }) {
  const editing = Boolean(folder);
  const [name, setName] = React.useState("");
  const [path, setPath] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    if (!open) return;
    setError("");
    setName(folder?.name || "");
    setPath(folder?.path || "");
  }, [open, folder]);

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    const body = { name: name.trim(), path: path.trim() };
    const res = editing ? await Folders.update(folder.id, body) : await Folders.create(body);
    setBusy(false);
    if (res.ok) {
      toast.success(editing ? "Folder updated." : `Folder “${body.name}” added.`);
      onOpenChange(false);
      onSaved();
    } else {
      setError(errorOf(res, "Could not save the folder."));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="contents">
          <DialogHeader>
            <DialogTitle>{editing ? "Edit folder" : "Add a folder"}</DialogTitle>
            <DialogDescription>
              Naming a folder turns a path into something reusable, so you pick it from a list
              instead of retyping it for every job.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="folder-name">Name</Label>
              <Input
                id="folder-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Documents"
                autoFocus
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="folder-path">Absolute path</Label>
              <Input
                id="folder-path"
                value={path}
                onChange={(e) => setPath(e.target.value)}
                placeholder="/home/you/Documents"
                className="font-mono text-xs"
              />
            </div>
            {error ? (
              <p className="rounded-md bg-destructive/10 px-3 py-2 text-[13px] text-destructive">
                {error}
              </p>
            ) : null}
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !name.trim() || !path.trim()}>
              {busy ? "Saving…" : editing ? "Save changes" : "Add folder"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export default function FoldersRoute() {
  const confirm = useConfirm();
  const location = useLocation();
  // Arriving from the setup guide's "Add a folder" opens the dialog straight away.
  const [dialog, setDialog] = React.useState({
    open: Boolean(location.state?.create),
    folder: null,
  });

  const loader = React.useCallback(async () => {
    const res = await Folders.list();
    return res.body?.folders || [];
  }, []);
  const { data, loading, reload } = useFetch(loader, { initial: [] });

  React.useEffect(() => {
    document.title = "Folders · restic backup manager";
  }, []);

  const remove = async (folder) => {
    const ok = await confirm({
      title: `Delete “${folder.name}”?`,
      description:
        "This only removes the folder from the app's list. Nothing on disk is touched.",
      confirmLabel: "Delete folder",
      destructive: true,
    });
    if (!ok) return;
    const res = await Folders.remove(folder.id);
    if (res.ok) {
      toast.success(`Folder “${folder.name}” deleted.`);
      reload();
    } else {
      toast.error("Could not delete the folder", { description: errorOf(res), duration: 8000 });
    }
  };

  const folders = data || [];

  return (
    <Page>
      <PageHeader
        title="Backup folders"
        description="The directories you want protected, named once and reused by any job."
        actions={
          folders.length > 0 ? (
            <Button onClick={() => setDialog({ open: true, folder: null })}>
              <Plus />
              Add folder
            </Button>
          ) : null
        }
      />

      {loading && folders.length === 0 ? (
        <div className="space-y-3">
          <Skeleton className="h-16 w-full rounded-xl" />
          <Skeleton className="h-16 w-full rounded-xl" />
        </div>
      ) : folders.length === 0 ? (
        <EmptyState
          icon={FolderOpen}
          title="No folders yet"
          description="Add the directory you want backed up — give it a name so you never have to retype the path."
          action={
            <Button onClick={() => setDialog({ open: true, folder: null })}>
              <Plus />
              Add a folder
            </Button>
          }
        />
      ) : (
        <div className="space-y-3">
          {folders.map((folder) => (
            <Card key={folder.id} className="transition-colors hover:border-input">
              <div className="flex flex-wrap items-center gap-3 p-4">
                <span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground">
                  <FolderOpen className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="display text-sm font-medium">{folder.name}</p>
                  <p className="truncate font-mono text-xs text-muted-foreground">
                    {folder.path}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => setDialog({ open: true, folder })}
                        aria-label={`Edit ${folder.name}`}
                      >
                        <Pencil />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Edit</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => remove(folder)}
                        className="text-muted-foreground hover:text-destructive"
                        aria-label={`Delete ${folder.name}`}
                      >
                        <Trash2 />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Delete</TooltipContent>
                  </Tooltip>
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      <FolderDialog
        open={dialog.open}
        onOpenChange={(open) => setDialog((d) => ({ ...d, open }))}
        folder={dialog.folder}
        onSaved={reload}
      />
    </Page>
  );
}

import * as React from "react";
import { Link, useLocation } from "react-router-dom";
import { toast } from "sonner";
import { Cloud, HardDrive, Pencil, Plus, Trash2 } from "lucide-react";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { EmptyState } from "@/components/empty";
import { Page, PageHeader } from "@/components/page";
import { useConfirm } from "@/components/confirm";
import { Repositories, errorOf } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { repoLocation } from "@/lib/format";

const EMPTY = {
  name: "",
  backendType: "Local",
  localPath: "",
  endpoint: "",
  bucket: "",
  region: "",
  accessKey: "",
  secretKey: "",
  password: "",
};

/**
 * RepositoryDialog creates and edits repositories.
 *
 * Secrets are never sent to the browser, so on edit the password and secret-key
 * fields start empty and mean "keep what is stored" — the server merges an
 * omitted secret with the existing one.
 */
export function RepositoryDialog({ open, onOpenChange, repository, onSaved }) {
  const editing = Boolean(repository);
  const [form, setForm] = React.useState(EMPTY);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    if (!open) return;
    setError("");
    setForm(
      repository
        ? { ...EMPTY, ...repository, password: "", secretKey: "" }
        : EMPTY,
    );
  }, [open, repository]);

  const set = (key) => (e) => setForm((f) => ({ ...f, [key]: e.target.value }));
  const isS3 = form.backendType === "S3";

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    const body = {
      name: form.name.trim(),
      backendType: form.backendType,
      localPath: form.localPath.trim(),
      endpoint: form.endpoint.trim(),
      bucket: form.bucket.trim(),
      region: form.region.trim(),
      accessKey: form.accessKey.trim(),
      secretKey: form.secretKey,
      password: form.password,
    };
    const res = editing
      ? await Repositories.update(repository.id, body)
      : await Repositories.create(body);
    setBusy(false);
    if (res.ok) {
      toast.success(editing ? "Repository updated." : `Repository “${body.name}” added.`);
      onOpenChange(false);
      onSaved();
    } else {
      setError(errorOf(res, "Could not save the repository."));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="contents">
          <DialogHeader>
            <DialogTitle>{editing ? "Edit repository" : "Add a repository"}</DialogTitle>
            <DialogDescription>
              Where restic keeps your encrypted backups. A local directory needs no
              credentials and is the quickest way to try this out.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="repo-name">Name</Label>
              <Input
                id="repo-name"
                value={form.name}
                onChange={set("name")}
                placeholder="Offsite S3"
                autoFocus
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="repo-backend">Backend</Label>
              <Select
                value={form.backendType}
                onValueChange={(v) => setForm((f) => ({ ...f, backendType: v }))}
              >
                <SelectTrigger id="repo-backend">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="Local">Local directory</SelectItem>
                  <SelectItem value="S3">S3-compatible</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {isS3 ? (
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2 sm:col-span-2">
                  <Label htmlFor="repo-endpoint">Endpoint</Label>
                  <Input
                    id="repo-endpoint"
                    value={form.endpoint}
                    onChange={set("endpoint")}
                    placeholder="https://s3.amazonaws.com"
                    className="font-mono text-xs"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="repo-bucket">Bucket</Label>
                  <Input id="repo-bucket" value={form.bucket} onChange={set("bucket")} />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="repo-region">Region (optional)</Label>
                  <Input id="repo-region" value={form.region} onChange={set("region")} />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="repo-access">Access key</Label>
                  <Input
                    id="repo-access"
                    value={form.accessKey}
                    onChange={set("accessKey")}
                    autoComplete="off"
                    className="font-mono text-xs"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="repo-secret">Secret key</Label>
                  <Input
                    id="repo-secret"
                    type="password"
                    value={form.secretKey}
                    onChange={set("secretKey")}
                    autoComplete="new-password"
                    placeholder={
                      repository?.hasSecretKey ? "leave blank to keep the stored key" : ""
                    }
                  />
                </div>
              </div>
            ) : (
              <div className="space-y-2">
                <Label htmlFor="repo-path">Repository directory</Label>
                <Input
                  id="repo-path"
                  value={form.localPath}
                  onChange={set("localPath")}
                  placeholder="/home/you/restic-repo"
                  className="font-mono text-xs"
                />
                <p className="text-xs text-muted-foreground">
                  Created and initialized automatically on the first backup.
                </p>
              </div>
            )}

            <div className="space-y-2">
              <Label htmlFor="repo-password">Repository password</Label>
              <Input
                id="repo-password"
                type="password"
                value={form.password}
                onChange={set("password")}
                autoComplete="new-password"
                placeholder={
                  repository?.hasPassword
                    ? "leave blank to keep the stored password"
                    : "encrypts your backups — do not lose it"
                }
              />
              <p className="text-xs text-muted-foreground">
                This encrypts everything in the repository. Without it the backups cannot be
                read — not by this app, and not by anyone else.
              </p>
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
            <Button type="submit" disabled={busy || !form.name.trim()}>
              {busy ? "Saving…" : editing ? "Save changes" : "Add repository"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export default function RepositoriesRoute() {
  const confirm = useConfirm();
  const location = useLocation();
  // Arriving from the setup guide's "Add a repository" opens the dialog directly.
  const [dialog, setDialog] = React.useState({
    open: Boolean(location.state?.create),
    repository: null,
  });

  const loader = React.useCallback(async () => {
    const res = await Repositories.list();
    return res.body?.repositories || [];
  }, []);
  const { data, loading, reload } = useFetch(loader, { initial: [] });

  React.useEffect(() => {
    document.title = "Repositories · restic backup manager";
  }, []);

  const remove = async (repo) => {
    const ok = await confirm({
      title: `Delete “${repo.name}”?`,
      description:
        "This removes the repository from the app. The backup data on disk or in the bucket is left untouched.",
      confirmLabel: "Delete repository",
      destructive: true,
    });
    if (!ok) return;
    const res = await Repositories.remove(repo.id);
    if (res.ok) {
      toast.success(`Repository “${repo.name}” deleted.`);
      reload();
    } else {
      // A repository still used by a job returns a 409 naming those jobs.
      toast.error("Could not delete the repository", {
        description: errorOf(res),
        duration: 8000,
      });
    }
  };

  const repos = data || [];

  return (
    <Page>
      <PageHeader
        title="Storage repositories"
        description="Where restic stores encrypted backups. A local directory is the quickest way to try this out — no credentials needed."
        actions={
          repos.length > 0 ? (
            <Button onClick={() => setDialog({ open: true, repository: null })}>
              <Plus />
              Add repository
            </Button>
          ) : null
        }
      />

      {loading && repos.length === 0 ? (
        <div className="space-y-3">
          <Skeleton className="h-20 w-full rounded-xl" />
          <Skeleton className="h-20 w-full rounded-xl" />
        </div>
      ) : repos.length === 0 ? (
        <EmptyState
          icon={HardDrive}
          title="No repositories yet"
          description="Add one to give your backups somewhere to live. A local directory works with no credentials."
          action={
            <Button onClick={() => setDialog({ open: true, repository: null })}>
              <Plus />
              Add a repository
            </Button>
          }
        />
      ) : (
        <div className="space-y-3">
          {repos.map((repo) => {
            const Icon = repo.backendType === "S3" ? Cloud : HardDrive;
            return (
              <Card key={repo.id} className="transition-colors hover:border-input">
                <div className="flex flex-wrap items-center gap-3 p-4">
                  <span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground">
                    <Icon className="size-4" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <Link
                      to={`/repositories/${repo.id}`}
                      className="display text-sm font-medium hover:underline"
                    >
                      {repo.name}
                    </Link>
                    <p className="truncate font-mono text-xs text-muted-foreground">
                      {repoLocation(repo) || "—"}
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-1.5">
                    <Button variant="outline" size="sm" asChild>
                      <Link to={`/repositories/${repo.id}`}>Open</Link>
                    </Button>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setDialog({ open: true, repository: repo })}
                          aria-label={`Edit ${repo.name}`}
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
                          onClick={() => remove(repo)}
                          className="text-muted-foreground hover:text-destructive"
                          aria-label={`Delete ${repo.name}`}
                        >
                          <Trash2 />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>Delete</TooltipContent>
                    </Tooltip>
                  </div>
                </div>
              </Card>
            );
          })}
        </div>
      )}

      <RepositoryDialog
        open={dialog.open}
        onOpenChange={(open) => setDialog((d) => ({ ...d, open }))}
        repository={dialog.repository}
        onSaved={reload}
      />
    </Page>
  );
}

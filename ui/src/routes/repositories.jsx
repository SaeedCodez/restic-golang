import * as React from "react";
import { Link, useLocation } from "react-router-dom";
import { toast } from "sonner";
import { ArrowRight, Cloud, HardDrive, Pencil, Plus, Trash2, Vault } from "lucide-react";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { EmptyState, ErrorState, InlineError } from "@/components/empty";
import { Field, Segmented } from "@/components/fields";
import { IconTile, repositoryIcon } from "@/components/kind";
import { StatusDot } from "@/components/status-badge";
import { SpeedScore } from "@/components/speed-test";
import { ListPagination } from "@/components/pagination";
import { Page, PageHeader } from "@/components/page";
import { useConfirm } from "@/components/confirm";
import { Jobs, Repositories, errorOf } from "@/lib/api";
import { PAGE_SIZE, pageSlice } from "@/lib/pagination";
import { useFetch } from "@/lib/use-fetch";
import { fmtRelative, fmtTime, repoLocation } from "@/lib/format";
import { statusLabel } from "@/lib/runs";
import { fmtSpeed } from "@/lib/speed";

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

          <div className="space-y-5">
            <Field label="Name" htmlFor="repo-name">
              <Input
                id="repo-name"
                value={form.name}
                onChange={set("name")}
                placeholder="Offsite S3"
                autoFocus
              />
            </Field>

            <Field label="Backend">
              <Segmented
                label="Backend"
                tone="cyan"
                value={form.backendType}
                onChange={(v) => setForm((f) => ({ ...f, backendType: v }))}
                options={[
                  { value: "Local", label: "Local directory", icon: HardDrive },
                  { value: "S3", label: "S3-compatible", icon: Cloud },
                ]}
              />
            </Field>

            {isS3 ? (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Endpoint" htmlFor="repo-endpoint" className="sm:col-span-2">
                  <Input
                    id="repo-endpoint"
                    value={form.endpoint}
                    onChange={set("endpoint")}
                    placeholder="https://s3.amazonaws.com"
                    className="font-mono text-sm"
                  />
                </Field>
                <Field label="Bucket" htmlFor="repo-bucket">
                  <Input id="repo-bucket" value={form.bucket} onChange={set("bucket")} />
                </Field>
                <Field label="Region (optional)" htmlFor="repo-region">
                  <Input id="repo-region" value={form.region} onChange={set("region")} />
                </Field>
                <Field label="Access key" htmlFor="repo-access">
                  <Input
                    id="repo-access"
                    value={form.accessKey}
                    onChange={set("accessKey")}
                    autoComplete="off"
                    className="font-mono text-sm"
                  />
                </Field>
                <Field label="Secret key" htmlFor="repo-secret">
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
                </Field>
              </div>
            ) : (
              <Field
                label="Repository directory"
                htmlFor="repo-path"
                hint="Created and initialized automatically on the first backup."
              >
                <Input
                  id="repo-path"
                  value={form.localPath}
                  onChange={set("localPath")}
                  placeholder="/home/you/restic-repo"
                  className="font-mono text-sm"
                />
              </Field>
            )}

            <Field
              label="Repository password"
              htmlFor="repo-password"
              hint="This encrypts everything in the repository. Without it the backups cannot be read — not by this app, and not by anyone else."
            >
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
            </Field>

            <InlineError>{error}</InlineError>
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

/** The backend in words, as it appears on cards and the detail page. */
export function backendLabel(repo) {
  return repo?.backendType === "S3" ? "S3-compatible" : "Local directory";
}

/**
 * RepositoryCard is one repository: where it is, how many jobs store there and
 * how the latest of them went. The whole card opens the repository; edit and
 * delete sit in its footer.
 */
function RepositoryCard({ repo, usage, onEdit, onDelete }) {
  const latest = usage?.latest;
  return (
    <Card className="relative flex flex-col transition-colors hover:border-input">
      <div className="flex items-start gap-4 p-5">
        <IconTile icon={repositoryIcon(repo)} tone="cyan" />
        <div className="min-w-0 flex-1">
          <Link
            to={`/repositories/${repo.id}`}
            className="display block truncate text-md font-semibold after:absolute after:inset-0 after:rounded-2xl focus-visible:outline-none"
          >
            {repo.name}
          </Link>
          <p className="mt-1 truncate font-mono text-[0.8125rem] text-muted-foreground">
            {repoLocation(repo) || "—"}
          </p>
        </div>
      </div>

      <dl className="grid grid-cols-2 gap-4 px-5 pb-5 sm:grid-cols-4">
        <div className="min-w-0">
          <dt className="text-xs text-muted-foreground">Backend</dt>
          <dd className="mt-1 truncate text-sm font-medium">{backendLabel(repo)}</dd>
        </div>
        <div className="min-w-0">
          <dt className="text-xs text-muted-foreground">Jobs</dt>
          <dd className="mt-1 text-sm font-medium tabular">{usage ? usage.jobs : "—"}</dd>
        </div>
        <div className="min-w-0">
          <dt className="text-xs text-muted-foreground">Latest run</dt>
          <dd className="mt-1 flex items-center gap-2 text-sm font-medium">
            {latest ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="relative flex min-w-0 items-center gap-2">
                    <StatusDot status={latest.status} />
                    <span className="truncate">{fmtRelative(latest.startedAt)}</span>
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  {statusLabel(latest.status)} · {fmtTime(latest.startedAt)}
                </TooltipContent>
              </Tooltip>
            ) : (
              <span className="text-muted-foreground">Never</span>
            )}
          </dd>
        </div>
        <div className="min-w-0">
          <dt className="text-xs text-muted-foreground">Speed</dt>
          <dd className="mt-1 text-sm font-medium">
            {repo.lastSpeedTest ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="relative inline-flex">
                    <SpeedScore test={repo.lastSpeedTest} />
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  Up {fmtSpeed(repo.lastSpeedTest.uploadBytesPerSec)} · down{" "}
                  {fmtSpeed(repo.lastSpeedTest.downloadBytesPerSec)} · tested{" "}
                  {fmtRelative(repo.lastSpeedTest.testedAt)}
                </TooltipContent>
              </Tooltip>
            ) : (
              <span className="text-muted-foreground">Not tested</span>
            )}
          </dd>
        </div>
      </dl>

      <div className="relative mt-auto flex items-center gap-1 border-t border-border px-3 py-2.5">
        <Button variant="ghost" size="sm" asChild className="text-muted-foreground">
          <Link to={`/repositories/${repo.id}`}>
            Snapshots and maintenance
            <ArrowRight />
          </Link>
        </Button>
        <span className="ml-auto" />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-sm" onClick={onEdit} aria-label={`Edit ${repo.name}`}>
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
              onClick={onDelete}
              className="text-muted-foreground hover:text-destructive"
              aria-label={`Delete ${repo.name}`}
            >
              <Trash2 />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Delete</TooltipContent>
        </Tooltip>
      </div>
    </Card>
  );
}

/** How many jobs store in each repository, and the newest run among them. */
function usageByRepository(jobs) {
  const m = {};
  for (const j of jobs || []) {
    const u = (m[j.repositoryId] ||= { jobs: 0, latest: null });
    u.jobs += 1;
    if (j.lastRun && (!u.latest || new Date(j.lastRun.startedAt) > new Date(u.latest.startedAt))) {
      u.latest = j.lastRun;
    }
  }
  return m;
}

export default function RepositoriesRoute() {
  const confirm = useConfirm();
  const location = useLocation();
  // Arriving from the setup guide's "Add a repository" opens the dialog directly.
  const [dialog, setDialog] = React.useState({
    open: Boolean(location.state?.create),
    repository: null,
  });
  const [page, setPage] = React.useState(1);

  const loader = React.useCallback(async () => {
    const [res, jobsR] = await Promise.all([Repositories.list(), Jobs.list()]);
    if (!res.ok) throw new Error(errorOf(res, "Could not load the repositories."));
    // Usage is extra detail: without the jobs list the cards still work.
    return {
      repositories: res.body?.repositories || [],
      usage: jobsR.ok ? usageByRepository(jobsR.body?.jobs) : null,
    };
  }, []);
  const { data, loading, error, reload } = useFetch(loader);

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

  const repos = data?.repositories || [];
  const paged = pageSlice(repos, page, PAGE_SIZE);
  const openCreate = () => setDialog({ open: true, repository: null });

  React.useEffect(() => {
    if (page !== paged.page) setPage(paged.page);
  }, [page, paged.page]);

  return (
    <Page>
      <PageHeader
        title="Storage repositories"
        description="Where restic stores encrypted backups. A local directory is the quickest way to try this out — no credentials needed."
        actions={
          repos.length > 0 ? (
            <Button onClick={openCreate}>
              <Plus />
              Add repository
            </Button>
          ) : null
        }
      />

      {error && !data ? (
        <ErrorState
          title="Could not load the repositories"
          description={`${error.message} Check that the server is running, then retry.`}
          onRetry={reload}
        />
      ) : loading && !data ? (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-[214px] rounded-2xl" />
          ))}
        </div>
      ) : repos.length === 0 ? (
        <EmptyState
          framed
          icon={Vault}
          tone="cyan"
          title="No repositories yet"
          description="Add one to give your backups somewhere to live. A local directory works with no credentials."
          action={
            <Button onClick={openCreate}>
              <Plus />
              Add a repository
            </Button>
          }
        />
      ) : (
        <div className="space-y-4">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {paged.items.map((repo) => (
              <RepositoryCard
                key={repo.id}
                repo={repo}
                usage={data.usage ? data.usage[repo.id] || { jobs: 0, latest: null } : null}
                onEdit={() => setDialog({ open: true, repository: repo })}
                onDelete={() => remove(repo)}
              />
            ))}
          </div>
          <ListPagination
            page={paged.page}
            pageSize={PAGE_SIZE}
            total={paged.total}
            onPageChange={setPage}
          />
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

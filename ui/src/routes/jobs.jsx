import * as React from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { Database, FolderOpen, Layers, Plus, Workflow } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ListPagination } from "@/components/pagination";
import { PAGE_SIZE, pageSlice } from "@/lib/pagination";
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
import { Page, PageHeader } from "@/components/page";
import { EmptyState, ErrorState, InlineError } from "@/components/empty";
import { FilterTabs } from "@/components/filter-tabs";
import { JobCard, useJobStrips } from "@/components/job-card";
import { Databases, Folders, Jobs, Repositories, errorOf } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";
import { cn } from "@/lib/utils";

/** The source filter tabs; `kind` matches a job's sourceKind. */
const SOURCE_FILTERS = [
  { value: "all", label: "All jobs", icon: Layers },
  { value: "folder", label: "Files", icon: FolderOpen, tone: "fuchsia" },
  { value: "database", label: "Databases", icon: Database, tone: "fuchsia" },
];

function NewJobDialog({
  open,
  onOpenChange,
  folders,
  databases,
  repositories,
  preferKind,
  onCreated,
}) {
  const [name, setName] = React.useState("");
  const [kind, setKind] = React.useState("folder");
  const [folderId, setFolderId] = React.useState("");
  const [databaseId, setDatabaseId] = React.useState("");
  const [repositoryId, setRepositoryId] = React.useState("");
  const [autoDaily, setAutoDaily] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    if (!open) return;
    setName("");
    setError("");
    setAutoDaily(false);
    // Opened from the Databases tab, the dialog starts on a database.
    const wantDb = preferKind === "database" ? databases.length > 0 : folders.length === 0;
    setKind(wantDb && databases.length > 0 ? "database" : "folder");
    setFolderId(folders[0]?.id || "");
    setDatabaseId(databases[0]?.id || "");
    setRepositoryId(repositories[0]?.id || "");
  }, [open, folders, databases, repositories, preferKind]);

  const sourceId = kind === "database" ? databaseId : folderId;

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    const body = { name: name.trim(), repositoryId };
    if (kind === "database") body.databaseId = databaseId;
    else body.folderId = folderId;
    if (autoDaily) {
      body.schedule = { enabled: true, kind: "daily", at: "02:00" };
    }
    const res = await Jobs.create(body);
    setBusy(false);
    if (res.ok) {
      toast.success(`Job “${res.body.job.name}” created.`);
      onOpenChange(false);
      onCreated();
    } else {
      setError(errorOf(res, "Could not create the job."));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="contents">
          <DialogHeader>
            <DialogTitle>New backup job</DialogTitle>
            <DialogDescription>
              A job pairs one folder or database with one repository. Run it, and its whole
              history lives on its page.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="job-name">Name</Label>
              <Input
                id="job-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Documents → offsite"
                autoFocus
              />
            </div>

            {databases.length > 0 ? (
              <div className="flex gap-1 rounded-xl bg-secondary p-1 text-sm">
                {[
                  ["folder", "A folder", FolderOpen],
                  ["database", "A database", Database],
                ].map(([value, label, Icon]) => (
                  <button
                    key={value}
                    type="button"
                    onClick={() => setKind(value)}
                    className={cn(
                      "flex h-9 flex-1 items-center justify-center gap-2 rounded-lg px-3 font-medium transition-colors",
                      kind === value
                        ? "bg-card text-foreground shadow-[0_1px_2px_rgb(0_0_0/0.08)]"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    <Icon className={cn("size-4", kind === value && "text-fuchsia")} />
                    {label}
                  </button>
                ))}
              </div>
            ) : null}

            {kind === "database" ? (
              <div className="space-y-2">
                <Label htmlFor="job-database">Back up this database</Label>
                <Select value={databaseId} onValueChange={setDatabaseId}>
                  <SelectTrigger id="job-database">
                    <SelectValue placeholder="Choose a database" />
                  </SelectTrigger>
                  <SelectContent>
                    {databases.map((d) => (
                      <SelectItem key={d.id} value={d.id}>
                        {d.name} — {d.database} on {d.container}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            ) : (
              <div className="space-y-2">
                <Label htmlFor="job-folder">Back up this folder</Label>
                <Select value={folderId} onValueChange={setFolderId}>
                  <SelectTrigger id="job-folder">
                    <SelectValue placeholder="Choose a folder" />
                  </SelectTrigger>
                  <SelectContent>
                    {folders.map((f) => (
                      <SelectItem key={f.id} value={f.id}>
                        {f.name} — {f.path}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            <div className="space-y-2">
              <Label htmlFor="job-repo">Store it in</Label>
              <Select value={repositoryId} onValueChange={setRepositoryId}>
                <SelectTrigger id="job-repo">
                  <SelectValue placeholder="Choose a repository" />
                </SelectTrigger>
                <SelectContent>
                  {repositories.map((r) => (
                    <SelectItem key={r.id} value={r.id}>
                      {r.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-border px-4 py-3.5 transition-colors hover:bg-accent/50 has-[:checked]:border-primary/40 has-[:checked]:bg-primary/5">
              <input
                type="checkbox"
                className="mt-0.5 size-4 accent-primary"
                checked={autoDaily}
                onChange={(e) => setAutoDaily(e.target.checked)}
              />
              <span>
                <span className="block font-medium">Also run automatically</span>
                <span className="mt-0.5 block text-sm text-muted-foreground">
                  Daily at 02:00 while the app is running. You can change this later on the job
                  page.
                </span>
              </span>
            </label>

            <InlineError>{error}</InlineError>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !name.trim() || !sourceId || !repositoryId}>
              {busy ? "Creating…" : "Create job"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export default function JobsRoute() {
  const { runsVersion } = useLive();
  const location = useLocation();
  const navigate = useNavigate();
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [page, setPage] = React.useState(1);
  // The source filter lives in the URL, so back/forward and reloads keep it.
  const [searchParams, setSearchParams] = useSearchParams();
  const sourceParam = searchParams.get("source");
  const source = SOURCE_FILTERS.some((f) => f.value === sourceParam) ? sourceParam : "all";
  const setSource = (v) => {
    setSearchParams(v === "all" ? {} : { source: v }, { replace: true });
    setPage(1);
  };

  const loader = React.useCallback(async () => {
    const [jobsR, foldersR, dbsR, reposR] = await Promise.all([
      Jobs.list(),
      Folders.list(),
      Databases.list(),
      Repositories.list(),
    ]);
    if (!jobsR.ok) throw new Error(errorOf(jobsR, "Could not load jobs."));
    return {
      jobs: jobsR.body?.jobs || [],
      folders: foldersR.body?.folders || [],
      databases: dbsR.body?.databases || [],
      repositories: reposR.body?.repositories || [],
    };
  }, []);
  const { data, loading, error, reload } = useFetch(loader);

  // A run finishing anywhere changes a job's health, so refresh on run events.
  React.useEffect(() => {
    if (runsVersion > 0) reload();
  }, [runsVersion, reload]);

  // Open New job from GettingStarted (Dashboard) or other deep links.
  // Effect + clear so same-route state.create works without a remount.
  React.useEffect(() => {
    if (location.state?.create) {
      setDialogOpen(true);
      navigate(location.pathname, { replace: true, state: {} });
    }
  }, [location.state, location.pathname, navigate]);

  React.useEffect(() => {
    document.title = "Jobs · restic backup manager";
  }, []);

  const jobs = data?.jobs || [];
  const folders = data?.folders || [];
  const databases = data?.databases || [];
  const repositories = data?.repositories || [];
  const canCreate = (folders.length > 0 || databases.length > 0) && repositories.length > 0;
  const filtered = source === "all" ? jobs : jobs.filter((j) => j.sourceKind === source);
  const counts = {
    all: jobs.length,
    folder: jobs.filter((j) => j.sourceKind === "folder").length,
    database: jobs.filter((j) => j.sourceKind === "database").length,
  };
  const paged = pageSlice(filtered, page, PAGE_SIZE);
  const strips = useJobStrips(
    paged.items.map((j) => j.id),
    runsVersion,
  );

  React.useEffect(() => {
    if (page !== paged.page) setPage(paged.page);
  }, [page, paged.page]);

  return (
    <Page>
      <PageHeader
        title="Backup jobs"
        description="A job is a saved pairing of a folder or database and a repository — the thing you run, and where its history lives."
        actions={
          jobs.length > 0 ? (
            <Button onClick={() => setDialogOpen(true)} disabled={!canCreate}>
              <Plus />
              New job
            </Button>
          ) : null
        }
      />

      {error && !data ? (
        <ErrorState
          title="Could not load jobs"
          description={`${error.message} Check that the server is running, then retry.`}
          onRetry={reload}
        />
      ) : loading && !data ? (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-[248px] rounded-2xl" />
          ))}
        </div>
      ) : jobs.length === 0 ? (
        <EmptyState
          framed
          icon={Workflow}
          tone="violet"
          title="No jobs yet"
          description={
            canCreate
              ? "You have a source and a repository — pair them into a job and you can back up in one click."
              : "Finish setup on the Dashboard, then come back here to create a job."
          }
          action={
            canCreate ? (
              <Button onClick={() => setDialogOpen(true)}>
                <Plus />
                Create a job
              </Button>
            ) : (
              <Button variant="outline" asChild>
                <Link to="/dashboard">Finish setup on the Dashboard</Link>
              </Button>
            )
          }
        />
      ) : (
        <div className="space-y-6">
          <FilterTabs
            label="Filter jobs by source"
            value={source}
            onChange={setSource}
            options={SOURCE_FILTERS.map((f) => ({ ...f, count: counts[f.value] }))}
          />
          {filtered.length === 0 ? (
            <FilteredEmpty
              source={source}
              hasSource={source === "database" ? databases.length > 0 : folders.length > 0}
              canCreate={canCreate}
              onCreate={() => setDialogOpen(true)}
            />
          ) : (
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
              {paged.items.map((job) => (
                <JobCard key={job.id} job={job} runs={strips[job.id] || []} actions onRan={reload} />
              ))}
            </div>
          )}
          <ListPagination
            page={paged.page}
            pageSize={PAGE_SIZE}
            total={paged.total}
            onPageChange={setPage}
          />
        </div>
      )}

      <NewJobDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        folders={folders}
        databases={databases}
        repositories={repositories}
        preferKind={source}
        onCreated={reload}
      />
    </Page>
  );
}

/** FilteredEmpty is the empty state for a source tab that has no jobs in it. */
function FilteredEmpty({ source, hasSource, canCreate, onCreate }) {
  const db = source === "database";
  return (
    <EmptyState
      framed
      icon={db ? Database : FolderOpen}
      tone="fuchsia"
      title={db ? "No database jobs yet" : "No file jobs yet"}
      description={
        hasSource
          ? db
            ? "Create a job and choose “A database” to back one up on its own schedule."
            : "Create a job and choose a folder to back it up on its own schedule."
          : db
            ? "A database job dumps one PostgreSQL, MySQL or MariaDB database. Add the database on the Sources page first."
            : "A file job backs up one folder. Add the folder on the Sources page first."
      }
      action={
        hasSource && canCreate ? (
          <Button onClick={onCreate}>
            <Plus />
            New job
          </Button>
        ) : (
          <Button variant="outline" asChild>
            <Link to="/folders">Open Sources</Link>
          </Button>
        )
      }
    />
  );
}

import * as React from "react";
import { Link, useLocation } from "react-router-dom";
import { toast } from "sonner";
import { Container, Database, FolderOpen, Pencil, Plus, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
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
import { Field } from "@/components/fields";
import { IconTile } from "@/components/kind";
import { ListPagination } from "@/components/pagination";
import { Page, PageHeader, Section } from "@/components/page";
import { useConfirm } from "@/components/confirm";
import { Databases, Folders, Jobs, errorOf } from "@/lib/api";
import { useDockerStatus } from "@/lib/docker";
import { engineLabel } from "@/lib/snapshots";
import { PAGE_SIZE, pageSlice } from "@/lib/pagination";
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

          <div className="space-y-5">
            <Field label="Name" htmlFor="folder-name">
              <Input
                id="folder-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Documents"
                autoFocus
              />
            </Field>
            <Field
              label="Absolute path"
              htmlFor="folder-path"
              hint="The directory as this app's server sees it."
            >
              <Input
                id="folder-path"
                value={path}
                onChange={(e) => setPath(e.target.value)}
                placeholder="/home/you/Documents"
                className="font-mono text-sm"
              />
            </Field>
            <InlineError>{error}</InlineError>
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

/**
 * UsedBy names the jobs that back a source up, linking to each, or says that
 * none does yet with a way to make one.
 */
function UsedBy({ jobs }) {
  if (!jobs) return null;
  if (jobs.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No job backs this up yet.{" "}
        <Link to="/jobs" state={{ create: true }} className="font-medium text-primary hover:underline">
          Create a job
        </Link>
      </p>
    );
  }
  return (
    <p className="min-w-0 text-sm text-muted-foreground">
      Backed up by{" "}
      {jobs.map((j, i) => (
        <React.Fragment key={j.id}>
          {i > 0 ? ", " : ""}
          <Link to={`/jobs/${j.id}`} className="font-medium text-foreground hover:text-primary hover:underline">
            {j.name}
          </Link>
        </React.Fragment>
      ))}
    </p>
  );
}

/**
 * SourceCard is one folder or database: its name and where it lives, which
 * jobs use it, and its edit and delete controls.
 */
function SourceCard({ icon, title, badge, location, jobs, actions }) {
  return (
    <Card className="flex flex-col gap-4 p-5 transition-colors hover:border-input">
      <div className="flex items-start gap-4">
        <IconTile icon={icon} tone="fuchsia" />
        <div className="min-w-0 flex-1">
          <p className="flex min-w-0 items-center gap-2">
            <span className="display truncate text-md font-semibold">{title}</span>
            {badge}
          </p>
          <p className="mt-1 truncate font-mono text-[0.8125rem] text-muted-foreground" title={location}>
            {location}
          </p>
        </div>
        <div className="-mr-1.5 -mt-1 flex shrink-0 items-center gap-0.5">{actions}</div>
      </div>
      {jobs ? (
        <div className="border-t border-border pt-3.5">
          <UsedBy jobs={jobs} />
        </div>
      ) : null}
    </Card>
  );
}

function IconAction({ label, onClick, destructive, icon: Icon }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onClick}
          className={destructive ? "text-muted-foreground hover:text-destructive" : undefined}
          aria-label={label}
        >
          <Icon />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label.split(" ")[0]}</TooltipContent>
    </Tooltip>
  );
}

function SourceGridSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      {[0, 1].map((i) => (
        <Skeleton key={i} className="h-[138px] rounded-2xl" />
      ))}
    </div>
  );
}

/**
 * DatabaseSources lists the databases jobs can back up. They are added from the
 * Coolify/Docker page, where the database is picked from the running servers.
 */
function DatabaseSources({ jobsBySource }) {
  const confirm = useConfirm();
  const docker = useDockerStatus();
  const loader = React.useCallback(async () => {
    const res = await Databases.list();
    if (!res.ok) throw new Error(errorOf(res, "Could not load the databases."));
    return res.body?.databases || [];
  }, []);
  const { data, loading, error, reload } = useFetch(loader);
  const databases = data || [];
  const place = docker?.coolify ? "Coolify" : "Docker";

  const remove = async (db) => {
    const ok = await confirm({
      title: `Delete “${db.name}”?`,
      description: "This only removes the database from the app's list. The database itself and its backups are not touched.",
      confirmLabel: "Delete source",
      destructive: true,
    });
    if (!ok) return;
    const res = await Databases.remove(db.id);
    if (res.ok) {
      toast.success(`Database source “${db.name}” deleted.`);
      reload();
    } else {
      toast.error("Could not delete the database source", { description: errorOf(res), duration: 8000 });
    }
  };

  return (
    <Section
      title={
        <span className="flex items-center gap-2.5">
          Databases
          {databases.length ? <Count n={databases.length} /> : null}
        </span>
      }
      description="Dumped inside their own container, so the backup is consistent while the site stays up."
      action={
        docker?.available && databases.length > 0 ? (
          <Button variant="outline" asChild>
            <Link to="/coolify">
              <Container />
              Add from {place}
            </Link>
          </Button>
        ) : null
      }
    >
      {error && !data ? (
        <ErrorState title="Could not load the databases" description={error.message} onRetry={reload} />
      ) : loading && !data ? (
        <SourceGridSkeleton />
      ) : databases.length === 0 ? (
        <EmptyState
          framed
          icon={Database}
          tone="fuchsia"
          title="No databases yet"
          description={
            docker?.available
              ? `Pick a database on the ${place} page and it shows up here.`
              : "Database backups need the Docker socket mounted into this app's container."
          }
          action={
            docker?.available ? (
              <Button asChild>
                <Link to="/coolify">
                  <Container />
                  Add from {place}
                </Link>
              </Button>
            ) : null
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          {databases.map((db) => (
            <SourceCard
              key={db.id}
              icon={Database}
              title={db.name}
              badge={<Badge variant="outline">{engineLabel(db.engine)}</Badge>}
              location={`${db.database} on ${db.container}`}
              jobs={jobsBySource?.database[db.id] || (jobsBySource ? [] : null)}
              actions={<IconAction label={`Delete ${db.name}`} icon={Trash2} destructive onClick={() => remove(db)} />}
            />
          ))}
        </div>
      )}
    </Section>
  );
}

function Count({ n }) {
  return (
    <span className="rounded-full bg-secondary px-2 py-0.5 text-xs font-semibold tabular text-muted-foreground">
      {n}
    </span>
  );
}

/** Which jobs use each folder and each database, so cards can name them. */
function groupJobsBySource(jobs) {
  const out = { folder: {}, database: {} };
  for (const j of jobs) {
    if (j.databaseId) (out.database[j.databaseId] ||= []).push(j);
    else if (j.folderId) (out.folder[j.folderId] ||= []).push(j);
  }
  return out;
}

export default function FoldersRoute() {
  const confirm = useConfirm();
  const location = useLocation();
  // Arriving from the setup guide's "Add a folder" opens the dialog straight away.
  const [dialog, setDialog] = React.useState({
    open: Boolean(location.state?.create),
    folder: null,
  });
  const [page, setPage] = React.useState(1);

  const loader = React.useCallback(async () => {
    const res = await Folders.list();
    if (!res.ok) throw new Error(errorOf(res, "Could not load the folders."));
    return res.body?.folders || [];
  }, []);
  const { data, loading, error, reload } = useFetch(loader);

  // Which jobs use what is extra detail: the page works without it.
  const loadJobs = React.useCallback(async () => {
    const res = await Jobs.list();
    return res.ok ? groupJobsBySource(res.body?.jobs || []) : null;
  }, []);
  const { data: jobsBySource } = useFetch(loadJobs);

  React.useEffect(() => {
    document.title = "Sources · restic backup manager";
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
  const paged = pageSlice(folders, page, PAGE_SIZE);
  const openCreate = () => setDialog({ open: true, folder: null });

  React.useEffect(() => {
    if (page !== paged.page) setPage(paged.page);
  }, [page, paged.page]);

  return (
    <Page>
      <PageHeader
        title="Backup sources"
        description="What jobs back up: folders, named once and reused by any job, and databases, dumped with their own tools."
      />

      <div className="space-y-12">
        <Section
          title={
            <span className="flex items-center gap-2.5">
              Folders
              {folders.length ? <Count n={folders.length} /> : null}
            </span>
          }
          description="Directories on this server, each named once so any job can pick it from a list."
          action={
            folders.length > 0 ? (
              <Button onClick={openCreate}>
                <Plus />
                Add folder
              </Button>
            ) : null
          }
        >
          {error && !data ? (
            <ErrorState title="Could not load the folders" description={error.message} onRetry={reload} />
          ) : loading && !data ? (
            <SourceGridSkeleton />
          ) : folders.length === 0 ? (
            <EmptyState
              framed
              icon={FolderOpen}
              tone="fuchsia"
              title="No folders yet"
              description="Add the directory you want backed up — give it a name so you never have to retype the path."
              action={
                <Button onClick={openCreate}>
                  <Plus />
                  Add a folder
                </Button>
              }
            />
          ) : (
            <div className="space-y-4">
              <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                {paged.items.map((folder) => (
                  <SourceCard
                    key={folder.id}
                    icon={FolderOpen}
                    title={folder.name}
                    location={folder.path}
                    jobs={jobsBySource ? jobsBySource.folder[folder.id] || [] : null}
                    actions={
                      <>
                        <IconAction
                          label={`Edit ${folder.name}`}
                          icon={Pencil}
                          onClick={() => setDialog({ open: true, folder })}
                        />
                        <IconAction
                          label={`Delete ${folder.name}`}
                          icon={Trash2}
                          destructive
                          onClick={() => remove(folder)}
                        />
                      </>
                    }
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
        </Section>

        <DatabaseSources jobsBySource={jobsBySource} />
      </div>

      <FolderDialog
        open={dialog.open}
        onOpenChange={(open) => setDialog((d) => ({ ...d, open }))}
        folder={dialog.folder}
        onSaved={reload}
      />
    </Page>
  );
}

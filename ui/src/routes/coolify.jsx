import * as React from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import {
  Boxes,
  Check,
  ChevronDown,
  ChevronRight,
  Database,
  FolderOpen,
  Globe,
  Plus,
  RefreshCw,
  Search,
  Server,
} from "lucide-react";
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
import { Mono, Page, PageHeader } from "@/components/page";
import { Docker, Jobs, Repositories, errorOf } from "@/lib/api";
import { useDockerStatus } from "@/lib/docker";
import { engineLabel } from "@/lib/snapshots";
import { handleStartResponse } from "@/lib/start-run";
import { useFetch } from "@/lib/use-fetch";
import { cn } from "@/lib/utils";

const isCoolifyKind = (kind) => kind === "application" || kind === "service" || kind === "database";

/** The name a person knows a resource by: its domain, else its Coolify name. */
function primaryName(resource) {
  return resource.domains?.[0] || resource.name;
}

/**
 * QuickBackupDialog turns one discovered volume or database into a job in one
 * step: name it, pick where to store it, and optionally schedule it and run it
 * right away.
 */
function QuickBackupDialog({ target, onOpenChange, repositories, onCreated }) {
  const navigate = useNavigate();
  const open = Boolean(target);
  const [name, setName] = React.useState("");
  const [repositoryId, setRepositoryId] = React.useState("");
  const [daily, setDaily] = React.useState(true);
  const [runNow, setRunNow] = React.useState(true);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    if (!open) return;
    setName(target.defaultName);
    setRepositoryId(repositories[0]?.id || "");
    setDaily(true);
    setRunNow(true);
    setError("");
  }, [open, target, repositories]);

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    const body = {
      kind: target.kind,
      path: target.path,
      container: target.container,
      database: target.database,
      name: name.trim(),
      repositoryId,
    };
    if (daily) body.schedule = { enabled: true, kind: "daily", at: "02:00" };
    const res = await Docker.createJob(body);
    if (!res.ok) {
      setBusy(false);
      setError(errorOf(res, "Could not create the job."));
      return;
    }
    const job = res.body.job;
    if (runNow) {
      const started = await Jobs.run(job.id);
      handleStartResponse(started, navigate, "The job was created, but its first backup could not start.", {
        stay: true,
      });
    } else {
      toast.success(`Job “${job.name}” created.`, {
        action: { label: "Open job", onClick: () => navigate(`/jobs/${job.id}`) },
      });
    }
    setBusy(false);
    onOpenChange(false);
    onCreated();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="contents">
          <DialogHeader>
            <DialogTitle>Back up {target?.title}</DialogTitle>
            <DialogDescription>{target?.description}</DialogDescription>
          </DialogHeader>

          {repositories.length === 0 ? (
            <p className="rounded-md bg-warning/10 px-3 py-2 text-[13px] text-warning">
              Add a storage repository first, then come back here.{" "}
              <Link to="/repositories" className="underline underline-offset-2">
                Repositories
              </Link>
            </p>
          ) : (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="quick-name">Job name</Label>
                <Input id="quick-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
              </div>
              <div className="space-y-2">
                <Label htmlFor="quick-repo">Store it in</Label>
                <Select value={repositoryId} onValueChange={setRepositoryId}>
                  <SelectTrigger id="quick-repo">
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
              <label className="flex cursor-pointer items-start gap-3 rounded-md border border-border px-3 py-2.5">
                <input
                  type="checkbox"
                  className="mt-0.5 size-4 accent-foreground"
                  checked={daily}
                  onChange={(e) => setDaily(e.target.checked)}
                />
                <span>
                  <span className="block text-sm font-medium">Back up every day</span>
                  <span className="block text-[13px] text-muted-foreground">
                    At 02:00. Change the schedule or add retention on the job page.
                  </span>
                </span>
              </label>
              <label className="flex cursor-pointer items-start gap-3 rounded-md border border-border px-3 py-2.5">
                <input
                  type="checkbox"
                  className="mt-0.5 size-4 accent-foreground"
                  checked={runNow}
                  onChange={(e) => setRunNow(e.target.checked)}
                />
                <span>
                  <span className="block text-sm font-medium">Run the first backup now</span>
                </span>
              </label>
              {error ? (
                <p className="rounded-md bg-destructive/10 px-3 py-2 text-[13px] text-destructive">
                  {error}
                </p>
              ) : null}
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !name.trim() || !repositoryId}>
              {busy ? "Creating…" : "Create job"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** BackedUp says which jobs already cover something, linking to them. */
function BackedUp({ jobs }) {
  if (!jobs?.length) return null;
  return (
    <span className="flex min-w-0 items-center gap-1.5 text-[13px] text-muted-foreground">
      <Badge variant="success">
        <Check className="size-3" />
        Backed up
      </Badge>
      <span className="truncate">
        by{" "}
        {jobs.map((j, i) => (
          <React.Fragment key={j.id}>
            {i > 0 ? ", " : ""}
            <Link to={`/jobs/${j.id}`} className="text-foreground hover:underline">
              {j.name}
            </Link>
          </React.Fragment>
        ))}
      </span>
    </span>
  );
}

function Row({ icon: Icon, title, detail, children, className }) {
  return (
    <div className={cn("flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2.5", className)}>
      <Icon className="size-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm">{title}</div>
        {detail ? <div className="truncate text-xs text-muted-foreground">{detail}</div> : null}
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  );
}

function VolumeRow({ resource, volume, onBackup }) {
  const mounted = volume.mountedAt.join(", ");
  return (
    <Row
      icon={FolderOpen}
      title={<Mono className="text-[13px]">{mounted}</Mono>}
      detail={volume.type === "volume" ? `volume ${volume.name}` : volume.hostPath}
    >
      {volume.jobs?.length ? (
        <BackedUp jobs={volume.jobs} />
      ) : volume.path ? (
        <Button
          size="sm"
          variant="outline"
          onClick={() =>
            onBackup({
              kind: "folder",
              path: volume.path,
              title: `${primaryName(resource)} files`,
              defaultName:
                resource.volumes.filter((v) => !v.databaseData).length > 1
                  ? `${primaryName(resource)} ${mounted}`
                  : `${primaryName(resource)} files`,
              description: `Backs up ${volume.path}, mounted at ${mounted} in ${volume.containers.join(", ")}.`,
            })
          }
        >
          <Plus />
          Back up
        </Button>
      ) : (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="text-[13px] text-muted-foreground">Not reachable</span>
          </TooltipTrigger>
          <TooltipContent className="max-w-xs">
            This app does not see {volume.hostPath}. Mount it into the restic-web container to back it
            up.
          </TooltipContent>
        </Tooltip>
      )}
    </Row>
  );
}

/**
 * DatabaseServerRow is one database container. Its databases are listed on
 * demand (that runs the engine's client inside the container).
 */
function DatabaseServerRow({ resource, server, onBackup }) {
  const [open, setOpen] = React.useState(false);
  const [state, setState] = React.useState({ loading: false, databases: null, error: "" });
  const running = server.state === "running";

  const toggle = async () => {
    const next = !open;
    setOpen(next);
    if (!next || state.databases) return;
    setState({ loading: true, databases: null, error: "" });
    const res = await Docker.databases(server.container);
    setState(
      res.ok
        ? { loading: false, databases: res.body.databases || [], error: "" }
        : { loading: false, databases: null, error: errorOf(res, "Could not list the databases.") },
    );
  };

  const sourceFor = (db) => (server.sources || []).find((s) => s.database === db);

  return (
    <div>
      <Row
        icon={Database}
        title={
          <span className="flex items-center gap-2">
            {engineLabel(server.engine)}
            <Mono className="text-muted-foreground">{server.container}</Mono>
          </span>
        }
        detail={running ? server.image : `${server.state} · ${server.image}`}
      >
        <Button size="sm" variant="ghost" onClick={toggle} disabled={!running}>
          {open ? <ChevronDown /> : <ChevronRight />}
          Databases
        </Button>
      </Row>
      {open ? (
        <div className="border-t border-border bg-muted/30 pl-7">
          {state.loading ? (
            <div className="space-y-2 px-4 py-3">
              <Skeleton className="h-6 w-full" />
            </div>
          ) : state.error ? (
            <p className="px-4 py-3 text-[13px] text-destructive">{state.error}</p>
          ) : state.databases?.length === 0 ? (
            <p className="px-4 py-3 text-[13px] text-muted-foreground">This server has no databases yet.</p>
          ) : (
            (state.databases || []).map((db) => (
              <Row key={db} icon={Database} title={<Mono className="text-[13px]">{db}</Mono>}>
                {sourceFor(db)?.jobs?.length ? (
                  <BackedUp jobs={sourceFor(db).jobs} />
                ) : (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() =>
                      onBackup({
                        kind: "database",
                        container: server.container,
                        database: db,
                        title: `the ${db} database`,
                        defaultName: `${primaryName(resource)} ${db}`,
                        description: `Dumps ${db} inside ${server.container} with the ${engineLabel(server.engine)} tools and keeps each dump as a snapshot. The database stays online.`,
                      })
                    }
                  >
                    <Plus />
                    Back up
                  </Button>
                )}
              </Row>
            ))
          )}
        </div>
      ) : null}
    </div>
  );
}

/** LinkedDatabaseRow is the database a site's settings point at. */
function LinkedDatabaseRow({ resource, link, byKey, onBackup }) {
  const server = byKey[link.resource]?.databases.find((d) => d.container === link.container);
  const jobs = server?.sources?.find((s) => s.database === link.database)?.jobs;
  return (
    <Row
      icon={Database}
      title={
        <span>
          Uses database <Mono className="text-[13px]">{link.database}</Mono>
        </span>
      }
      detail={
        link.container
          ? `${engineLabel(link.engine)} on ${byKey[link.resource]?.name || link.container}`
          : `on ${link.host} (not found on this host)`
      }
    >
      {jobs?.length ? (
        <BackedUp jobs={jobs} />
      ) : link.container ? (
        <Button
          size="sm"
          variant="outline"
          onClick={() =>
            onBackup({
              kind: "database",
              container: link.container,
              database: link.database,
              title: `the ${link.database} database`,
              defaultName: `${primaryName(resource)} database`,
              description: `Dumps ${link.database} inside ${link.container}, the database ${primaryName(resource)} is configured to use. The database stays online.`,
            })
          }
        >
          <Plus />
          Back up
        </Button>
      ) : null}
    </Row>
  );
}

function ResourceCard({ resource, byKey, onBackup }) {
  const files = resource.volumes.filter((v) => !v.databaseData);
  const stopped = resource.containers.filter((c) => c.state !== "running");
  const Icon = resource.kind === "database" ? Database : resource.domains.length ? Globe : Boxes;
  const nothing = files.length === 0 && resource.databases.length === 0 && resource.linkedDatabases.length === 0;

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-start gap-3 p-4">
        <span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground">
          <Icon className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <p className="display truncate text-sm font-medium">{resource.name}</p>
            <Badge variant="outline">{resource.kind}</Badge>
            {resource.self ? <Badge>This app</Badge> : null}
            {stopped.length ? (
              <Badge variant="warning">
                {stopped.length === resource.containers.length ? "stopped" : `${stopped.length} stopped`}
              </Badge>
            ) : null}
          </div>
          <p className="mt-0.5 flex flex-wrap gap-x-2 text-[13px] text-muted-foreground">
            {resource.domains.map((d) => (
              <a
                key={d}
                href={`https://${d}`}
                target="_blank"
                rel="noopener noreferrer"
                className="hover:text-foreground hover:underline"
              >
                {d}
              </a>
            ))}
            {resource.environment ? <span>{resource.environment}</span> : null}
          </p>
        </div>
      </div>
      {nothing ? null : (
        <div className="divide-y divide-border border-t border-border">
          {files.map((v) => (
            <VolumeRow key={v.hostPath} resource={resource} volume={v} onBackup={onBackup} />
          ))}
          {resource.linkedDatabases.map((l) => (
            <LinkedDatabaseRow
              key={l.host + "/" + l.database}
              resource={resource}
              link={l}
              byKey={byKey}
              onBackup={onBackup}
            />
          ))}
          {resource.databases.map((db) => (
            <DatabaseServerRow key={db.containerId} resource={resource} server={db} onBackup={onBackup} />
          ))}
        </div>
      )}
    </Card>
  );
}

function matches(resource, q) {
  if (!q) return true;
  const hay = [
    resource.name,
    resource.project,
    resource.environment,
    ...resource.domains,
    ...resource.containers.map((c) => c.name),
  ]
    .join(" ")
    .toLowerCase();
  return hay.includes(q);
}

export default function CoolifyRoute() {
  const docker = useDockerStatus();
  const [query, setQuery] = React.useState("");
  const [showOther, setShowOther] = React.useState(false);
  const [target, setTarget] = React.useState(null);

  const loader = React.useCallback(async () => {
    const [res, repos] = await Promise.all([Docker.resources(), Repositories.list()]);
    return {
      ok: res.ok,
      error: res.ok ? "" : errorOf(res, "Could not read the Docker host."),
      resources: res.body?.resources || [],
      repositories: repos.body?.repositories || [],
    };
  }, []);
  const { data, loading, reload } = useFetch(loader);

  React.useEffect(() => {
    document.title = `${docker?.coolify ? "Coolify" : "Docker"} · restic backup manager`;
  }, [docker]);

  const resources = data?.resources || [];
  const byKey = React.useMemo(() => Object.fromEntries(resources.map((r) => [r.key, r])), [resources]);
  const q = query.trim().toLowerCase();
  const coolify = resources.filter((r) => isCoolifyKind(r.kind) && matches(r, q));
  const other = resources.filter((r) => !isCoolifyKind(r.kind) && matches(r, q));
  const projects = React.useMemo(() => {
    const groups = new Map();
    for (const r of coolify) {
      const p = r.project || "other";
      if (!groups.has(p)) groups.set(p, []);
      groups.get(p).push(r);
    }
    return [...groups.entries()];
  }, [coolify]);

  return (
    <Page>
      <PageHeader
        title={docker?.coolify ? "Coolify" : "Docker"}
        description="The sites and databases running on this server. Pick a site's files or a database and it is backed up on a schedule — nothing to change in the deployment."
        actions={
          <Button variant="outline" onClick={reload} disabled={loading}>
            <RefreshCw className={loading ? "animate-spin" : undefined} />
            Refresh
          </Button>
        }
      />

      {loading && !data ? (
        <div className="space-y-3">
          <Skeleton className="h-24 w-full rounded-xl" />
          <Skeleton className="h-24 w-full rounded-xl" />
        </div>
      ) : !data?.ok ? (
        <EmptyState icon={Server} title="Docker is not reachable" description={data?.error || docker?.message} />
      ) : (
        <div className="space-y-8">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search sites, domains, projects"
              className="pl-9"
            />
          </div>

          {projects.length === 0 && other.length === 0 ? (
            <EmptyState icon={Search} title="Nothing matches" description="Try another name or domain." />
          ) : null}

          {projects.map(([project, list]) => (
            <section key={project} className="space-y-3">
              <h2 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{project}</h2>
              {list.map((r) => (
                <ResourceCard key={r.key} resource={r} byKey={byKey} onBackup={setTarget} />
              ))}
            </section>
          ))}

          {other.length > 0 ? (
            <section className="space-y-3">
              <button
                type="button"
                onClick={() => setShowOther((v) => !v)}
                className="flex items-center gap-1 text-xs font-medium tracking-wide text-muted-foreground uppercase hover:text-foreground"
              >
                {showOther ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
                Other containers ({other.length})
              </button>
              {showOther
                ? other.map((r) => <ResourceCard key={r.key} resource={r} byKey={byKey} onBackup={setTarget} />)
                : null}
            </section>
          ) : null}
        </div>
      )}

      <QuickBackupDialog
        target={target}
        onOpenChange={(open) => !open && setTarget(null)}
        repositories={data?.repositories || []}
        onCreated={reload}
      />
    </Page>
  );
}

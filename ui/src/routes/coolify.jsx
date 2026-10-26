import * as React from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import {
  ArrowUpRight,
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
  Unplug,
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { EmptyState, ErrorState, InlineError } from "@/components/empty";
import { Callout, CheckOption, Field } from "@/components/fields";
import { IconTile } from "@/components/kind";
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
            <Callout tone="warning" title="Add a storage repository first">
              Then come back here.{" "}
              <Link to="/repositories" className="font-medium underline underline-offset-2">
                Open Repositories
              </Link>
            </Callout>
          ) : (
            <div className="space-y-5">
              <Field label="Job name" htmlFor="quick-name">
                <Input id="quick-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
              </Field>
              <Field label="Store it in" htmlFor="quick-repo">
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
              </Field>
              <div className="space-y-2.5">
                <CheckOption
                  checked={daily}
                  onChange={setDaily}
                  title="Back up every day"
                  description="At 02:00. Change the schedule or add retention on the job page."
                />
                <CheckOption
                  checked={runNow}
                  onChange={setRunNow}
                  title="Run the first backup now"
                  description="Starts as soon as the job is created, so you can check it worked."
                />
              </div>
              <InlineError>{error}</InlineError>
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
    <span className="flex min-w-0 max-w-full flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
      <Badge variant="success">
        <Check className="size-3" />
        Backed up
      </Badge>
      <span className="min-w-0">
        by{" "}
        {jobs.map((j, i) => (
          <React.Fragment key={j.id}>
            {i > 0 ? ", " : ""}
            <Link to={`/jobs/${j.id}`} className="font-medium text-foreground hover:text-primary hover:underline">
              {j.name}
            </Link>
          </React.Fragment>
        ))}
      </span>
    </span>
  );
}

/** Row is one backup-able thing inside a resource: a volume or a database. */
function Row({ icon, tone = "neutral", title, detail, children, className }) {
  return (
    <div className={cn("flex flex-wrap items-center gap-x-4 gap-y-2.5 px-5 py-3.5 sm:px-6", className)}>
      <IconTile icon={icon} tone={tone} size="sm" />
      <div className="min-w-0 flex-1 basis-48">
        <div className="truncate">{title}</div>
        {detail ? <div className="mt-0.5 truncate text-sm text-muted-foreground">{detail}</div> : null}
      </div>
      <div className="flex min-w-0 shrink-0 items-center gap-2">{children}</div>
    </div>
  );
}

function VolumeRow({ resource, volume, onBackup }) {
  const mounted = volume.mountedAt.join(", ");
  return (
    <Row
      icon={FolderOpen}
      tone="fuchsia"
      title={<Mono>{mounted}</Mono>}
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
            <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
              <Unplug className="size-4" />
              Not reachable
            </span>
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
        icon={Server}
        tone="fuchsia"
        title={
          <span className="flex min-w-0 items-center gap-2">
            <span className="font-medium">{engineLabel(server.engine)}</span>
            <Mono className="truncate text-muted-foreground">{server.container}</Mono>
          </span>
        }
        detail={running ? server.image : `${server.state} · ${server.image}`}
      >
        <Button size="sm" variant="ghost" onClick={toggle} disabled={!running} aria-expanded={open}>
          {open ? <ChevronDown /> : <ChevronRight />}
          {open ? "Hide databases" : "Show databases"}
        </Button>
      </Row>
      {open ? (
        <div className="border-t border-border bg-secondary/40">
          <div className="ml-9 border-l border-border sm:ml-10">
            {state.loading ? (
              <div className="space-y-2 px-5 py-4">
                <Skeleton className="h-6 w-full" />
                <Skeleton className="h-6 w-2/3" />
              </div>
            ) : state.error ? (
              <p className="px-5 py-3.5 text-sm text-destructive">{state.error}</p>
            ) : state.databases?.length === 0 ? (
              <p className="px-5 py-3.5 text-sm text-muted-foreground">This server has no databases yet.</p>
            ) : (
              (state.databases || []).map((db) => (
              <Row key={db} icon={Database} tone="fuchsia" title={<Mono>{db}</Mono>} className="sm:px-5">
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
      tone="fuchsia"
      title={
        <span>
          Uses database <Mono>{link.database}</Mono>
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
      <div className="flex items-start gap-4 p-5 sm:px-6">
        <IconTile icon={Icon} tone={isCoolifyKind(resource.kind) ? "fuchsia" : "neutral"} />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <p className="display min-w-0 truncate text-md font-semibold">{resource.name}</p>
            <Badge variant="outline">{resource.kind}</Badge>
            {resource.self ? <Badge variant="running">This app</Badge> : null}
            {stopped.length ? (
              <Badge variant="warning">
                {stopped.length === resource.containers.length ? "stopped" : `${stopped.length} stopped`}
              </Badge>
            ) : null}
          </div>
          {resource.domains.length || resource.environment ? (
            <p className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
              {resource.domains.map((d) => (
                <a
                  key={d}
                  href={`https://${d}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex min-w-0 items-center gap-0.5 hover:text-primary"
                >
                  <span className="truncate">{d}</span>
                  <ArrowUpRight className="size-3.5 shrink-0" />
                </a>
              ))}
              {resource.environment ? (
                <span className="rounded-md bg-secondary px-1.5 py-0.5 text-xs">{resource.environment}</span>
              ) : null}
            </p>
          ) : null}
        </div>
      </div>
      {nothing ? (
        <p className="border-t border-border px-5 py-3.5 text-sm text-muted-foreground sm:px-6">
          No volumes or databases to back up.
        </p>
      ) : (
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
  // null until toggled: other containers start open when there is nothing else to show.
  const [showOther, setShowOther] = React.useState(null);
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
  const otherOpen = showOther ?? coolify.length === 0;
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
        <div className="space-y-4">
          <Skeleton className="h-11 w-full rounded-[10px]" />
          <Skeleton className="h-36 w-full rounded-2xl" />
          <Skeleton className="h-36 w-full rounded-2xl" />
        </div>
      ) : !data?.ok ? (
        <ErrorState
          title="Docker is not reachable"
          description={data?.error || docker?.message}
          onRetry={reload}
        />
      ) : (
        <div className="space-y-10">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-3.5 size-[18px] -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search sites, domains, projects"
              className="h-11 pl-10"
              aria-label="Search sites, domains and projects"
            />
          </div>

          {projects.length === 0 && other.length === 0 ? (
            <EmptyState
              framed
              icon={Search}
              title="Nothing matches"
              description={q ? `No site, domain or project matches “${query.trim()}”.` : "No containers were found on this host."}
              action={
                q ? (
                  <Button variant="outline" onClick={() => setQuery("")}>
                    Clear search
                  </Button>
                ) : null
              }
            />
          ) : null}

          {projects.map(([project, list]) => (
            <section key={project} className="space-y-4">
              <h2 className="display flex items-center gap-2.5 text-lg font-semibold">
                {project}
                <span className="rounded-full bg-secondary px-2 py-0.5 text-xs font-semibold tabular text-muted-foreground">
                  {list.length}
                </span>
              </h2>
              {list.map((r) => (
                <ResourceCard key={r.key} resource={r} byKey={byKey} onBackup={setTarget} />
              ))}
            </section>
          ))}

          {other.length > 0 ? (
            <section className="space-y-4">
              <button
                type="button"
                onClick={() => setShowOther(!otherOpen)}
                aria-expanded={otherOpen}
                className="display flex items-center gap-2 text-lg font-semibold text-muted-foreground transition-colors hover:text-foreground"
              >
                {otherOpen ? <ChevronDown className="size-5" /> : <ChevronRight className="size-5" />}
                Other containers
                <span className="rounded-full bg-secondary px-2 py-0.5 text-xs font-semibold tabular">
                  {other.length}
                </span>
              </button>
              {otherOpen
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

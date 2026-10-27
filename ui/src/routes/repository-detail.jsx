import * as React from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { Database, LoaderCircle, Pencil, PlugZap, Trash2, Unlock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Breadcrumb, Facts, Page, PageHeader } from "@/components/page";
import { Callout } from "@/components/fields";
import { ErrorState } from "@/components/empty";
import { IconTile, KindTile, repositoryIcon } from "@/components/kind";
import { RepositoryDialog, backendLabel } from "@/routes/repositories";
import { SnapshotsPanel } from "@/components/snapshots-panel";
import { SpeedTestCard } from "@/components/speed-test";
import { StatusDot } from "@/components/status-badge";
import { useConfirm } from "@/components/confirm";
import NotFound from "@/routes/not-found";
import { Jobs, Repositories, errorOf } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";
import { fmtRelative, repoLocation } from "@/lib/format";
import { handleStartResponse } from "@/lib/start-run";
import { cn } from "@/lib/utils";

const RESULT_TONE = { good: "success", bad: "destructive", info: "info" };

function OpResult({ result }) {
  if (!result) return null;
  return (
    <Callout tone={RESULT_TONE[result.kind] || "info"} className="mt-3">
      <p>{result.message}</p>
      {result.detail ? (
        <pre className="mt-1.5 max-h-32 overflow-auto scroll-thin whitespace-pre-wrap break-words font-mono text-xs opacity-80">
          {result.detail}
        </pre>
      ) : null}
    </Callout>
  );
}

/**
 * ActionRow is one maintenance action: what it does in a line, and the whole
 * row is the button. Destructive actions wear the destructive hue.
 */
function ActionRow({ icon, title, busyTitle, description, onClick, busy, disabled, destructive }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled || busy}
      className={cn(
        "flex w-full items-start gap-3.5 rounded-xl px-3 py-3 text-left transition-colors",
        "hover:bg-accent/60 focus-visible:bg-accent/60 focus-visible:outline-none",
        "disabled:cursor-not-allowed disabled:opacity-60",
      )}
    >
      <IconTile
        icon={busy ? LoaderCircle : icon}
        tone={destructive ? "destructive" : "cyan"}
        size="sm"
        className={busy ? "[&_svg]:animate-spin" : undefined}
      />
      <span className="min-w-0 flex-1">
        <span className={cn("block font-medium", destructive && "text-destructive")}>
          {busy ? busyTitle || title : title}
        </span>
        <span className="mt-0.5 block text-sm text-muted-foreground">{description}</span>
      </span>
    </button>
  );
}

export default function RepositoryDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { runsVersion } = useLive();
  const confirm = useConfirm();
  const [result, setResult] = React.useState(null);
  const [busy, setBusy] = React.useState("");
  const [editing, setEditing] = React.useState(false);

  const loader = React.useCallback(async () => {
    const [res, jobsR] = await Promise.all([Repositories.get(id), Jobs.list()]);
    if (res.status === 404) return { missing: true };
    if (!res.ok) throw new Error(errorOf(res, "Could not load the repository."));
    return {
      repository: res.body?.repository,
      jobs: jobsR.ok ? (jobsR.body?.jobs || []).filter((j) => j.repositoryId === id) : null,
    };
  }, [id]);
  const { data, loading, error, reload } = useFetch(loader);
  const loadSnapshots = React.useCallback(() => Repositories.snapshots(id), [id]);

  React.useEffect(() => {
    if (data?.repository) document.title = `${data.repository.name} · restic backup manager`;
  }, [data]);

  if (data?.missing) return <NotFound />;
  if (error && !data) {
    return (
      <Page>
        <Breadcrumb to="/repositories" label="Repositories" current="Repository" />
        <ErrorState
          title="Could not load this repository"
          description={`${error.message} Check that the server is running, then retry.`}
          onRetry={reload}
        />
      </Page>
    );
  }
  if (loading && !data) {
    return (
      <Page>
        <Skeleton className="mb-4 h-5 w-48" />
        <Skeleton className="mb-10 h-10 w-72" />
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
          <Skeleton className="h-80 rounded-2xl" />
          <Skeleton className="h-80 rounded-2xl" />
        </div>
      </Page>
    );
  }

  const repo = data.repository;
  const jobs = data.jobs;

  const test = async () => {
    setBusy("test");
    setResult({ kind: "info", message: "Testing the connection…" });
    const res = await Repositories.test(id);
    setBusy("");
    const b = res.body || {};
    setResult({
      kind: b.ok ? "good" : b.initialized === false && b.code !== "timeout" ? "info" : "bad",
      message: b.message || b.error || "The test failed.",
      detail: b.detail,
    });
  };

  const initialize = async () => {
    setBusy("init");
    const res = await Repositories.init(id);
    setBusy("");
    if (!handleStartResponse(res, navigate, "Could not initialize the repository.")) {
      setResult({ kind: "bad", message: errorOf(res, "Could not initialize the repository.") });
    }
  };

  const unlock = async () => {
    setBusy("unlock");
    const res = await Repositories.unlock(id);
    setBusy("");
    const b = res.body || {};
    setResult({
      kind: b.ok ? "good" : "bad",
      message: b.message || b.error || (b.ok ? "Stale locks removed." : "Could not unlock."),
    });
    if (b.ok) toast.success("Stale locks removed.");
  };

  const resetRepo = async () => {
    const ok = await confirm({
      title: `Empty “${repo.name}”?`,
      description:
        "Forget every snapshot in this repository and prune unused data. Jobs are not deleted. The repository stays initialized. This cannot be undone.",
      confirmLabel: "Empty repository",
      destructive: true,
    });
    if (!ok) return;
    setBusy("reset");
    const res = await Repositories.reset(id);
    setBusy("");
    if (!handleStartResponse(res, navigate, "Could not empty the repository.")) {
      setResult({ kind: "bad", message: errorOf(res, "Could not empty the repository.") });
    }
  };

  const isS3 = repo.backendType === "S3";

  return (
    <Page>
      <PageHeader
        breadcrumb={<Breadcrumb to="/repositories" label="Repositories" current={repo.name} />}
        icon={<IconTile icon={repositoryIcon(repo)} tone="cyan" size="lg" />}
        title={repo.name}
        description={
          <span className="flex min-w-0 items-center gap-2.5">
            <span className="shrink-0">{backendLabel(repo)}</span>
            <span aria-hidden="true">·</span>
            <span className="min-w-0 truncate font-mono text-sm" title={repoLocation(repo)}>
              {repoLocation(repo)}
            </span>
          </span>
        }
        actions={
          <Button variant="outline" onClick={() => setEditing(true)}>
            <Pencil />
            Edit
          </Button>
        }
      />

      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <SnapshotsPanel
          title="All snapshots"
          description="Everything stored in this repository, from every job."
          load={loadSnapshots}
          repositoryId={id}
          reloadKey={runsVersion}
        />

        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Details</CardTitle>
            </CardHeader>
            <CardContent className="space-y-6">
              <Facts
                className="sm:grid-cols-1"
                items={[
                  { label: "Backend", value: backendLabel(repo) },
                  isS3
                    ? { label: "Endpoint", value: repo.endpoint, mono: true }
                    : { label: "Directory", value: repo.localPath, mono: true },
                  isS3 ? { label: "Bucket", value: repo.bucket, mono: true } : null,
                  isS3 ? { label: "Region", value: repo.region } : null,
                  {
                    label: "Encryption",
                    value: repo.hasPassword ? "Password stored" : "No password stored",
                  },
                ]}
              />
              {jobs ? (
                <div>
                  <p className="text-sm text-muted-foreground">
                    Jobs storing here <span className="tabular">({jobs.length})</span>
                  </p>
                  {jobs.length === 0 ? (
                    <p className="mt-1 text-sm">
                      None yet.{" "}
                      <Link to="/jobs" className="font-medium text-primary hover:underline">
                        Create a job
                      </Link>{" "}
                      to back something up here.
                    </p>
                  ) : (
                    <ul className="-mx-2 mt-1.5 space-y-0.5">
                      {jobs.map((j) => (
                        <li key={j.id}>
                          <Link
                            to={`/jobs/${j.id}`}
                            className="flex items-center gap-3 rounded-lg px-2 py-1.5 transition-colors hover:bg-accent/60"
                          >
                            <KindTile kind="job" size="sm" />
                            <span className="min-w-0 flex-1 truncate font-medium">{j.name}</span>
                            {j.lastRun ? (
                              <span className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
                                <StatusDot status={j.lastRun.status} />
                                {fmtRelative(j.lastRun.startedAt)}
                              </span>
                            ) : (
                              <span className="shrink-0 text-xs text-muted-foreground">Never run</span>
                            )}
                          </Link>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              ) : null}
            </CardContent>
          </Card>

          <SpeedTestCard repositoryId={id} />

          <Card>
            <CardHeader>
              <div className="min-w-0 flex-1">
                <CardTitle>Maintenance</CardTitle>
                <CardDescription className="mt-1">
                  A backup sets the repository up on its own. These are for checking or repairing it
                  by hand.
                </CardDescription>
              </div>
            </CardHeader>
            <div className="px-3 pb-4">
              <ActionRow
                icon={PlugZap}
                title="Test connection"
                busyTitle="Testing…"
                description="Checks the credentials and that the repository answers."
                onClick={test}
                busy={busy === "test"}
              />
              <ActionRow
                icon={Database}
                title="Initialize"
                busyTitle="Starting…"
                description="Creates the repository structure, if it is not there yet."
                onClick={initialize}
                busy={busy === "init"}
              />
              <ActionRow
                icon={Unlock}
                title="Remove stale locks"
                busyTitle="Unlocking…"
                description="Clears locks left behind by a run that crashed."
                onClick={unlock}
                busy={busy === "unlock"}
              />
              <ActionRow
                icon={Trash2}
                title="Empty repository"
                busyTitle="Starting…"
                description="Forgets every snapshot and prunes the data. Jobs are kept."
                onClick={resetRepo}
                busy={busy === "reset"}
                destructive
              />
              <div className="px-3">
                <OpResult result={result} />
              </div>
            </div>
          </Card>
        </div>
      </div>

      <RepositoryDialog
        open={editing}
        onOpenChange={setEditing}
        repository={repo}
        onSaved={reload}
      />
    </Page>
  );
}

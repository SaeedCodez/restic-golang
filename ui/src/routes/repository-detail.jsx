import * as React from "react";
import { useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { CircleAlert, CircleCheck, Database, Info, Pencil, PlugZap, Unlock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Breadcrumb, Mono, Page, PageHeader } from "@/components/page";
import { RepositoryDialog } from "@/routes/repositories";
import { SnapshotsPanel } from "@/components/snapshots-panel";
import NotFound from "@/routes/not-found";
import { Repositories, errorOf } from "@/lib/api";
import { useFetch } from "@/lib/use-fetch";
import { useLive } from "@/lib/live";
import { repoLocation } from "@/lib/format";
import { handleStartResponse } from "@/lib/start-run";
import { cn } from "@/lib/utils";

const RESULT_STYLES = {
  good: { cls: "bg-success/10 text-success", Icon: CircleCheck },
  bad: { cls: "bg-destructive/10 text-destructive", Icon: CircleAlert },
  info: { cls: "bg-primary/10 text-primary", Icon: Info },
};

function OpResult({ result }) {
  if (!result) return null;
  const { cls, Icon } = RESULT_STYLES[result.kind] || RESULT_STYLES.info;
  return (
    <div className={cn("mx-5 mb-5 flex items-start gap-2 rounded-lg px-3 py-2.5 text-[13px]", cls)}>
      <Icon className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 flex-1">
        <p className="break-words">{result.message}</p>
        {result.detail ? (
          <pre className="mt-1.5 max-h-32 overflow-auto scroll-thin whitespace-pre-wrap break-words font-mono text-xs opacity-80">
            {result.detail}
          </pre>
        ) : null}
      </div>
    </div>
  );
}

export default function RepositoryDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { runsVersion } = useLive();
  const [result, setResult] = React.useState(null);
  const [busy, setBusy] = React.useState("");
  const [editing, setEditing] = React.useState(false);

  const loader = React.useCallback(async () => {
    const res = await Repositories.get(id);
    if (res.status === 404) return { missing: true };
    return { repository: res.body?.repository };
  }, [id]);
  const { data, loading, reload } = useFetch(loader);
  const loadSnapshots = React.useCallback(() => Repositories.snapshots(id), [id]);

  React.useEffect(() => {
    if (data?.repository) document.title = `${data.repository.name} · restic backup manager`;
  }, [data]);

  if (data?.missing) return <NotFound />;
  if (loading && !data) {
    return (
      <Page>
        <Skeleton className="mb-4 h-7 w-52" />
        <Skeleton className="h-32 w-full rounded-xl" />
      </Page>
    );
  }

  const repo = data.repository;

  const test = async () => {
    setBusy("test");
    setResult({ kind: "info", message: "Testing the connection…" });
    const res = await Repositories.test(id);
    setBusy("");
    const b = res.body || {};
    setResult({
      kind: b.ok ? "good" : b.initialized === false ? "info" : "bad",
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

  return (
    <Page>
      <PageHeader
        breadcrumb={<Breadcrumb to="/repositories" label="Repositories" current={repo.name} />}
        title={repo.name}
        description={
          <span className="flex flex-wrap items-center gap-2">
            <span>{repo.backendType === "S3" ? "S3-compatible" : "Local directory"}</span>
            <Mono className="text-muted-foreground">{repoLocation(repo)}</Mono>
          </span>
        }
        actions={
          <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
            <Pencil />
            Edit
          </Button>
        }
      />

      <div className="space-y-4">
        <Card>
          <CardHeader>
            <div className="min-w-0 flex-1">
              <CardTitle>Maintenance</CardTitle>
              <CardDescription className="mt-1">
                A backup initializes the repository on its own, so these are here for when you
                want to check or repair it by hand.
              </CardDescription>
            </div>
          </CardHeader>
          <div className="flex flex-wrap gap-2 px-5 pb-5">
            <Button variant="outline" size="sm" onClick={test} disabled={busy === "test"}>
              <PlugZap />
              {busy === "test" ? "Testing…" : "Test connection"}
            </Button>
            <Button variant="outline" size="sm" onClick={initialize} disabled={busy === "init"}>
              <Database />
              Initialize
            </Button>
            <Button variant="outline" size="sm" onClick={unlock} disabled={busy === "unlock"}>
              <Unlock />
              {busy === "unlock" ? "Unlocking…" : "Remove stale locks"}
            </Button>
          </div>
          <OpResult result={result} />
        </Card>

        <SnapshotsPanel
          title="All snapshots"
          description="Everything stored in this repository, from every job."
          load={loadSnapshots}
          repositoryId={id}
          reloadKey={runsVersion}
        />
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

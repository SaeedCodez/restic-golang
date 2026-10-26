import { Link } from "react-router-dom";
import { ArrowRight, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { KindTile } from "@/components/kind";
import { cn } from "@/lib/utils";

/**
 * GettingStarted walks a brand-new install through the three things it needs.
 * Step 3 deep-links to Jobs with state.create so the New job dialog opens there.
 */
export function GettingStarted({ hasFolder, hasRepo }) {
  const steps = [
    {
      done: hasFolder,
      kind: "folder",
      title: "Add a folder to back up",
      body: "Name the directory you want protected, so you never retype the path.",
      to: "/folders",
      cta: "Add a folder",
    },
    {
      done: hasRepo,
      kind: "repository",
      title: "Add a storage repository",
      body: "Where restic keeps the encrypted backups. A local directory works with no credentials.",
      to: "/repositories",
      cta: "Add a repository",
    },
    {
      done: false,
      kind: "job",
      title: "Create a job",
      body: "Pair the two. The job is the thing you run and come back to.",
      cta: "Create a job",
      createJob: true,
      ready: hasFolder && hasRepo,
    },
  ];
  const doneCount = steps.filter((s) => s.done).length;

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-end justify-between gap-3 px-6 pb-5 pt-6">
        <div>
          <h2 className="display text-xl font-semibold">Get set up</h2>
          <p className="mt-1 text-base text-muted-foreground">
            Three steps, once. After that, backing up is one click.
          </p>
        </div>
        <span className="text-sm font-medium tabular text-muted-foreground">
          {doneCount} of {steps.length} done
        </span>
      </div>
      <ol className="grid grid-cols-1 gap-px border-t border-border bg-border md:grid-cols-3">
        {steps.map((step, i) => (
          <li key={step.title} className="flex flex-col gap-4 bg-card p-6">
            <div className="flex items-center justify-between">
              <KindTile kind={step.kind} />
              {step.done ? (
                <span className="flex items-center gap-1.5 rounded-full bg-success/12 px-2.5 py-1 text-xs font-semibold text-success">
                  <Check className="size-3.5" />
                  Done
                </span>
              ) : (
                <span className="text-xs font-semibold tabular text-muted-foreground">
                  Step {i + 1}
                </span>
              )}
            </div>
            <div className="flex-1">
              <p className={cn("font-semibold", step.done && "text-muted-foreground")}>
                {step.title}
              </p>
              <p className="mt-1 text-sm text-muted-foreground">{step.body}</p>
            </div>
            {step.done ? null : step.createJob ? (
              step.ready ? (
                <Button asChild className="self-start">
                  <Link to="/jobs" state={{ create: true }}>
                    {step.cta}
                    <ArrowRight />
                  </Link>
                </Button>
              ) : (
                <Button disabled className="self-start">
                  {step.cta}
                </Button>
              )
            ) : (
              <Button variant="outline" asChild className="self-start">
                <Link to={step.to} state={{ create: true }}>
                  {step.cta}
                  <ArrowRight />
                </Link>
              </Button>
            )}
          </li>
        ))}
      </ol>
    </Card>
  );
}

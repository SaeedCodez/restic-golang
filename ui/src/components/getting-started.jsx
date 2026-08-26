import { Link } from "react-router-dom";
import { FolderOpen, HardDrive, Play } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";

/**
 * GettingStarted walks a brand-new install through the three things it needs.
 * Step 3 deep-links to Jobs with state.create so the New job dialog opens there.
 */
export function GettingStarted({ hasFolder, hasRepo }) {
  const steps = [
    {
      done: hasFolder,
      title: "Add a folder to back up",
      body: "Name the directory you want protected, so you never retype the path.",
      to: "/folders",
      cta: "Add a folder",
      icon: FolderOpen,
    },
    {
      done: hasRepo,
      title: "Add a storage repository",
      body: "Where restic keeps the encrypted backups. A local directory works with no credentials.",
      to: "/repositories",
      cta: "Add a repository",
      icon: HardDrive,
    },
    {
      done: false,
      title: "Create a job",
      body: "Pair the two. The job is the thing you run and come back to.",
      cta: "Create a job",
      icon: Play,
      createJob: true,
      ready: hasFolder && hasRepo,
    },
  ];

  return (
    <Card className="overflow-hidden">
      <div className="border-b border-border px-5 py-4">
        <h2 className="text-[15px] font-semibold tracking-tight">Get set up</h2>
        <p className="mt-1 text-[13px] text-muted-foreground">
          Three steps, once. After that, backing up is one click.
        </p>
      </div>
      <ol className="divide-y divide-border">
        {steps.map((step, i) => (
          <li key={step.title} className="flex flex-wrap items-center gap-4 px-5 py-4">
            <span
              className={
                step.done
                  ? "grid size-7 shrink-0 place-items-center rounded-full bg-success/15 text-xs font-semibold text-success"
                  : "grid size-7 shrink-0 place-items-center rounded-full bg-muted text-xs font-semibold text-muted-foreground"
              }
            >
              {step.done ? "✓" : i + 1}
            </span>
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">{step.title}</p>
              <p className="text-[13px] text-muted-foreground">{step.body}</p>
            </div>
            {step.done ? null : step.createJob ? (
              step.ready ? (
                <Button size="sm" variant="outline" asChild>
                  <Link to="/jobs" state={{ create: true }}>
                    {step.cta}
                  </Link>
                </Button>
              ) : (
                <Button size="sm" disabled>
                  {step.cta}
                </Button>
              )
            ) : (
              <Button size="sm" variant="outline" asChild>
                <Link to={step.to} state={{ create: true }}>
                  {step.cta}
                </Link>
              </Button>
            )}
          </li>
        ))}
      </ol>
    </Card>
  );
}

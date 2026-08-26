import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Jobs, errorOf } from "@/lib/api";
import { fmtRelative } from "@/lib/format";
import {
  WEEKDAYS,
  describeScheduleTitle,
  scheduleFormFromJob,
  schedulePayload,
} from "@/lib/schedule";
import { cn } from "@/lib/utils";

/**
 * ScheduleEditor edits a job's automatic-backup cadence. Pausing sets
 * enabled=false but keeps the rest of the config.
 */
export function ScheduleEditor({ job, onSaved }) {
  const [form, setForm] = React.useState(() => scheduleFormFromJob(job));
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    setForm(scheduleFormFromJob(job));
    setError("");
  }, [job]);

  const set = (patch) => setForm((f) => ({ ...f, ...patch }));

  const toggleDay = (day) => {
    setForm((f) => {
      const has = (f.weekdays || []).includes(day);
      const weekdays = has
        ? f.weekdays.filter((d) => d !== day)
        : [...(f.weekdays || []), day].sort((a, b) => a - b);
      return { ...f, weekdays };
    });
  };

  const save = async (nextForm) => {
    setBusy(true);
    setError("");
    const schedule = schedulePayload(nextForm);
    const res = await Jobs.update(job.id, {
      name: job.name,
      folderId: job.folderId,
      repositoryId: job.repositoryId,
      schedule,
      retention: job.retention || null,
    });
    setBusy(false);
    if (res.ok) {
      toast.success(
        schedule?.enabled
          ? `Automatic backup: ${describeScheduleTitle(schedule)}.`
          : "Automatic backup paused.",
      );
      onSaved?.(res.body.job);
    } else {
      setError(errorOf(res, "Could not save the schedule."));
    }
  };

  const onSubmit = (e) => {
    e.preventDefault();
    if (form.enabled && form.kind === "weekly" && !(form.weekdays || []).length) {
      setError("Choose at least one weekday.");
      return;
    }
    save(form);
  };

  const state = job.scheduleState || "off";
  const nextLine =
    state === "overdue"
      ? "Overdue — a backup will run when the app can."
      : state === "running"
        ? "A backup is running now."
        : job.nextDueAt
          ? `Next run ${fmtRelative(job.nextDueAt)}`
          : null;

  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Automatic backup</CardTitle>
          <CardDescription className="mt-1">
            Runs this job on a schedule while the app is running. Closing the browser is
            fine; quitting the app is not.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-4">
          <label className="flex cursor-pointer items-center justify-between gap-3 rounded-md border border-border px-3 py-2.5">
            <div>
              <p className="text-sm font-medium">Enable schedule</p>
              <p className="text-[13px] text-muted-foreground">
                {form.enabled
                  ? describeScheduleTitle(schedulePayload(form)) || "On"
                  : "Off — run backups manually"}
              </p>
            </div>
            <input
              type="checkbox"
              className="size-4 accent-foreground"
              checked={form.enabled}
              onChange={(e) => set({ enabled: e.target.checked })}
              disabled={busy}
            />
          </label>

          {form.enabled ? (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="sched-kind">How often</Label>
                <Select value={form.kind} onValueChange={(kind) => set({ kind })}>
                  <SelectTrigger id="sched-kind">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="hourly">Hourly</SelectItem>
                    <SelectItem value="every">Every N hours</SelectItem>
                    <SelectItem value="daily">Daily</SelectItem>
                    <SelectItem value="weekly">Weekly</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              {form.kind === "every" ? (
                <div className="space-y-2">
                  <Label htmlFor="sched-every">Interval</Label>
                  <Select value={form.every} onValueChange={(every) => set({ every })}>
                    <SelectTrigger id="sched-every">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="4h">Every 4 hours</SelectItem>
                      <SelectItem value="6h">Every 6 hours</SelectItem>
                      <SelectItem value="12h">Every 12 hours</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              ) : null}

              {form.kind === "daily" || form.kind === "weekly" ? (
                <div className="space-y-2">
                  <Label htmlFor="sched-at">Time of day</Label>
                  <Input
                    id="sched-at"
                    type="time"
                    value={form.at}
                    onChange={(e) => set({ at: e.target.value })}
                    className="w-40 font-mono"
                  />
                </div>
              ) : null}

              {form.kind === "weekly" ? (
                <div className="space-y-2">
                  <Label>Days</Label>
                  <div className="flex flex-wrap gap-1.5">
                    {WEEKDAYS.map((d) => {
                      const on = (form.weekdays || []).includes(d.value);
                      return (
                        <button
                          key={d.value}
                          type="button"
                          onClick={() => toggleDay(d.value)}
                          className={cn(
                            "size-9 rounded-md border text-xs font-medium transition-colors",
                            on
                              ? "border-foreground bg-foreground text-background"
                              : "border-border text-muted-foreground hover:border-input",
                          )}
                          aria-pressed={on}
                          aria-label={d.label}
                          title={d.label}
                        >
                          {d.short}
                        </button>
                      );
                    })}
                  </div>
                </div>
              ) : null}
            </div>
          ) : null}

          {nextLine && (job.schedule?.enabled || form.enabled) ? (
            <p
              className={cn(
                "text-[13px]",
                state === "overdue" ? "text-warning" : "text-muted-foreground",
              )}
            >
              {nextLine}
            </p>
          ) : null}

          {error ? (
            <p className="rounded-md bg-destructive/10 px-3 py-2 text-[13px] text-destructive">
              {error}
            </p>
          ) : null}

          <div className="flex justify-end">
            <Button type="submit" size="sm" disabled={busy}>
              {busy ? "Saving…" : "Save schedule"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

import * as React from "react";
import { toast } from "sonner";
import { CalendarClock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { InlineError } from "@/components/empty";
import { Field, SwitchRow } from "@/components/fields";
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
    const res = await Jobs.save(job, { schedule });
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
          <CardDescription className="mt-1.5">
            Runs this job on a schedule while the app is running. Closing the browser is
            fine; quitting the app is not.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-5">
          <SwitchRow
            id={`sched-enabled-${job.id}`}
            checked={form.enabled}
            onCheckedChange={(enabled) => set({ enabled })}
            disabled={busy}
            title="Enable schedule"
            description={
              form.enabled
                ? describeScheduleTitle(schedulePayload(form)) || "On"
                : "Off — run backups manually"
            }
          />

          {form.enabled ? (
            <div className="space-y-4">
              <Field label="How often" htmlFor="sched-kind">
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
              </Field>

              {form.kind === "every" ? (
                <Field label="Interval" htmlFor="sched-every">
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
                </Field>
              ) : null}

              {form.kind === "daily" || form.kind === "weekly" ? (
                <Field label="Time of day" htmlFor="sched-at">
                  <Input
                    id="sched-at"
                    type="time"
                    value={form.at}
                    onChange={(e) => set({ at: e.target.value })}
                    className="w-40 font-mono"
                  />
                </Field>
              ) : null}

              {form.kind === "weekly" ? (
                <Field label="Days">
                  <div className="flex flex-wrap gap-1.5">
                    {WEEKDAYS.map((d) => {
                      const on = (form.weekdays || []).includes(d.value);
                      return (
                        <button
                          key={d.value}
                          type="button"
                          onClick={() => toggleDay(d.value)}
                          className={cn(
                            "size-9 rounded-[10px] border text-sm font-medium transition-colors",
                            on
                              ? "border-primary bg-primary text-primary-foreground"
                              : "border-border bg-card text-muted-foreground hover:border-input hover:text-foreground",
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
                </Field>
              ) : null}
            </div>
          ) : null}

          {nextLine && (job.schedule?.enabled || form.enabled) ? (
            <p
              className={cn(
                "flex items-center gap-2 text-sm",
                state === "overdue" ? "text-warning" : "text-muted-foreground",
              )}
            >
              <CalendarClock className="size-4 shrink-0" />
              {nextLine}
            </p>
          ) : null}

          <InlineError>{error}</InlineError>

          <div className="flex justify-end">
            <Button type="submit" disabled={busy}>
              {busy ? "Saving…" : "Save schedule"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

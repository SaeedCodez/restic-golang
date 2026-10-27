import * as React from "react";
import { toast } from "sonner";
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
import { useConfirm } from "@/components/confirm";
import { InlineError } from "@/components/empty";
import { Field, SwitchRow } from "@/components/fields";
import { Jobs, errorOf } from "@/lib/api";
import { handleStartResponse } from "@/lib/start-run";
import {
  RETENTION_PRESETS,
  describeRetentionTitle,
  retentionFormFromJob,
  retentionPayload,
} from "@/lib/retention";
import { useNavigate } from "react-router-dom";

const CUSTOM_FIELDS = [
  { key: "keepLast", label: "Keep last", hint: "Most recent snapshots" },
  { key: "keepHourly", label: "Keep hourly", hint: "One per hour" },
  { key: "keepDaily", label: "Keep daily", hint: "One per day" },
  { key: "keepWeekly", label: "Keep weekly", hint: "One per week" },
  { key: "keepMonthly", label: "Keep monthly", hint: "One per month" },
  { key: "keepWithinDays", label: "Keep within days", hint: "Anything newer than this many days" },
];

/**
 * RetentionEditor edits a job's snapshot keep-policy. Enabling applies
 * forget+prune after each successful backup, scoped to this job's tag.
 *
 * When `stay` is true, "Apply now" keeps the user on this page and surfaces
 * the new run in the job Activity section instead of navigating away.
 */
export function RetentionEditor({ job, onSaved, stay = false }) {
  const navigate = useNavigate();
  const confirm = useConfirm();
  const [form, setForm] = React.useState(() => retentionFormFromJob(job));
  const [busy, setBusy] = React.useState(false);
  const [applying, setApplying] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    setForm(retentionFormFromJob(job));
    setError("");
  }, [job]);

  const set = (patch) => setForm((f) => ({ ...f, ...patch }));

  const setPreset = (preset) => {
    const def = RETENTION_PRESETS.find((p) => p.id === preset);
    if (def?.values) {
      set({ preset, ...def.values });
    } else {
      set({ preset });
    }
  };

  const save = async (nextForm) => {
    setBusy(true);
    setError("");
    const retention = retentionPayload(nextForm);
    if (retention?.enabled) {
      const hasRule =
        retention.keepLast > 0 ||
        retention.keepHourly > 0 ||
        retention.keepDaily > 0 ||
        retention.keepWeekly > 0 ||
        retention.keepMonthly > 0 ||
        retention.keepWithinDays > 0;
      if (!hasRule) {
        setBusy(false);
        setError("Set at least one keep rule, or choose a preset.");
        return;
      }
    }
    const res = await Jobs.save(job, { retention });
    setBusy(false);
    if (res.ok) {
      toast.success(
        retention?.enabled
          ? `Retention: ${describeRetentionTitle(retention)}.`
          : "Retention paused.",
      );
      onSaved?.(res.body.job);
    } else {
      setError(errorOf(res, "Could not save retention."));
    }
  };

  const onSubmit = (e) => {
    e.preventDefault();
    save(form);
  };

  // A preview runs `restic forget --dry-run`: its log lists, per group, how
  // many snapshots the saved policy keeps and would remove.
  const preview = async () => {
    setApplying(true);
    const res = await Jobs.retention(job.id, { dryRun: true });
    setApplying(false);
    handleStartResponse(res, navigate, "Could not start the retention preview.", { stay: false });
  };

  const applyNow = async () => {
    const ok = await confirm({
      title: "Apply retention now?",
      description:
        "This forgets old snapshots for this job according to the saved policy, then prunes unused data from the repository. This cannot be undone.",
      confirmLabel: "Apply retention",
      destructive: true,
    });
    if (!ok) return;
    setApplying(true);
    const res = await Jobs.retention(job.id);
    setApplying(false);
    if (handleStartResponse(res, navigate, "Could not start retention.", { stay })) {
      onSaved?.();
    }
  };

  const policyLine = describeRetentionTitle(retentionPayload(form));
  const retentionEnabled = Boolean(job.retention?.enabled);

  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Snapshot retention</CardTitle>
          <CardDescription className="mt-1.5">
            After each successful backup, forget old snapshots for this job and prune
            unused data. Other jobs sharing the repository are left alone.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-5">
          <SwitchRow
            id={`ret-enabled-${job.id}`}
            checked={form.enabled}
            onCheckedChange={(enabled) => set({ enabled })}
            disabled={busy}
            title="Enable retention"
            description={form.enabled ? policyLine || "On" : "Off — keep all snapshots"}
          />

          {form.enabled ? (
            <div className="space-y-4">
              <Field
                label="Policy"
                htmlFor="ret-preset"
                hint={RETENTION_PRESETS.find((p) => p.id === form.preset)?.description}
              >
                <Select value={form.preset} onValueChange={setPreset}>
                  <SelectTrigger id="ret-preset">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {RETENTION_PRESETS.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>

              {form.preset === "custom" ? (
                <div className="grid grid-cols-2 gap-x-3 gap-y-4">
                  {CUSTOM_FIELDS.map((field) => (
                    <Field key={field.key} label={field.label} htmlFor={`ret-${field.key}`} hint={field.hint}>
                      <Input
                        id={`ret-${field.key}`}
                        type="number"
                        min={0}
                        step={1}
                        value={form[field.key] || 0}
                        onChange={(e) =>
                          set({ [field.key]: Math.max(0, parseInt(e.target.value || "0", 10) || 0) })
                        }
                        className="font-mono"
                      />
                    </Field>
                  ))}
                </div>
              ) : (
                <p className="rounded-xl bg-secondary/60 px-4 py-3 text-sm text-muted-foreground">
                  Keeps <span className="font-medium text-foreground">{policyLine.toLowerCase()}</span>.
                  Snapshots that match any rule are kept.
                </p>
              )}
            </div>
          ) : null}

          <InlineError>{error}</InlineError>

          <div className="flex flex-wrap items-center justify-end gap-2">
            {retentionEnabled ? (
              <>
                <Button
                  type="button"
                  variant="ghost"
                  disabled={busy || applying}
                  onClick={preview}
                >
                  Preview
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy || applying}
                  onClick={applyNow}
                >
                  {applying ? "Starting…" : "Apply now"}
                </Button>
              </>
            ) : null}
            <Button type="submit" disabled={busy}>
              {busy ? "Saving…" : "Save retention"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

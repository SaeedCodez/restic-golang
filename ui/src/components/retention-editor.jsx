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
import { useConfirm } from "@/components/confirm";
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
 */
export function RetentionEditor({ job, onSaved }) {
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
    const res = await Jobs.update(job.id, {
      name: job.name,
      folderId: job.folderId,
      repositoryId: job.repositoryId,
      schedule: job.schedule || null,
      retention,
    });
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
    handleStartResponse(res, navigate, "Could not start retention.");
    onSaved?.();
  };

  const policyLine = describeRetentionTitle(retentionPayload(form));
  const retentionEnabled = Boolean(job.retention?.enabled);

  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Snapshot retention</CardTitle>
          <CardDescription className="mt-1">
            After each successful backup, forget old snapshots for this job and prune
            unused data. Other jobs sharing the repository are left alone.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-4">
          <label className="flex cursor-pointer items-center justify-between gap-3 rounded-md border border-border px-3 py-2.5">
            <div>
              <p className="text-sm font-medium">Enable retention</p>
              <p className="text-[13px] text-muted-foreground">
                {form.enabled ? policyLine || "On" : "Off — keep all snapshots"}
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
                <Label htmlFor="ret-preset">Policy</Label>
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
                {RETENTION_PRESETS.find((p) => p.id === form.preset)?.description ? (
                  <p className="text-[13px] text-muted-foreground">
                    {RETENTION_PRESETS.find((p) => p.id === form.preset).description}
                  </p>
                ) : null}
              </div>

              {form.preset === "custom" ? (
                <div className="grid gap-3 sm:grid-cols-2">
                  {CUSTOM_FIELDS.map((field) => (
                    <div key={field.key} className="space-y-1.5">
                      <Label htmlFor={`ret-${field.key}`}>{field.label}</Label>
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
                      <p className="text-[12px] text-muted-foreground">{field.hint}</p>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-[13px] text-muted-foreground">
                  Keeps {policyLine.toLowerCase()}. Snapshots that match any rule are kept.
                </p>
              )}
            </div>
          ) : null}

          {error ? (
            <p className="rounded-md bg-destructive/10 px-3 py-2 text-[13px] text-destructive">
              {error}
            </p>
          ) : null}

          <div className="flex flex-wrap items-center justify-end gap-2">
            {retentionEnabled ? (
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={busy || applying}
                onClick={applyNow}
              >
                {applying ? "Starting…" : "Apply now"}
              </Button>
            ) : null}
            <Button type="submit" size="sm" disabled={busy}>
              {busy ? "Saving…" : "Save retention"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

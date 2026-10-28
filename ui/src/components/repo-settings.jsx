import * as React from "react";
import { Link } from "react-router-dom";
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
import { StatusDot } from "@/components/status-badge";
import { Repositories, errorOf } from "@/lib/api";
import { fmtRelative } from "@/lib/format";
import { fmtSpeed } from "@/lib/speed";

const EVERY_LABEL = { daily: "Daily", weekly: "Weekly", monthly: "Monthly" };
const READ_DATA = [
  { value: "none", label: "No: structure only (quick)" },
  { value: "1%", label: "Yes: 1% of the data" },
  { value: "5%", label: "Yes: 5% of the data" },
  { value: "10%", label: "Yes: 10% of the data" },
];

/** describeCheck is a schedule as one line, e.g. "Weekly at 04:00, reading 5% of the data". */
export function describeCheck(c) {
  if (!c?.enabled) return "";
  const read = c.readData ? `, reading ${c.readData} of the data` : "";
  return `${EVERY_LABEL[c.every] || c.every} at ${c.at}${read}`;
}

/** checkOutcome is a finished check as a short phrase. */
export function checkOutcome(check) {
  if (!check) return "";
  if (check.status === "success" || check.status === "success_warnings") return "No errors";
  if (check.status === "failed") return "Found errors";
  return "Did not finish";
}

function checkForm(repo) {
  const c = repo.checkSchedule;
  return {
    enabled: Boolean(c?.enabled),
    every: c?.every || "weekly",
    at: c?.at || "04:00",
    readData: c?.readData || "none",
  };
}

/**
 * CheckScheduleCard turns on the automatic repository check and shows how the
 * last one went. Turning it off keeps the choices for next time.
 */
export function CheckScheduleCard({ repo, onSaved }) {
  const [form, setForm] = React.useState(() => checkForm(repo));
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    setForm(checkForm(repo));
    setError("");
  }, [repo]);

  const set = (patch) => setForm((f) => ({ ...f, ...patch }));
  const payload = {
    enabled: form.enabled,
    every: form.every,
    at: form.at,
    readData: form.readData === "none" ? "" : form.readData,
  };

  const save = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    const res = await Repositories.setCheckSchedule(repo.id, payload);
    setBusy(false);
    if (res.ok) {
      toast.success(payload.enabled ? `Automatic check: ${describeCheck(payload)}.` : "Automatic check turned off.");
      onSaved?.();
    } else {
      setError(errorOf(res, "Could not save the check schedule."));
    }
  };

  const last = repo.lastCheck;
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Automatic check</CardTitle>
          <CardDescription className="mt-1.5">
            Checks on its own that the backups stored here are complete and readable, so damage
            is found before you need a restore. Backups to this repository wait while it runs:
            pick a quiet hour.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={save} className="space-y-5">
          <p className="flex items-center gap-2 text-sm">
            {last ? (
              <>
                <StatusDot status={last.status} />
                <span className={last.status === "failed" ? "text-destructive" : undefined}>
                  Last check: {checkOutcome(last)} · {fmtRelative(last.finishedAt || last.startedAt)}
                </span>
                <Link to={`/runs/${last.runId}`} className="ml-auto text-primary hover:underline">
                  Log
                </Link>
              </>
            ) : (
              <span className="text-muted-foreground">Never checked yet.</span>
            )}
          </p>

          <SwitchRow
            id={`check-enabled-${repo.id}`}
            checked={form.enabled}
            onCheckedChange={(enabled) => set({ enabled })}
            disabled={busy}
            title="Check automatically"
            description={form.enabled ? describeCheck(payload) : "Off: check by hand from Maintenance"}
          />

          {form.enabled ? (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <Field label="How often" htmlFor="check-every">
                  <Select value={form.every} onValueChange={(every) => set({ every })}>
                    <SelectTrigger id="check-every">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="daily">Daily</SelectItem>
                      <SelectItem value="weekly">Weekly</SelectItem>
                      <SelectItem value="monthly">Monthly</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
                <Field label="Time" htmlFor="check-at">
                  <Input
                    id="check-at"
                    type="time"
                    value={form.at}
                    onChange={(e) => set({ at: e.target.value })}
                    className="font-mono"
                  />
                </Field>
              </div>
              <Field
                label="Also read the data"
                htmlFor="check-read"
                hint="Reading downloads that share of the stored backups each time (it uses traffic), and finds damaged data, not only missing pieces."
              >
                <Select value={form.readData} onValueChange={(readData) => set({ readData })}>
                  <SelectTrigger id="check-read">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {READ_DATA.map((o) => (
                      <SelectItem key={o.value} value={o.value}>
                        {o.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
          ) : null}

          {repo.checkSchedule?.enabled && repo.nextCheckAt ? (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <CalendarClock className="size-4 shrink-0" />
              Next check {fmtRelative(repo.nextCheckAt)}
            </p>
          ) : null}

          <InlineError>{error}</InlineError>

          <div className="flex justify-end">
            <Button type="submit" disabled={busy}>
              {busy ? "Saving…" : "Save"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

// Speeds are shown in the app's units: 1 MB = 1024 KB, as in the speed test.
const toMB = (kib) => (kib > 0 ? String(Math.round((kib / 1024) * 100) / 100) : "");
const toKiB = (mb) => {
  const n = Number(String(mb).trim().replace(",", "."));
  return mb === "" || !Number.isFinite(n) || n <= 0 ? 0 : Math.round(n * 1024);
};

/** fmtLimit is a limit in KiB/s for reading: "5 MB/s" or "no limit". */
export function fmtLimit(kib) {
  return kib > 0 ? `${toMB(kib)} MB/s` : "no limit";
}

/**
 * SpeedLimitCard caps how fast restic moves data to and from the repository,
 * so a backup does not take all of the server's bandwidth.
 */
export function SpeedLimitCard({ repo, onSaved }) {
  const [upload, setUpload] = React.useState(() => toMB(repo.uploadLimitKiB));
  const [download, setDownload] = React.useState(() => toMB(repo.downloadLimitKiB));
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    setUpload(toMB(repo.uploadLimitKiB));
    setDownload(toMB(repo.downloadLimitKiB));
    setError("");
  }, [repo]);

  const bad = (v) => v.trim() !== "" && !(Number(v.trim().replace(",", ".")) >= 0);

  const save = async (e) => {
    e.preventDefault();
    if (bad(upload) || bad(download)) {
      setError("Enter a number of MB/s, or leave the field empty for no limit.");
      return;
    }
    setBusy(true);
    setError("");
    const body = { uploadLimitKiB: toKiB(upload), downloadLimitKiB: toKiB(download) };
    const res = await Repositories.setSpeedLimit(repo.id, body);
    setBusy(false);
    if (res.ok) {
      toast.success(`Upload: ${fmtLimit(body.uploadLimitKiB)}. Download: ${fmtLimit(body.downloadLimitKiB)}.`);
      onSaved?.();
    } else {
      setError(errorOf(res, "Could not save the speed limit."));
    }
  };

  const tested = repo.lastSpeedTest;
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Speed limit</CardTitle>
          <CardDescription className="mt-1.5">
            Keeps backups from using all of the server's bandwidth, so the sites on it stay fast
            while a backup runs. With a limit, backups take longer. Leave a field empty for no
            limit.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={save} className="space-y-5">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Upload (MB/s)" htmlFor="limit-up" hint="Backups sending data here.">
              <Input
                id="limit-up"
                inputMode="decimal"
                value={upload}
                onChange={(e) => setUpload(e.target.value)}
                placeholder="No limit"
                className="tabular"
              />
            </Field>
            <Field label="Download (MB/s)" htmlFor="limit-down" hint="Restores and checks reading it.">
              <Input
                id="limit-down"
                inputMode="decimal"
                value={download}
                onChange={(e) => setDownload(e.target.value)}
                placeholder="No limit"
                className="tabular"
              />
            </Field>
          </div>

          {tested ? (
            <p className="text-sm text-muted-foreground">
              The last speed test measured up {fmtSpeed(tested.uploadBytesPerSec)} and down{" "}
              {fmtSpeed(tested.downloadBytesPerSec)}. The speed test itself ignores these limits.
            </p>
          ) : null}

          <InlineError>{error}</InlineError>

          <div className="flex justify-end">
            <Button type="submit" disabled={busy}>
              {busy ? "Saving…" : "Save"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

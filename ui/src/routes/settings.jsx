import * as React from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { Container, KeyRound, LogOut, Monitor, Moon, Palette, Sun, Terminal } from "lucide-react";
import { Page, PageHeader } from "@/components/page";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { InlineError } from "@/components/empty";
import { Field, Segmented } from "@/components/fields";
import { IconTile } from "@/components/kind";
import { authError, useAuth } from "@/lib/auth";
import { useDockerStatus } from "@/lib/docker";
import { useLive } from "@/lib/live";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

const MIN_LEN = 6;

/**
 * SettingsSection is one settings card: what it is on the left, its controls
 * on the right; stacked on small screens.
 */
function SettingsSection({ icon, tone = "brand", title, description, children }) {
  return (
    <Card className="grid grid-cols-1 gap-6 p-6 md:grid-cols-[16rem_minmax(0,1fr)] md:gap-10 sm:p-7">
      <div className="flex items-start gap-4 md:flex-col md:gap-3">
        <IconTile icon={icon} tone={tone} />
        <div className="min-w-0">
          <h2 className="display text-md font-semibold">{title}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{description}</p>
        </div>
      </div>
      <div className="min-w-0">{children}</div>
    </Card>
  );
}

function StatusLine({ ok, label, detail }) {
  return (
    <div className="flex items-start gap-3 py-3 first:pt-0 last:pb-0">
      <span
        className={cn(
          "mt-2 size-2 shrink-0 rounded-full",
          ok ? "bg-success" : "bg-muted-foreground/45",
        )}
        aria-hidden="true"
      />
      <div className="min-w-0">
        <p className="font-medium">{label}</p>
        {detail ? <p className="mt-0.5 break-words text-sm text-muted-foreground">{detail}</p> : null}
      </div>
    </div>
  );
}

/** Settings holds the app-level controls: theme, login password, session, and what the server runs. */
export default function Settings() {
  const { changePassword, logout } = useAuth();
  const { theme, setTheme } = useTheme();
  const { status } = useLive();
  const docker = useDockerStatus();
  const navigate = useNavigate();
  // "restic 0.19.0 compiled with go1.26 on linux/amd64": the number up front, the rest below.
  const resticFull = (status.resticVersion || "").split("\n")[0];
  const resticShort = (resticFull.match(/restic\s+([\d.]+)/) || [])[1] || "";

  const [current, setCurrent] = React.useState("");
  const [next, setNext] = React.useState("");
  const [confirm, setConfirm] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    document.title = "Settings · restic backup manager";
  }, []);

  const submit = async (e) => {
    e.preventDefault();
    setError("");
    if (next.length < MIN_LEN) {
      setError(`Use at least ${MIN_LEN} characters.`);
      return;
    }
    if (next !== confirm) {
      setError("The two new passwords do not match.");
      return;
    }
    setBusy(true);
    const res = await changePassword(current, next);
    setBusy(false);
    if (res.ok) {
      toast.success("Password updated.");
      setCurrent("");
      setNext("");
      setConfirm("");
    } else {
      setError(authError(res, "Could not change the password."));
    }
  };

  const onLogout = async () => {
    await logout();
    navigate("/login", { replace: true });
  };

  return (
    <Page>
      <PageHeader
        title="Settings"
        description="How the app looks, the login password that protects it, and what the server is running."
      />

      <div className="space-y-6">
        <SettingsSection
          icon={Palette}
          tone="violet"
          title="Appearance"
          description="Light, dark, or follow this device. Saved in this browser."
        >
          <Segmented
            label="Theme"
            value={theme}
            onChange={setTheme}
            className="max-w-md"
            options={[
              { value: "light", label: "Light", icon: Sun },
              { value: "dark", label: "Dark", icon: Moon },
              { value: "system", label: "System", icon: Monitor },
            ]}
          />
        </SettingsSection>

        <SettingsSection
          icon={KeyRound}
          title="Login password"
          description="You stay logged in on this browser. Other open sessions are signed out."
        >
          <form onSubmit={submit} className="max-w-md space-y-5">
            <Field label="Current password" htmlFor="settings-current">
              <Input
                id="settings-current"
                type="password"
                autoComplete="current-password"
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
              />
            </Field>
            <Field label="New password" htmlFor="settings-new">
              <Input
                id="settings-new"
                type="password"
                autoComplete="new-password"
                value={next}
                onChange={(e) => setNext(e.target.value)}
                placeholder={`at least ${MIN_LEN} characters`}
              />
            </Field>
            <Field label="Confirm new password" htmlFor="settings-confirm">
              <Input
                id="settings-confirm"
                type="password"
                autoComplete="new-password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
              />
            </Field>
            <InlineError>{error}</InlineError>
            <Button type="submit" disabled={busy || !current || !next || !confirm}>
              {busy ? "Saving…" : "Update password"}
            </Button>
          </form>
        </SettingsSection>

        <SettingsSection
          icon={LogOut}
          tone="neutral"
          title="Session"
          description="Sign out on this browser. You will need the password to return."
        >
          <Button variant="outline" onClick={onLogout}>
            <LogOut />
            Log out
          </Button>
        </SettingsSection>

        <SettingsSection
          icon={Terminal}
          tone="cyan"
          title="This server"
          description="What the app found when it started. Backups need restic; site and database backups need Docker."
        >
          <div className="divide-y divide-border">
            <StatusLine
              ok={status.resticInstalled}
              label={status.resticInstalled ? `restic ${resticShort}`.trim() : "restic not found"}
              detail={
                status.resticInstalled
                  ? resticFull || "Installed and ready to run backups."
                  : "Install the restic binary on the server (see restic.net), then restart the app."
              }
            />
            <StatusLine
              ok={Boolean(docker?.available)}
              label={
                docker == null
                  ? "Checking Docker…"
                  : docker.available
                    ? docker.coolify
                      ? "Docker, managed by Coolify"
                      : "Docker"
                    : "Docker not available"
              }
              detail={
                docker?.available
                  ? "Sites, volumes and databases on this host can be backed up."
                  : docker?.message
              }
            />
          </div>
          {docker?.available ? (
            <Button variant="ghost" size="sm" asChild className="mt-3 -ml-3 text-muted-foreground">
              <a href="#/coolify">
                <Container />
                Open the {docker.coolify ? "Coolify" : "Docker"} page
              </a>
            </Button>
          ) : null}
        </SettingsSection>
      </div>
    </Page>
  );
}

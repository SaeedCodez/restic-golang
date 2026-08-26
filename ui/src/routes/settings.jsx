import * as React from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { LogOut } from "lucide-react";
import { Page, PageHeader } from "@/components/page";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { authError, useAuth } from "@/lib/auth";

const MIN_LEN = 6;

/** Settings holds account-level controls: change the login password, log out. */
export default function Settings() {
  const { changePassword, logout } = useAuth();
  const navigate = useNavigate();

  const [current, setCurrent] = React.useState("");
  const [next, setNext] = React.useState("");
  const [confirm, setConfirm] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

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
        description="Manage the login password that protects this app."
      />

      <div className="space-y-6">
        <Card className="overflow-hidden">
          <div className="border-b border-border px-5 py-4">
            <h2 className="text-[15px] font-semibold tracking-tight">Change password</h2>
            <p className="mt-1 text-[13px] text-muted-foreground">
              You will stay logged in on this browser. Other open sessions are signed out.
            </p>
          </div>
          <CardContent className="pt-5">
            <form onSubmit={submit} className="max-w-sm space-y-4">
              <div className="space-y-2">
                <Label htmlFor="settings-current">Current password</Label>
                <Input
                  id="settings-current"
                  type="password"
                  autoComplete="current-password"
                  value={current}
                  onChange={(e) => setCurrent(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="settings-new">New password</Label>
                <Input
                  id="settings-new"
                  type="password"
                  autoComplete="new-password"
                  value={next}
                  onChange={(e) => setNext(e.target.value)}
                  placeholder={`at least ${MIN_LEN} characters`}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="settings-confirm">Confirm new password</Label>
                <Input
                  id="settings-confirm"
                  type="password"
                  autoComplete="new-password"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                />
              </div>
              {error ? <p className="text-[13px] text-destructive">{error}</p> : null}
              <Button type="submit" disabled={busy || !current || !next || !confirm}>
                {busy ? "Saving…" : "Update password"}
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card className="overflow-hidden">
          <div className="border-b border-border px-5 py-4">
            <h2 className="text-[15px] font-semibold tracking-tight">Session</h2>
            <p className="mt-1 text-[13px] text-muted-foreground">
              Sign out on this browser. You will need the password to return.
            </p>
          </div>
          <CardContent className="pt-5">
            <Button variant="outline" onClick={onLogout}>
              <LogOut />
              Log out
            </Button>
          </CardContent>
        </Card>
      </div>
    </Page>
  );
}

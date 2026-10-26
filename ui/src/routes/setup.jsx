import * as React from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { AuthLayout } from "@/components/auth-layout";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { InlineError } from "@/components/empty";
import { Field } from "@/components/fields";
import { authError, useAuth } from "@/lib/auth";

const MIN_LEN = 6;

/**
 * Setup is the first-open screen: choose the app login password before anything
 * else is reachable.
 */
export default function Setup() {
  const { loading, setupRequired, authenticated, setup } = useAuth();
  const navigate = useNavigate();
  const [password, setPassword] = React.useState("");
  const [confirm, setConfirm] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    document.title = "Set a password · restic backup manager";
  }, []);

  if (loading) {
    return <div className="min-h-dvh bg-background" />;
  }
  if (!setupRequired && authenticated) {
    return <Navigate to="/dashboard" replace />;
  }
  if (!setupRequired && !authenticated) {
    return <Navigate to="/login" replace />;
  }

  const submit = async (e) => {
    e.preventDefault();
    setError("");
    if (password.length < MIN_LEN) {
      setError(`Use at least ${MIN_LEN} characters.`);
      return;
    }
    if (password !== confirm) {
      setError("The two passwords do not match.");
      return;
    }
    setBusy(true);
    const res = await setup(password);
    setBusy(false);
    if (res.ok) {
      navigate("/dashboard", { replace: true });
    } else {
      setError(authError(res, "Could not save the password."));
    }
  };

  return (
    <AuthLayout
      title="Set a login password"
      description="This is the first time the app has been opened. Choose a password to protect access on this machine. You can change it later in Settings."
    >
      <form onSubmit={submit} className="space-y-5">
        <Field label="Password" htmlFor="setup-password">
          <Input
            id="setup-password"
            type="password"
            autoComplete="new-password"
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={`at least ${MIN_LEN} characters`}
          />
        </Field>
        <Field label="Confirm password" htmlFor="setup-confirm">
          <Input
            id="setup-confirm"
            type="password"
            autoComplete="new-password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
          />
        </Field>
        <InlineError>{error}</InlineError>
        <Button type="submit" size="lg" className="w-full" disabled={busy}>
          {busy ? "Saving…" : "Save password and continue"}
        </Button>
      </form>
    </AuthLayout>
  );
}

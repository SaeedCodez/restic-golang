import * as React from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { AuthLayout } from "@/components/auth-layout";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { InlineError } from "@/components/empty";
import { Field } from "@/components/fields";
import { authError, useAuth } from "@/lib/auth";

/** Login unlocks the app after the password was set on first open. */
export default function Login() {
  const { loading, setupRequired, authenticated, login } = useAuth();
  const navigate = useNavigate();
  const [password, setPassword] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

  React.useEffect(() => {
    document.title = "Log in · restic backup manager";
  }, []);

  if (loading) {
    return <div className="min-h-dvh bg-background" />;
  }
  if (setupRequired) {
    return <Navigate to="/setup" replace />;
  }
  if (authenticated) {
    return <Navigate to="/dashboard" replace />;
  }

  const submit = async (e) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    const res = await login(password);
    setBusy(false);
    if (res.ok) {
      navigate("/dashboard", { replace: true });
    } else {
      setError(authError(res, "Incorrect password."));
      setPassword("");
    }
  };

  return (
    <AuthLayout
      title="Log in"
      description="Enter the password you chose when you first opened this app."
      footnote="Scheduled backups keep running while you are logged out."
    >
      <form onSubmit={submit} className="space-y-5">
        <Field label="Password" htmlFor="login-password">
          <Input
            id="login-password"
            type="password"
            autoComplete="current-password"
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        <InlineError>{error}</InlineError>
        <Button type="submit" size="lg" className="w-full" disabled={busy || !password}>
          {busy ? "Checking…" : "Log in"}
        </Button>
      </form>
    </AuthLayout>
  );
}

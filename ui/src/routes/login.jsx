import * as React from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { AuthLayout } from "@/components/auth-layout";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { authError, useAuth } from "@/lib/auth";

/** Login unlocks the app after the password was set on first open. */
export default function Login() {
  const { loading, setupRequired, authenticated, login } = useAuth();
  const navigate = useNavigate();
  const [password, setPassword] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");

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
    >
      <Card>
        <CardContent className="pt-5">
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="login-password">Password</Label>
              <Input
                id="login-password"
                type="password"
                autoComplete="current-password"
                autoFocus
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            {error ? <p className="text-[13px] text-destructive">{error}</p> : null}
            <Button type="submit" className="w-full" disabled={busy || !password}>
              {busy ? "Checking…" : "Log in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </AuthLayout>
  );
}

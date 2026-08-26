import * as React from "react";
import { Auth, errorOf } from "@/lib/api";

const AuthContext = React.createContext({
  loading: true,
  setupRequired: false,
  authenticated: false,
  refresh: async () => {},
  setup: async () => ({ ok: false }),
  login: async () => ({ ok: false }),
  logout: async () => {},
  changePassword: async () => ({ ok: false }),
});

/**
 * AuthProvider loads /api/auth/status once and exposes setup/login/logout so
 * the router can send the user to the right screen on first open.
 */
export function AuthProvider({ children }) {
  const [loading, setLoading] = React.useState(true);
  const [setupRequired, setSetupRequired] = React.useState(false);
  const [authenticated, setAuthenticated] = React.useState(false);

  const apply = React.useCallback((body) => {
    setSetupRequired(!!body?.setupRequired);
    setAuthenticated(!!body?.authenticated);
  }, []);

  const refresh = React.useCallback(async () => {
    const res = await Auth.status();
    if (res.ok) {
      apply(res.body);
    } else {
      // If the status call itself fails, treat as unauthenticated so the UI
      // still has somewhere to go (login/setup) rather than hanging.
      setSetupRequired(false);
      setAuthenticated(false);
    }
    setLoading(false);
  }, [apply]);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  const setup = React.useCallback(
    async (password) => {
      const res = await Auth.setup(password);
      if (res.ok) {
        setSetupRequired(false);
        setAuthenticated(true);
      }
      return res;
    },
    [],
  );

  const login = React.useCallback(async (password) => {
    const res = await Auth.login(password);
    if (res.ok) {
      setAuthenticated(true);
      setSetupRequired(false);
    }
    return res;
  }, []);

  const logout = React.useCallback(async () => {
    await Auth.logout();
    setAuthenticated(false);
  }, []);

  const changePassword = React.useCallback(async (currentPassword, newPassword) => {
    return Auth.changePassword(currentPassword, newPassword);
  }, []);

  const value = React.useMemo(
    () => ({
      loading,
      setupRequired,
      authenticated,
      refresh,
      setup,
      login,
      logout,
      changePassword,
    }),
    [loading, setupRequired, authenticated, refresh, setup, login, logout, changePassword],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  return React.useContext(AuthContext);
}

/** authError is a short message for setup/login/password forms. */
export function authError(res, fallback) {
  return errorOf(res, fallback);
}

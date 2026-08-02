import * as React from "react";

/**
 * useFetch runs an async loader and tracks {data, loading, error}, with a
 * `reload` for explicit refreshes and a stale-response guard so a slow request
 * can never overwrite a newer one.
 *
 * `loader` must be stable (wrap it in useCallback) — it is the dependency.
 */
export function useFetch(loader, { initial = null } = {}) {
  const [data, setData] = React.useState(initial);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState(null);
  const [nonce, setNonce] = React.useState(0);
  const generation = React.useRef(0);

  React.useEffect(() => {
    const mine = ++generation.current;
    let cancelled = false;
    setLoading(true);
    Promise.resolve(loader())
      .then((value) => {
        if (cancelled || mine !== generation.current) return;
        setData(value);
        setError(null);
      })
      .catch((err) => {
        if (cancelled || mine !== generation.current) return;
        setError(err);
      })
      .finally(() => {
        if (cancelled || mine !== generation.current) return;
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [loader, nonce]);

  const reload = React.useCallback(() => setNonce((n) => n + 1), []);
  return { data, loading, error, reload, setData };
}

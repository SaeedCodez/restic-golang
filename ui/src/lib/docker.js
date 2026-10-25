import * as React from "react";
import { Docker } from "@/lib/api";

// Whether this app can reach Docker does not change while the page is open, so
// the status is fetched once and shared by every component that asks.
let cached = null;
let pending = null;

function fetchStatus() {
  if (!pending) {
    pending = Docker.status().then((res) => {
      cached = res.body?.docker || { available: false, message: "Could not ask the server." };
      return cached;
    });
  }
  return pending;
}

/** useDockerStatus returns { available, coolify, message } once known, else null. */
export function useDockerStatus() {
  const [status, setStatus] = React.useState(cached);
  React.useEffect(() => {
    if (cached) return undefined;
    let alive = true;
    fetchStatus().then((s) => alive && setStatus(s));
    return () => {
      alive = false;
    };
  }, []);
  return status;
}

/**
 * useContainersUsing lists the running containers that use a local path (what
 * a restore with "stop the site" would stop). Empty while unknown, without
 * Docker, or when nothing uses the path.
 */
export function useContainersUsing(path) {
  const docker = useDockerStatus();
  const [containers, setContainers] = React.useState([]);
  const available = Boolean(docker?.available);
  React.useEffect(() => {
    setContainers([]);
    if (!available || !path || !path.startsWith("/")) return undefined;
    let alive = true;
    Docker.containersUsing(path).then((res) => {
      if (alive && res.ok) setContainers(res.body.containers || []);
    });
    return () => {
      alive = false;
    };
  }, [available, path]);
  return containers;
}

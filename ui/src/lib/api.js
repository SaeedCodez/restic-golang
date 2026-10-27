/**
 * Thin JSON client for the app's API. Every call resolves to
 * `{ status, ok, body }` — errors are values, never exceptions — because the
 * server returns a structured `{ ok, code, error }` body for failures and the
 * UI reacts to `code` (e.g. "busy", "not_initialized", "no_restic").
 */
async function req(method, url, body) {
  const opt = { method, headers: {}, credentials: "same-origin" };
  if (body !== undefined) {
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  let r;
  try {
    r = await fetch(url, opt);
  } catch (err) {
    return {
      status: 0,
      ok: false,
      body: { ok: false, code: "network", error: "Could not reach the app: " + err.message },
    };
  }
  const ct = r.headers.get("content-type") || "";
  let parsed;
  if (ct.includes("application/json")) {
    parsed = await r.json().catch(() => ({}));
  } else {
    parsed = { error: (await r.text().catch(() => "")) || r.statusText };
  }
  return { status: r.status, ok: r.ok, body: parsed };
}

export const api = {
  get: (u) => req("GET", u),
  post: (u, b) => req("POST", u, b === undefined ? {} : b),
  put: (u, b) => req("PUT", u, b),
  del: (u) => req("DELETE", u),
};

/** errorOf pulls the most useful message out of a failed response. */
export function errorOf(res, fallback = "Something went wrong.") {
  return (res && res.body && (res.body.error || res.body.message)) || fallback;
}

// ---- endpoint helpers ------------------------------------------------------

export const Status = {
  get: () => api.get("/api/status"),
};

export const Auth = {
  status: () => api.get("/api/auth/status"),
  setup: (password) => api.post("/api/auth/setup", { password }),
  login: (password) => api.post("/api/auth/login", { password }),
  logout: () => api.post("/api/auth/logout"),
  changePassword: (currentPassword, newPassword) =>
    api.post("/api/auth/password", { currentPassword, newPassword }),
};

export const Repositories = {
  list: () => api.get("/api/repositories"),
  get: (id) => api.get(`/api/repositories/${id}`),
  create: (b) => api.post("/api/repositories", b),
  update: (id, b) => api.put(`/api/repositories/${id}`, b),
  remove: (id) => api.del(`/api/repositories/${id}`),
  test: (id) => api.post(`/api/repositories/${id}/test`),
  init: (id) => api.post(`/api/repositories/${id}/init`),
  unlock: (id) => api.post(`/api/repositories/${id}/unlock`),
  snapshots: (id) => api.get(`/api/repositories/${id}/snapshots`),
  /** include limits the restore to those snapshot paths; stopContainers stops
   * the site using the destination while it runs. */
  restore: (id, snapshotId, target, { include, stopContainers } = {}) =>
    api.post(`/api/repositories/${id}/restore`, { snapshotId, target, include, stopContainers }),
  download: (id, snapshotId, include) =>
    api.post(`/api/repositories/${id}/download`, { snapshotId, include }),
  /** body: { snapshotId, databaseId, into?, safetyBackup? } */
  restoreDb: (id, body) => api.post(`/api/repositories/${id}/restore-db`, body),
  /** One folder of a snapshot, for the file browser. */
  files: (id, snapshotId, path = "/") =>
    api.get(
      `/api/repositories/${id}/snapshots/${encodeURIComponent(snapshotId)}/ls?path=${encodeURIComponent(path)}`,
    ),
  forget: (id, snapshotId) => api.post(`/api/repositories/${id}/forget`, { snapshotId }),
  reset: (id) => api.post(`/api/repositories/${id}/reset`),
};

export const Folders = {
  list: () => api.get("/api/folders"),
  create: (b) => api.post("/api/folders", b),
  update: (id, b) => api.put(`/api/folders/${id}`, b),
  remove: (id) => api.del(`/api/folders/${id}`),
};

export const Databases = {
  list: () => api.get("/api/databases"),
  create: (b) => api.post("/api/databases", b),
  update: (id, b) => api.put(`/api/databases/${id}`, b),
  remove: (id) => api.del(`/api/databases/${id}`),
};

/** Docker / Coolify discovery: what runs on this host, and quick job creation. */
export const Docker = {
  status: () => api.get("/api/docker/status"),
  resources: () => api.get("/api/docker/resources"),
  databases: (container) =>
    api.get(`/api/docker/containers/${encodeURIComponent(container)}/databases`),
  containersUsing: (path) =>
    api.get(`/api/docker/containers-using?path=${encodeURIComponent(path)}`),
  /** body: { kind: "folder"|"database", path | container+database, name, repositoryId, schedule? } */
  createJob: (b) => api.post("/api/docker/jobs", b),
};

export const Jobs = {
  list: () => api.get("/api/jobs"),
  get: (id) => api.get(`/api/jobs/${id}`),
  create: (b) => api.post("/api/jobs", b),
  /**
   * save stores `job` with `changes` applied. PUT replaces the whole job, so
   * every field it keeps — its source, folder or database, among them — is
   * sent from `job` as the API returned it.
   */
  save: (job, changes) =>
    api.put(`/api/jobs/${job.id}`, {
      name: job.name,
      folderId: job.folderId,
      databaseId: job.databaseId,
      repositoryId: job.repositoryId,
      schedule: job.schedule || null,
      retention: job.retention || null,
      ...changes,
    }),
  remove: (id) => api.del(`/api/jobs/${id}`),
  run: (id) => api.post(`/api/jobs/${id}/run`),
  /** dryRun previews what the policy keeps and removes, changing nothing. */
  retention: (id, { dryRun } = {}) => api.post(`/api/jobs/${id}/retention`, { dryRun: !!dryRun }),
  forget: (id, { deleteJob } = {}) => api.post(`/api/jobs/${id}/forget`, { deleteJob: !!deleteJob }),
  runs: (id, params = {}) => {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== null && v !== "") q.set(k, v);
    }
    const qs = q.toString();
    return api.get(`/api/jobs/${id}/runs` + (qs ? "?" + qs : ""));
  },
  snapshots: (id) => api.get(`/api/jobs/${id}/snapshots`),
};

export const Runs = {
  get: (id) => api.get(`/api/runs/${id}`),
  stop: (id) => api.post(`/api/runs/${id}/stop`),
  log: (id, after = 0) => api.get(`/api/runs/${id}/log?after=${after}`),
  list: (params = {}) => {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== null && v !== "") q.set(k, v);
    }
    const qs = q.toString();
    return api.get("/api/runs" + (qs ? "?" + qs : ""));
  },
  downloadURL: (id) => `/api/runs/${id}/download`,
};

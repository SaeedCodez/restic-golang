/* ------------------------------------------------------------------ *
 * restic backup manager — front-end (vanilla JS).
 *
 * Hash-routed SPA over the JSON API. Everything long-running is a "run"
 * watched through one shared view backed by Server-Sent Events, so live
 * progress and the full log survive refresh, second tabs and restarts.
 * ------------------------------------------------------------------ */
"use strict";

// ---- tiny helpers ----------------------------------------------------------

const $ = (id) => document.getElementById(id);

function esc(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function fmtBytes(n) {
  n = Number(n) || 0;
  if (n < 1024) return n + " B";
  const u = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(n < 10 ? 2 : 1) + " " + u[i];
}

function fmtDur(sec) {
  sec = Number(sec) || 0;
  if (sec < 60) return sec.toFixed(1) + "s";
  const m = Math.floor(sec / 60), s = Math.round(sec % 60);
  if (m < 60) return m + "m " + s + "s";
  return Math.floor(m / 60) + "h " + (m % 60) + "m";
}

function fmtTime(iso) {
  if (!iso) return "—";
  const d = new Date(iso);
  return isNaN(d.getTime()) ? iso : d.toLocaleString();
}

function runDuration(run) {
  if (!run.startedAt) return "—";
  const start = new Date(run.startedAt).getTime();
  const end = run.finishedAt ? new Date(run.finishedAt).getTime() : Date.now();
  return fmtDur(Math.max(0, (end - start) / 1000));
}

function shortId(id) { return id ? String(id).slice(0, 8) : ""; }

function statusBadge(status) {
  const label = {
    starting: "starting", running: "running", success: "success",
    success_warnings: "warnings", failed: "failed", canceled: "canceled",
    interrupted: "interrupted",
  }[status] || status;
  return `<span class="badge st-${esc(status)}">${esc(label)}</span>`;
}

function isActive(status) { return status === "running" || status === "starting"; }

async function req(method, url, body) {
  const opt = { method };
  if (body !== undefined) {
    opt.headers = { "Content-Type": "application/json" };
    opt.body = JSON.stringify(body);
  }
  const r = await fetch(url, opt);
  const ct = r.headers.get("content-type") || "";
  const b = ct.includes("application/json") ? await r.json() : { error: await r.text() };
  return { status: r.status, ok: r.ok, body: b };
}

const get = (u) => req("GET", u);
const post = (u, b) => req("POST", u, b || {});
const put = (u, b) => req("PUT", u, b);
const del = (u) => req("DELETE", u);

// ---- shared state ----------------------------------------------------------

const app = {
  resticInstalled: false,
  activeRuns: new Map(), // runId -> status
};

// ---- boot & status ---------------------------------------------------------

document.addEventListener("DOMContentLoaded", async () => {
  window.addEventListener("hashchange", router);
  await loadStatus();
  openGlobalStream();
  router();
});

async function loadStatus() {
  const r = await get("/api/status");
  if (!r.ok) return;
  const s = r.body;
  app.resticInstalled = !!s.resticInstalled;

  const pill = $("resticPill");
  if (app.resticInstalled) {
    pill.className = "pill pill-good";
    pill.textContent = s.resticVersion ? s.resticVersion.split("\n")[0] : "restic ready";
  } else {
    pill.className = "pill pill-bad";
    pill.textContent = "restic not found";
  }

  const banner = $("globalBanner");
  if (!app.resticInstalled) {
    banner.innerHTML =
      "<strong>restic is not installed.</strong> This app runs the <code>restic</code> binary. " +
      "Install it (e.g. <code>brew install restic</code> or see " +
      '<a href="https://restic.net" target="_blank" rel="noopener">restic.net</a>) and restart.';
    banner.classList.remove("hidden");
  } else {
    banner.classList.add("hidden");
  }

  app.activeRuns.clear();
  (s.activeRuns || []).forEach((run) => app.activeRuns.set(run.id, run.status));
  updateActivePill();
}

function updateActivePill() {
  const pill = $("activePill");
  const n = app.activeRuns.size;
  if (n > 0) {
    pill.className = "pill pill-busy";
    pill.textContent = n + " running";
  } else {
    pill.className = "pill pill-idle";
    pill.textContent = "idle";
  }
}

// ---- global live stream (lists + badges) -----------------------------------

let globalStream = null;
function openGlobalStream() {
  if (globalStream) globalStream.close();
  globalStream = new EventSource("/api/events");
  globalStream.onmessage = (e) => {
    let ev;
    try { ev = JSON.parse(e.data); } catch (_) { return; }
    if (ev.type !== "run" || !ev.run) return;
    const run = ev.run;
    if (isActive(run.status)) app.activeRuns.set(run.id, run.status);
    else app.activeRuns.delete(run.id);
    updateActivePill();
    // Refresh the current list view so run state stays fresh.
    const sec = currentSection();
    if (sec === "activity") renderActivity();
    else if (sec === "jobs" && !currentId()) { /* list view: leave as-is */ }
  };
}

// ---- router ----------------------------------------------------------------

function parseHash() {
  const raw = location.hash.replace(/^#/, "") || "/jobs";
  return raw.split("/").filter((x) => x !== "");
}
function currentSection() { return parseHash()[0] || "jobs"; }
function currentId() { return parseHash()[1] || ""; }

function setActiveTab(section) {
  document.querySelectorAll(".tab").forEach((t) =>
    t.classList.toggle("active", t.dataset.section === section));
}

function router() {
  closeRunStream();
  const parts = parseHash();
  const section = parts[0] || "jobs";
  setActiveTab(section);

  switch (section) {
    case "jobs":
      if (parts[1]) renderJobDetail(parts[1]);
      else renderJobs();
      break;
    case "repositories":
      if (parts[1]) renderRepoDetail(parts[1]);
      else renderRepositories();
      break;
    case "folders":
      renderFolders();
      break;
    case "activity":
      renderActivity();
      break;
    case "runs":
      if (parts[1]) renderRun(parts[1]);
      else location.hash = "#/activity";
      break;
    default:
      location.hash = "#/jobs";
  }
}

function view(html) { $("view").innerHTML = html; }
function notFoundCard(what) {
  return `<div class="card"><p class="empty">${esc(what)} not found. <a href="#/jobs">Go back</a></p></div>`;
}

// ---- JOBS ------------------------------------------------------------------

async function renderJobs() {
  const [jobsR, foldersR, reposR] = await Promise.all([
    get("/api/jobs"), get("/api/folders"), get("/api/repositories"),
  ]);
  const jobs = (jobsR.body && jobsR.body.jobs) || [];
  const folders = (foldersR.body && foldersR.body.folders) || [];
  const repos = (reposR.body && reposR.body.repositories) || [];

  let html = `<div class="card"><div class="card-head"><h2>Backup jobs</h2></div>
    <p class="hint">A job is a saved pairing of a backup folder and a storage repository. Run it, and its history lives here.</p>`;

  if (!folders.length || !repos.length) {
    html += `<p class="muted">First create at least one
      <a href="#/folders">folder</a> and one <a href="#/repositories">repository</a>, then define a job.</p>`;
  } else {
    html += `<div class="grid2">
      <div class="field"><label>Job name</label><input id="jobName" placeholder="e.g. Documents → S3"/></div>
      <div class="field"><label>Backup folder</label><select id="jobFolder">${
        folders.map((f) => `<option value="${esc(f.id)}">${esc(f.name)} (${esc(f.path)})</option>`).join("")
      }</select></div>
      <div class="field"><label>Repository</label><select id="jobRepo">${
        repos.map((r) => `<option value="${esc(r.id)}">${esc(r.name)}</option>`).join("")
      }</select></div>
    </div>
    <div class="actions"><button id="createJob" class="btn btn-primary">Create job</button></div>
    <div id="jobFormResult"></div>`;
  }
  html += `</div>`;

  html += `<div class="card"><h3>Your jobs</h3><div class="table-wrap"><table>
    <thead><tr><th>Name</th><th>Folder</th><th>Repository</th><th class="right">Actions</th></tr></thead>
    <tbody>`;
  if (!jobs.length) {
    html += `<tr><td colspan="4" class="empty">No jobs yet.</td></tr>`;
  } else {
    html += jobs.map((j) => `<tr>
      <td><a href="#/jobs/${esc(j.id)}">${esc(j.name)}</a></td>
      <td class="path">${esc(j.folderPath || j.folderName || "—")}</td>
      <td>${esc(j.repoName || "—")}</td>
      <td class="right"><div class="row-actions">
        <button class="btn btn-small run-job" data-id="${esc(j.id)}">Run backup</button>
        <a class="btn btn-small" href="#/jobs/${esc(j.id)}">Open</a>
      </div></td></tr>`).join("");
  }
  html += `</tbody></table></div></div>`;
  view(html);

  if ($("createJob")) $("createJob").onclick = createJob;
  document.querySelectorAll(".run-job").forEach((b) => (b.onclick = () => runJob(b.dataset.id)));
}

async function createJob() {
  const body = {
    name: $("jobName").value.trim(),
    folderId: $("jobFolder").value,
    repositoryId: $("jobRepo").value,
  };
  const r = await post("/api/jobs", body);
  if (r.ok) renderJobs();
  else setInlineResult("jobFormResult", "bad", (r.body && r.body.error) || "Could not create job.");
}

async function runJob(id) {
  const r = await post(`/api/jobs/${id}/run`);
  if (r.status === 202 && r.body.runId) { location.hash = "#/runs/" + r.body.runId; return; }
  alert((r.body && r.body.error) || "Could not start backup.");
}

async function renderJobDetail(id) {
  const r = await get(`/api/jobs/${id}`);
  if (r.status === 404) { view(notFoundCard("Job")); return; }
  const job = r.body.job;

  view(`
    <div class="crumbs"><a href="#/jobs">Jobs</a> / ${esc(job.name)}</div>
    <div class="card"><div class="card-head">
      <div><h2>${esc(job.name)}</h2>
        <p class="hint">Folder <span class="path">${esc(job.folderPath || "?")}</span> → repository <strong>${esc(job.repoName || "?")}</strong></p>
      </div>
      <div class="row-actions">
        <button id="runJobBtn" class="btn btn-primary">Run backup</button>
        <button id="delJobBtn" class="btn btn-small btn-danger">Delete job</button>
      </div>
    </div>
    <p class="muted mono" style="font-size:12px">tag: ${esc(job.tag || "")}</p>
    <div id="jobRunResult"></div></div>

    <div class="card"><div class="card-head"><h3>Run history</h3>
      <button id="refreshRuns" class="btn btn-small">Refresh</button></div>
      <div class="table-wrap"><table>
      <thead><tr><th>Kind</th><th>Status</th><th>Started</th><th>Duration</th><th>Stored</th></tr></thead>
      <tbody id="runsBody"><tr><td colspan="5" class="empty">Loading…</td></tr></tbody>
      </table></div>
    </div>

    <div class="card"><div class="card-head"><h3>Snapshots</h3>
      <button id="refreshSnaps" class="btn btn-small">Refresh</button></div>
      <div class="field"><label>Restore / download target folder (absolute path)</label>
        <input id="restoreTarget" placeholder="/home/you/restore-here"/></div>
      <div id="snapsResult"></div>
      <div class="table-wrap"><table>
        <thead><tr><th>ID</th><th>Time</th><th>Size</th><th>Files</th><th class="right">Actions</th></tr></thead>
        <tbody id="snapsBody"><tr><td colspan="5" class="empty">Loading…</td></tr></tbody>
      </table></div>
    </div>`);

  $("runJobBtn").onclick = () => runJob(id);
  $("delJobBtn").onclick = async () => {
    if (!confirm(`Delete job "${job.name}"? Its run history is kept.`)) return;
    const d = await del(`/api/jobs/${id}`);
    if (d.ok) location.hash = "#/jobs";
    else alert((d.body && d.body.error) || "Could not delete job.");
  };
  $("refreshRuns").onclick = () => loadJobRuns(id);
  $("refreshSnaps").onclick = () => loadJobSnapshots(job);
  loadJobRuns(id);
  loadJobSnapshots(job);
}

async function loadJobRuns(id) {
  const r = await get(`/api/jobs/${id}/runs`);
  const runs = (r.body && r.body.runs) || [];
  const body = $("runsBody");
  if (!body) return;
  if (!runs.length) { body.innerHTML = `<tr><td colspan="5" class="empty">No runs yet.</td></tr>`; return; }
  body.innerHTML = runs.map((run) => {
    const stored = run.summary
      ? (run.summary.snapshotId ? "snapshot " + shortId(run.summary.snapshotId) + " · " : "") +
        (run.summary.dataAdded ? fmtBytes(run.summary.dataAdded) + " added" : "")
      : "";
    return `<tr class="clickable" data-run="${esc(run.id)}">
      <td>${esc(run.kind)}</td><td>${statusBadge(run.status)}</td>
      <td>${esc(fmtTime(run.startedAt))}</td><td>${esc(runDuration(run))}</td>
      <td class="mono-cell">${esc(stored)}</td></tr>`;
  }).join("");
  body.querySelectorAll("tr.clickable").forEach((tr) =>
    (tr.onclick = () => (location.hash = "#/runs/" + tr.dataset.run)));
}

async function loadJobSnapshots(job) {
  const r = await get(`/api/jobs/${job.id}/snapshots`);
  renderSnapshotsInto(r.body, job.repositoryId);
}

// Shared snapshot table renderer (used by job detail and repo detail).
function renderSnapshotsInto(payload, repoId) {
  const body = $("snapsBody");
  const result = $("snapsResult");
  if (!body) return;
  if (result) result.innerHTML = "";
  if (!payload || !payload.ok) {
    body.innerHTML = `<tr><td colspan="5" class="empty">—</td></tr>`;
    if (result) {
      const code = payload && payload.code;
      const kind = code === "not_initialized" ? "info" : "bad";
      setInlineResult("snapsResult", kind, (payload && payload.error) || "Could not list snapshots.");
    }
    return;
  }
  const snaps = (payload.snapshots || []).slice().sort((a, b) => new Date(b.time) - new Date(a.time));
  if (!snaps.length) { body.innerHTML = `<tr><td colspan="5" class="empty">No snapshots yet.</td></tr>`; return; }
  body.innerHTML = snaps.map((s) => `<tr>
    <td class="mono-cell">${esc(s.shortId || shortId(s.id))}</td>
    <td>${esc(fmtTime(s.time))}</td>
    <td>${s.sizeBytes ? esc(fmtBytes(s.sizeBytes)) : "—"}</td>
    <td>${s.fileCount ? esc(s.fileCount.toLocaleString()) : "—"}</td>
    <td class="right"><div class="row-actions">
      <button class="btn btn-small snap-restore" data-id="${esc(s.id)}">Restore</button>
      <button class="btn btn-small snap-download" data-id="${esc(s.id)}">Download</button>
    </div></td></tr>`).join("");
  body.querySelectorAll(".snap-restore").forEach((b) =>
    (b.onclick = () => restoreSnapshot(repoId, b.dataset.id)));
  body.querySelectorAll(".snap-download").forEach((b) =>
    (b.onclick = () => downloadSnapshot(repoId, b.dataset.id)));
}

async function restoreSnapshot(repoId, snapshotId) {
  const target = ($("restoreTarget") && $("restoreTarget").value.trim()) || "";
  if (!target) { alert("Enter a target folder to restore into."); return; }
  const r = await post(`/api/repositories/${repoId}/restore`, { snapshotId, target });
  if (r.status === 202 && r.body.runId) location.hash = "#/runs/" + r.body.runId;
  else alert((r.body && r.body.error) || "Could not start restore.");
}

async function downloadSnapshot(repoId, snapshotId) {
  const r = await post(`/api/repositories/${repoId}/download`, { snapshotId });
  if (r.status === 202 && r.body.runId) location.hash = "#/runs/" + r.body.runId;
  else alert((r.body && r.body.error) || "Could not start download.");
}

// ---- REPOSITORIES ----------------------------------------------------------

async function renderRepositories() {
  const r = await get("/api/repositories");
  const repos = (r.body && r.body.repositories) || [];
  view(`
    <div class="card"><h2>Storage repositories</h2>
      <p class="hint">Where restic stores encrypted backups. Use a local directory to try it with no credentials.</p>
      <div id="repoForm"></div>
    </div>
    <div class="card"><h3>Your repositories</h3><div class="table-wrap"><table>
      <thead><tr><th>Name</th><th>Backend</th><th>Location</th><th class="right">Actions</th></tr></thead>
      <tbody>${
        repos.length ? repos.map((repo) => `<tr>
          <td><a href="#/repositories/${esc(repo.id)}">${esc(repo.name)}</a></td>
          <td>${esc(repo.backendType)}</td>
          <td class="path">${esc(repo.backendType === "S3" ? (repo.endpoint || "") + "/" + (repo.bucket || "") : (repo.localPath || ""))}</td>
          <td class="right"><div class="row-actions">
            <button class="btn btn-small repo-edit" data-id="${esc(repo.id)}">Edit</button>
            <button class="btn btn-small btn-danger repo-del" data-id="${esc(repo.id)}">Delete</button>
          </div></td></tr>`).join("")
        : `<tr><td colspan="4" class="empty">No repositories yet.</td></tr>`
      }</tbody></table></div></div>`);

  renderRepoForm(null);
  document.querySelectorAll(".repo-edit").forEach((b) =>
    (b.onclick = () => renderRepoForm(repos.find((x) => x.id === b.dataset.id))));
  document.querySelectorAll(".repo-del").forEach((b) => (b.onclick = () => deleteRepo(b.dataset.id)));
}

function renderRepoForm(repo) {
  const r = repo || { backendType: "Local" };
  const isS3 = r.backendType === "S3";
  $("repoForm").innerHTML = `
    <div class="field"><label>Name</label><input id="rName" value="${esc(r.name || "")}"/></div>
    <div class="field"><label>Backend</label>
      <select id="rBackend"><option value="Local"${isS3 ? "" : " selected"}>Local directory</option>
      <option value="S3"${isS3 ? " selected" : ""}>S3-compatible</option></select></div>
    <div id="rLocal" class="${isS3 ? "hidden" : ""}">
      <div class="field"><label>Repository directory</label><input id="rLocalPath" value="${esc(r.localPath || "")}" placeholder="/home/you/restic-repo"/></div>
    </div>
    <div id="rS3" class="${isS3 ? "" : "hidden"}"><div class="grid2">
      <div class="field"><label>Endpoint</label><input id="rEndpoint" value="${esc(r.endpoint || "")}" placeholder="https://s3.amazonaws.com"/></div>
      <div class="field"><label>Bucket</label><input id="rBucket" value="${esc(r.bucket || "")}"/></div>
      <div class="field"><label>Region (optional)</label><input id="rRegion" value="${esc(r.region || "")}"/></div>
      <div class="field"><label>Access key</label><input id="rAccess" value="${esc(r.accessKey || "")}" autocomplete="off"/></div>
      <div class="field"><label>Secret key</label><input id="rSecret" type="password" value="${esc(r.secretKey || "")}" autocomplete="off"/></div>
    </div></div>
    <div class="field"><label>Repository password</label><input id="rPassword" type="password" value="${esc(r.password || "")}" autocomplete="off" placeholder="encrypts your backups — don't lose it"/></div>
    <div class="actions">
      <button id="rSave" class="btn btn-primary">${repo ? "Save changes" : "Add repository"}</button>
      ${repo ? `<button id="rCancel" class="btn">Cancel</button>` : ""}
    </div>
    <div id="repoResult"></div>`;

  $("rBackend").onchange = () => {
    const s3 = $("rBackend").value === "S3";
    $("rLocal").classList.toggle("hidden", s3);
    $("rS3").classList.toggle("hidden", !s3);
  };
  $("rSave").onclick = () => saveRepo(repo && repo.id);
  if ($("rCancel")) $("rCancel").onclick = () => renderRepoForm(null);
}

function gatherRepo() {
  return {
    name: $("rName").value.trim(),
    backendType: $("rBackend").value,
    localPath: $("rLocalPath").value.trim(),
    endpoint: $("rEndpoint").value.trim(),
    bucket: $("rBucket").value.trim(),
    region: $("rRegion").value.trim(),
    accessKey: $("rAccess").value.trim(),
    secretKey: $("rSecret").value,
    password: $("rPassword").value,
  };
}

async function saveRepo(id) {
  const body = gatherRepo();
  const r = id ? await put(`/api/repositories/${id}`, body) : await post("/api/repositories", body);
  if (r.ok) renderRepositories();
  else setInlineResult("repoResult", "bad", (r.body && r.body.error) || "Could not save repository.");
}

async function deleteRepo(id) {
  if (!confirm("Delete this repository?")) return;
  const r = await del(`/api/repositories/${id}`);
  if (r.ok) renderRepositories();
  else alert((r.body && r.body.error) || "Could not delete repository.");
}

async function renderRepoDetail(id) {
  const r = await get(`/api/repositories/${id}`);
  if (r.status === 404) { view(notFoundCard("Repository")); return; }
  const repo = r.body.repository;
  view(`
    <div class="crumbs"><a href="#/repositories">Repositories</a> / ${esc(repo.name)}</div>
    <div class="card"><div class="card-head">
      <div><h2>${esc(repo.name)}</h2><p class="hint">${esc(repo.backendType)} backend</p></div>
      <div class="row-actions">
        <button id="rTest" class="btn btn-small">Test connection</button>
        <button id="rInit" class="btn btn-small">Initialize</button>
        <button id="rUnlock" class="btn btn-small">Unlock</button>
      </div></div>
      <div id="repoOpResult"></div>
    </div>
    <div class="card"><div class="card-head"><h3>Snapshots</h3>
      <button id="refreshSnaps" class="btn btn-small">Refresh</button></div>
      <div class="field"><label>Restore / download target folder (absolute path)</label>
        <input id="restoreTarget" placeholder="/home/you/restore-here"/></div>
      <div id="snapsResult"></div>
      <div class="table-wrap"><table>
        <thead><tr><th>ID</th><th>Time</th><th>Size</th><th>Files</th><th class="right">Actions</th></tr></thead>
        <tbody id="snapsBody"><tr><td colspan="5" class="empty">Loading…</td></tr></tbody>
      </table></div>
    </div>`);

  $("rTest").onclick = async () => {
    setInlineResult("repoOpResult", "info", "Testing…");
    const t = await post(`/api/repositories/${id}/test`);
    const b = t.body || {};
    setInlineResult("repoOpResult", b.ok ? "good" : "bad", b.message || b.error || "Failed.", b.detail);
  };
  $("rInit").onclick = async () => {
    const t = await post(`/api/repositories/${id}/init`);
    if (t.status === 202 && t.body.runId) location.hash = "#/runs/" + t.body.runId;
    else setInlineResult("repoOpResult", "bad", (t.body && t.body.error) || "Could not initialize.");
  };
  $("rUnlock").onclick = async () => {
    const t = await post(`/api/repositories/${id}/unlock`);
    setInlineResult("repoOpResult", t.body && t.body.ok ? "good" : "bad",
      (t.body && (t.body.message || t.body.error)) || "Failed.");
  };
  const loadSnaps = async () => renderSnapshotsInto((await get(`/api/repositories/${id}/snapshots`)).body, id);
  $("refreshSnaps").onclick = loadSnaps;
  loadSnaps();
}

// ---- FOLDERS ---------------------------------------------------------------

async function renderFolders() {
  const r = await get("/api/folders");
  const folders = (r.body && r.body.folders) || [];
  view(`
    <div class="card"><h2>Backup folders</h2>
      <p class="hint">Named folders you back up, so a path becomes a reusable thing rather than retyped each time.</p>
      <div id="folderForm"></div>
    </div>
    <div class="card"><h3>Your folders</h3><div class="table-wrap"><table>
      <thead><tr><th>Name</th><th>Path</th><th class="right">Actions</th></tr></thead>
      <tbody>${
        folders.length ? folders.map((f) => `<tr>
          <td>${esc(f.name)}</td><td class="path">${esc(f.path)}</td>
          <td class="right"><div class="row-actions">
            <button class="btn btn-small folder-edit" data-id="${esc(f.id)}">Edit</button>
            <button class="btn btn-small btn-danger folder-del" data-id="${esc(f.id)}">Delete</button>
          </div></td></tr>`).join("")
        : `<tr><td colspan="3" class="empty">No folders yet.</td></tr>`
      }</tbody></table></div></div>`);

  renderFolderForm(null);
  document.querySelectorAll(".folder-edit").forEach((b) =>
    (b.onclick = () => renderFolderForm(folders.find((x) => x.id === b.dataset.id))));
  document.querySelectorAll(".folder-del").forEach((b) => (b.onclick = () => deleteFolder(b.dataset.id)));
}

function renderFolderForm(folder) {
  const f = folder || {};
  $("folderForm").innerHTML = `
    <div class="grid2">
      <div class="field"><label>Name</label><input id="fName" value="${esc(f.name || "")}"/></div>
      <div class="field"><label>Absolute path</label><input id="fPath" value="${esc(f.path || "")}" placeholder="/home/you/Documents"/></div>
    </div>
    <div class="actions">
      <button id="fSave" class="btn btn-primary">${folder ? "Save changes" : "Add folder"}</button>
      ${folder ? `<button id="fCancel" class="btn">Cancel</button>` : ""}
    </div><div id="folderResult"></div>`;
  $("fSave").onclick = () => saveFolder(folder && folder.id);
  if ($("fCancel")) $("fCancel").onclick = () => renderFolderForm(null);
}

async function saveFolder(id) {
  const body = { name: $("fName").value.trim(), path: $("fPath").value.trim() };
  const r = id ? await put(`/api/folders/${id}`, body) : await post("/api/folders", body);
  if (r.ok) renderFolders();
  else setInlineResult("folderResult", "bad", (r.body && r.body.error) || "Could not save folder.");
}

async function deleteFolder(id) {
  if (!confirm("Delete this folder?")) return;
  const r = await del(`/api/folders/${id}`);
  if (r.ok) renderFolders();
  else alert((r.body && r.body.error) || "Could not delete folder.");
}

// ---- ACTIVITY --------------------------------------------------------------

async function renderActivity() {
  const r = await get("/api/runs?status=active");
  const runs = (r.body && r.body.runs) || [];
  view(`<div class="card"><div class="card-head"><h2>Activity</h2>
    <button id="refreshActivity" class="btn btn-small">Refresh</button></div>
    <p class="hint">Everything running right now. Refreshing, opening a second tab or coming back later shows the same live state.</p>
    <div class="table-wrap"><table>
      <thead><tr><th>Kind</th><th>Status</th><th>Job / repository</th><th>Started</th></tr></thead>
      <tbody>${
        runs.length ? runs.map((run) => `<tr class="clickable" data-run="${esc(run.id)}">
          <td>${esc(run.kind)}</td><td>${statusBadge(run.status)}</td>
          <td>${esc(run.jobName || run.repoName || "—")}</td>
          <td>${esc(fmtTime(run.startedAt))}</td></tr>`).join("")
        : `<tr><td colspan="4" class="empty">Nothing is running.</td></tr>`
      }</tbody></table></div></div>`);
  $("refreshActivity").onclick = renderActivity;
  document.querySelectorAll("tr.clickable").forEach((tr) =>
    (tr.onclick = () => (location.hash = "#/runs/" + tr.dataset.run)));
}

// ---- RUN VIEW (shared, live) -----------------------------------------------

let runStream = null;
let runLogSeq = 0;

function closeRunStream() {
  if (runStream) { runStream.close(); runStream = null; }
}

async function renderRun(id) {
  const r = await get(`/api/runs/${id}`);
  if (r.status === 404) { view(notFoundCard("Run")); return; }
  const run = r.body.run;

  view(`
    <div class="crumbs">${run.jobId ? `<a href="#/jobs/${esc(run.jobId)}">← ${esc(run.jobName || "job")}</a>`
      : `<a href="#/activity">← Activity</a>`}</div>
    <div class="card"><div class="card-head">
      <div><h2 style="text-transform:capitalize">${esc(run.kind)} <span id="runBadge">${statusBadge(run.status)}</span></h2>
        <p class="hint" id="runMeta"></p></div>
      <div class="row-actions">
        <button id="runStopBtn" class="btn btn-small btn-danger hidden">Stop</button>
        <span id="runDownloadWrap"></span>
      </div>
    </div>
      <div class="progress-head"><span class="muted" id="runPhase"></span><span class="percent" id="runPercent">0%</span></div>
      <div class="progressbar"><div class="progressbar-fill" id="runBar"></div></div>
      <div class="progress-stats"><span id="runFiles">0 / 0 files</span><span id="runBytes">0 B / 0 B</span></div>
      <div class="current-file" id="runCurrent"></div>
      <div id="runSummary"></div>
    </div>
    <div class="card"><h3>Log</h3><pre class="log" id="runLog"></pre></div>`);

  $("runStopBtn").onclick = async () => {
    $("runStopBtn").disabled = true;
    await post(`/api/runs/${id}/stop`);
  };

  runLogSeq = 0;
  updateRunView(run);
  openRunStream(id);
}

function openRunStream(id) {
  closeRunStream();
  runStream = new EventSource(`/api/runs/${id}/events`);
  runStream.onmessage = (e) => {
    let ev;
    try { ev = JSON.parse(e.data); } catch (_) { return; }
    if (ev.type === "run" && ev.run) updateRunView(ev.run);
    else if (ev.type === "progress" && ev.progress) updateProgress(ev.progress);
    else if (ev.type === "log" && ev.line) appendLogLine(ev.line);
  };
  // EventSource auto-reconnects and resumes via Last-Event-ID; nothing to do onerror.
}

function updateRunView(run) {
  const badge = $("runBadge");
  if (!badge) return; // navigated away
  badge.innerHTML = statusBadge(run.status);

  const started = fmtTime(run.startedAt);
  let meta = `started ${started} · ${runDuration(run)}`;
  if (run.params && run.params.target) meta += ` · target ${run.params.target}`;
  if (run.error) meta += ` · ${run.error}`;
  $("runMeta").textContent = meta;

  updateProgress(run.progress || {});
  // A finished successful run should read 100% even if restic's last status tick
  // (before the summary) was below it.
  if (run.status === "success" || run.status === "success_warnings") {
    $("runBar").style.width = "100%";
    $("runPercent").textContent = "100%";
  }
  renderSummary(run);

  const stop = $("runStopBtn");
  stop.classList.toggle("hidden", !isActive(run.status));
  stop.disabled = false;

  const phase = { starting: "Starting…", running: "Running…", success: "Complete", success_warnings: "Complete (warnings)", failed: "Failed", canceled: "Stopped", interrupted: "Interrupted" }[run.status] || run.status;
  if ($("runPhase")) $("runPhase").textContent = phase;

  // Download link for a finished download run.
  const dl = $("runDownloadWrap");
  if (run.kind === "download" && run.status === "success") {
    dl.innerHTML = `<a class="btn btn-small btn-primary" href="/api/runs/${esc(run.id)}/download">Download .zip</a>`;
  } else {
    dl.innerHTML = "";
  }
}

function updateProgress(p) {
  if (!$("runBar")) return;
  const pct = Math.max(0, Math.min(100, Math.round((Number(p.percent) || 0) * 100)));
  $("runBar").style.width = pct + "%";
  $("runPercent").textContent = pct + "%";
  $("runFiles").textContent =
    (Number(p.filesDone) || 0).toLocaleString() + " / " + (Number(p.totalFiles) || 0).toLocaleString() + " files";
  $("runBytes").textContent = fmtBytes(p.bytesDone) + " / " + fmtBytes(p.totalBytes);
  $("runCurrent").textContent = p.currentFile ? "▸ " + p.currentFile : "";
}

function renderSummary(run) {
  const box = $("runSummary");
  if (!box) return;
  const s = run.summary;
  if (!s) { box.innerHTML = ""; return; }
  const stat = (num, label) => `<div class="stat"><div class="num">${num}</div><div class="label">${label}</div></div>`;
  let cells;
  if (run.kind === "restore" || run.kind === "download") {
    cells = stat((s.filesRestored || 0).toLocaleString(), "files restored") +
      stat(fmtBytes(s.bytesRestored), "data restored") +
      (s.totalDuration ? stat(fmtDur(s.totalDuration), "duration") : "");
  } else {
    cells = stat((s.filesNew || 0).toLocaleString(), "new") +
      stat((s.filesChanged || 0).toLocaleString(), "changed") +
      stat((s.filesUnmodified || 0).toLocaleString(), "unchanged") +
      stat(fmtBytes(s.dataAdded), "data added") +
      (s.snapshotId ? stat(shortId(s.snapshotId), "snapshot") : "") +
      (s.totalDuration ? stat(fmtDur(s.totalDuration), "duration") : "");
  }
  box.innerHTML = `<div class="summary-grid">${cells}</div>`;
}

function appendLogLine(line) {
  const log = $("runLog");
  if (!log) return;
  if (line.seq && line.seq <= runLogSeq) return; // de-dup
  if (line.seq) runLogSeq = line.seq;
  const span = document.createElement("span");
  span.className = "l-" + (line.level || "info");
  span.textContent = (line.message || "") + "\n";
  log.appendChild(span);
  log.scrollTop = log.scrollHeight;
}

// ---- inline result helper --------------------------------------------------

function setInlineResult(id, kind, message, detail) {
  const el = $(id);
  if (!el) return;
  el.className = "result " + kind;
  el.innerHTML = esc(message) + (detail ? `<span class="detail">${esc(detail)}</span>` : "");
}

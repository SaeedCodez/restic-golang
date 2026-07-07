/* ------------------------------------------------------------------ *
 * restic backup demo — front-end logic (vanilla JS)
 *
 * Talks to the Go server's JSON API and listens to one persistent
 * Server-Sent Events stream for live backup/restore progress.
 * ------------------------------------------------------------------ */

"use strict";

// ---- tiny helpers ----------------------------------------------------------

const $ = (id) => document.getElementById(id);
const show = (el) => el.classList.remove("hidden");
const hide = (el) => el.classList.add("hidden");

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])
  );
}

function formatBytes(n) {
  n = Number(n) || 0;
  if (n < 1024) return n + " B";
  const units = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do {
    n /= 1024;
    i++;
  } while (n >= 1024 && i < units.length - 1);
  return n.toFixed(n < 10 ? 2 : 1) + " " + units[i];
}

function formatDuration(seconds) {
  seconds = Number(seconds) || 0;
  if (seconds < 60) return seconds.toFixed(1) + "s";
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return m + "m " + s + "s";
}

function formatTime(iso) {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

async function getJSON(url, opts) {
  const resp = await fetch(url, opts);
  const ct = resp.headers.get("content-type") || "";
  if (ct.includes("application/json")) return resp.json();
  return { ok: resp.ok, error: await resp.text() };
}

async function postJSON(url, body) {
  return getJSON(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body || {}),
  });
}

function setResult(el, kind, message, detail) {
  el.className = "result " + kind; // good | bad | info
  el.innerHTML =
    escapeHtml(message) +
    (detail ? '<span class="detail">' + escapeHtml(detail) + "</span>" : "");
  show(el);
}

// ---- shared state ----------------------------------------------------------

const state = {
  resticInstalled: false,
  configValid: false,
  busy: false,
  busyOp: "",
  repository: "",
  snapshots: [],
};

// Cached per-operation progress elements.
const els = {};

// ---- tab navigation --------------------------------------------------------

function setupTabs() {
  document.querySelectorAll(".tab").forEach((btn) => {
    btn.addEventListener("click", () => switchTab(btn.dataset.tab));
  });
}

function switchTab(name) {
  document.querySelectorAll(".tab").forEach((b) =>
    b.classList.toggle("active", b.dataset.tab === name)
  );
  document.querySelectorAll(".tabpanel").forEach((p) =>
    p.classList.toggle("active", p.id === "tab-" + name)
  );
}

// ---- settings --------------------------------------------------------------

function setupBackendToggle() {
  document.querySelectorAll("#backendToggle .seg").forEach((seg) => {
    seg.addEventListener("click", () => selectBackend(seg.dataset.backend));
  });
}

function selectBackend(type) {
  document.querySelectorAll("#backendToggle .seg").forEach((s) =>
    s.classList.toggle("active", s.dataset.backend === type)
  );
  $("localFields").classList.toggle("hidden", type !== "Local");
  $("s3Fields").classList.toggle("hidden", type !== "S3");
}

function currentBackend() {
  const active = document.querySelector("#backendToggle .seg.active");
  return active ? active.dataset.backend : "Local";
}

async function loadSettings() {
  const data = await getJSON("/api/settings");
  if (!data.ok) return;
  const c = data.settings || {};
  selectBackend(c.backendType || "Local");
  $("localPath").value = c.localPath || "";
  $("endpoint").value = c.endpoint || "";
  $("bucket").value = c.bucket || "";
  $("region").value = c.region || "";
  $("accessKey").value = c.accessKey || "";
  $("secretKey").value = c.secretKey || "";
  $("password").value = c.password || "";
}

function gatherSettings() {
  return {
    backendType: currentBackend(),
    localPath: $("localPath").value.trim(),
    endpoint: $("endpoint").value.trim(),
    bucket: $("bucket").value.trim(),
    region: $("region").value.trim(),
    accessKey: $("accessKey").value.trim(),
    secretKey: $("secretKey").value,
    password: $("password").value,
  };
}

async function saveSettings() {
  const res = $("settingsResult");
  const data = await postJSON("/api/settings", gatherSettings());
  if (data.ok) {
    setResult(res, "good", "Settings saved.");
    await loadStatus();
    await loadSnapshots();
  } else {
    setResult(res, "bad", data.error || "Could not save settings.");
  }
}

async function testConnection() {
  const res = $("settingsResult");
  setResult(res, "info", "Testing connection…");
  const data = await postJSON("/api/test", {});
  if (data.ok) {
    setResult(res, "good", data.message || "Connected.");
    hide($("initRepo"));
  } else if (data.code === "no_restic" || data.code === "bad_config") {
    setResult(res, "bad", data.error || data.message || "Failed.");
  } else {
    setResult(res, "bad", data.message || data.error || "Connection failed.", data.detail);
    // If the repository simply isn't initialized yet, offer to create it.
    if (data.initialized === false) show($("initRepo"));
    else hide($("initRepo"));
  }
}

async function initRepository() {
  const res = $("settingsResult");
  setResult(res, "info", "Initializing repository…");
  const data = await postJSON("/api/init", {});
  if (data.ok) {
    setResult(res, "good", data.message || "Repository initialized.");
    hide($("initRepo"));
    await loadStatus();
    await loadSnapshots();
  } else {
    setResult(res, "bad", data.error || "Initialization failed.", data.detail);
  }
}

// ---- status & global UI ----------------------------------------------------

async function loadStatus() {
  const s = await getJSON("/api/status");
  state.resticInstalled = !!s.resticInstalled;
  state.busy = !!s.busy;
  state.busyOp = s.busyOp || "";
  state.configValid = !!s.configValid;
  state.repository = s.repository || "";

  // restic pill
  const rp = $("resticPill");
  if (state.resticInstalled) {
    rp.className = "pill pill-good";
    rp.textContent = s.resticVersion ? s.resticVersion.split("\n")[0] : "restic ready";
  } else {
    rp.className = "pill pill-bad";
    rp.textContent = "restic not found";
  }

  // repository pill
  const repoPill = $("repoPill");
  if (state.repository) {
    repoPill.className = "pill pill-muted";
    repoPill.textContent = (s.backendType || "") + ": " + state.repository;
  } else {
    repoPill.className = "pill pill-muted";
    repoPill.textContent = "no repository";
  }

  // global banner if restic is missing
  const banner = $("globalBanner");
  if (!state.resticInstalled) {
    banner.innerHTML =
      "<strong>restic is not installed.</strong> This app shells out to the <code>restic</code> binary. " +
      "Install it first — for example <code>brew install restic</code> (macOS), " +
      "<code>apt install restic</code> (Debian/Ubuntu), or see " +
      '<a href="https://restic.readthedocs.io/en/stable/020_installation.html" target="_blank" rel="noopener">the install guide</a>. ' +
      "Then restart this app.";
    show(banner);
  } else {
    hide(banner);
  }

  updateBusyPill();
  applyGlobalState();
}

function updateBusyPill() {
  const pill = $("busyPill");
  if (state.busy) {
    pill.className = "pill pill-busy";
    const labels = {
      backup: "backing up…",
      restore: "restoring…",
      download: "preparing download…",
      init: "initializing…",
    };
    pill.textContent = labels[state.busyOp] || "working…";
  } else {
    pill.className = "pill pill-idle";
    pill.textContent = "idle";
  }
}

function applyGlobalState() {
  const ok = state.resticInstalled;
  const busy = state.busy;
  const ready = ok && !busy;

  $("startBackup").disabled = !ready || !state.configValid;
  $("startRestore").disabled = !ready || !state.configValid;
  $("testConn").disabled = !ready;
  $("initRepo").disabled = !ready;
  $("refreshSnaps").disabled = !ready;
  document
    .querySelectorAll(".download-btn, .restore-row-btn")
    .forEach((b) => (b.disabled = !ready));
}

// ---- backup ----------------------------------------------------------------

async function startBackup() {
  const source = $("sourcePath").value.trim();
  const data = await postJSON("/api/backup", { source: source });
  if (!data.ok) {
    // Show the error in the backup progress card's log area.
    resetProgress("backup");
    show(els.backup.card);
    appendLog("backup", data.error || "Could not start backup.", "error");
  }
  // Success path is driven entirely by SSE events.
}

// ---- restore ---------------------------------------------------------------

async function startRestore() {
  const snapshotId = $("restoreSnap").value;
  const target = $("restoreTarget").value.trim();
  const data = await postJSON("/api/restore", { snapshotId, target });
  if (!data.ok) {
    resetProgress("restore");
    show(els.restore.card);
    appendLog("restore", data.error || "Could not start restore.", "error");
  }
}

// ---- snapshots -------------------------------------------------------------

async function loadSnapshots() {
  const res = $("snapsResult");
  const data = await getJSON("/api/snapshots");

  if (!data.ok) {
    state.snapshots = [];
    renderSnapsTable([]);
    populateRestoreSelect([]);
    if (data.code === "not_initialized") {
      setResult(res, "info", data.error || "Repository is not initialized yet. Initialize it in Settings.");
    } else if (data.code === "no_restic") {
      setResult(res, "bad", data.error || "restic is not installed.");
    } else {
      setResult(res, "bad", data.error || "Could not load snapshots.");
    }
    return;
  }

  hide(res);
  state.snapshots = data.snapshots || [];
  renderSnapsTable(state.snapshots);
  populateRestoreSelect(state.snapshots);
}

function renderSnapsTable(snaps) {
  const body = $("snapsBody");
  if (!snaps.length) {
    body.innerHTML =
      '<tr><td colspan="6" class="empty">No snapshots yet. Run a backup to create one.</td></tr>';
    return;
  }
  // Newest first.
  const sorted = snaps.slice().sort((a, b) => new Date(b.time) - new Date(a.time));
  body.innerHTML = sorted
    .map((s) => {
      const id = escapeHtml(s.id);
      const shortId = escapeHtml(s.shortId || s.id.slice(0, 8));
      const paths = (s.paths || []).map(escapeHtml).join(", ");
      const size = s.sizeBytes ? formatBytes(s.sizeBytes) : "—";
      const files = s.fileCount ? s.fileCount.toLocaleString() : "—";
      return (
        "<tr>" +
        '<td><span class="snap-id">' + shortId + "</span></td>" +
        "<td>" + escapeHtml(formatTime(s.time)) + "</td>" +
        '<td class="snap-paths">' + paths + "</td>" +
        "<td>" + size + "</td>" +
        "<td>" + files + "</td>" +
        '<td class="right"><div class="row-actions">' +
        '<button class="btn btn-small restore-row-btn" data-id="' + id + '">Restore</button>' +
        '<button class="btn btn-small download-btn" data-id="' + id + '" data-short="' + shortId + '">Download</button>' +
        "</div></td>" +
        "</tr>"
      );
    })
    .join("");

  // Wire row buttons.
  body.querySelectorAll(".restore-row-btn").forEach((b) =>
    b.addEventListener("click", () => prefillRestore(b.dataset.id))
  );
  body.querySelectorAll(".download-btn").forEach((b) =>
    b.addEventListener("click", () => downloadSnapshot(b.dataset.id, b.dataset.short))
  );
  applyGlobalState();
}

function populateRestoreSelect(snaps) {
  const sel = $("restoreSnap");
  const prev = sel.value;
  if (!snaps.length) {
    sel.innerHTML = '<option value="">— no snapshots yet —</option>';
    return;
  }
  const sorted = snaps.slice().sort((a, b) => new Date(b.time) - new Date(a.time));
  sel.innerHTML = sorted
    .map((s) => {
      const short = escapeHtml(s.shortId || s.id.slice(0, 8));
      const when = escapeHtml(formatTime(s.time));
      return '<option value="' + escapeHtml(s.id) + '">' + short + " · " + when + "</option>";
    })
    .join("");
  if (prev) sel.value = prev;
}

function prefillRestore(id) {
  switchTab("restore");
  $("restoreSnap").value = id;
  $("restoreTarget").focus();
}

async function downloadSnapshot(id, shortId) {
  const res = $("snapsResult");
  setResult(res, "info", "Preparing download… restoring the snapshot to a temporary folder and zipping it.");
  try {
    const resp = await fetch("/api/download?id=" + encodeURIComponent(id));
    if (!resp.ok) {
      setResult(res, "bad", "Download failed: " + (await resp.text()));
      return;
    }
    const blob = await resp.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "snapshot-" + (shortId || "backup") + ".zip";
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    setResult(res, "good", "Download ready: snapshot-" + (shortId || "backup") + ".zip (" + formatBytes(blob.size) + ")");
  } catch (e) {
    setResult(res, "bad", "Download failed: " + e.message);
  }
}

// ---- progress rendering (shared by backup & restore) -----------------------

function cacheProgressEls() {
  for (const op of ["backup", "restore"]) {
    const cap = op[0].toUpperCase() + op.slice(1);
    els[op] = {
      card: $(op + "Progress"),
      bar: $(op + "Bar"),
      percent: $(op + "Percent"),
      phase: $(op + "Phase"),
      files: $(op + "Files"),
      bytes: $(op + "Bytes"),
      current: $(op + "Current"),
      summary: $(op + "Summary"),
      log: $(op + "Log"),
    };
  }
}

function resetProgress(op) {
  const e = els[op];
  e.bar.style.width = "0%";
  e.percent.textContent = "0%";
  e.phase.textContent = "Starting…";
  e.files.textContent = "0 / 0 files";
  e.bytes.textContent = "0 B / 0 B";
  e.current.textContent = "";
  e.log.textContent = "";
  hide(e.summary);
  e.summary.className = "summary hidden";
}

function updateProgress(op, ev) {
  const e = els[op];
  show(e.card);
  const pct = Math.max(0, Math.min(100, Math.round((ev.percent || 0) * 100)));
  e.bar.style.width = pct + "%";
  e.percent.textContent = pct + "%";
  e.phase.textContent =
    op === "restore"
      ? "Restoring…"
      : Number(ev.totalBytes) > 0
      ? "Backing up…"
      : "Scanning files…";
  e.files.textContent =
    (Number(ev.filesDone) || 0).toLocaleString() +
    " / " +
    (Number(ev.totalFiles) || 0).toLocaleString() +
    " files";
  e.bytes.textContent = formatBytes(ev.bytesDone) + " / " + formatBytes(ev.totalBytes);
  e.current.textContent = ev.currentFile ? "▸ " + ev.currentFile : "";
}

function appendLog(op, message, level) {
  const e = els[op];
  if (!e) return;
  const span = document.createElement("span");
  span.className = "l-" + (level || "info");
  span.textContent = message + "\n";
  e.log.appendChild(span);
  e.log.scrollTop = e.log.scrollHeight;
}

function renderBackupSummary(sum, ok) {
  const e = els.backup;
  const stat = (num, label) =>
    '<div class="stat"><div class="num">' + num + '</div><div class="label">' + label + "</div></div>";

  let html = "<h4>" + (ok ? "Backup complete ✓" : "Backup finished") + "</h4>";
  html += '<div class="summary-grid">';
  html += stat((sum.filesNew || 0).toLocaleString(), "files new");
  html += stat((sum.filesChanged || 0).toLocaleString(), "files changed");
  html += stat((sum.filesUnmodified || 0).toLocaleString(), "files unchanged");
  html += stat(formatBytes(sum.dataAdded), "data added");
  html += stat(formatBytes(sum.totalBytesProcessed), "total processed");
  html += stat(formatDuration(sum.totalDuration), "duration");
  html += "</div>";

  // Make the incremental nature of the second run obvious.
  if ((sum.filesUnmodified || 0) > 0) {
    html +=
      '<div class="incremental-note">♻ Incremental run: ' +
      (sum.filesUnmodified || 0).toLocaleString() +
      " file(s) were unchanged and skipped. Only " +
      formatBytes(sum.dataAdded) +
      " of new/changed data was actually stored.</div>";
  } else {
    html +=
      '<div class="incremental-note">First backup of this folder: all ' +
      (sum.filesNew || 0).toLocaleString() +
      " file(s) were stored. Run it again after changing a file to see an incremental backup.</div>";
  }

  if (sum.snapshotId) {
    html += '<div class="snapid-chip">snapshot ' + escapeHtml(String(sum.snapshotId).slice(0, 8)) + "</div>";
  }

  e.summary.innerHTML = html;
  e.summary.className = "summary " + (ok ? "ok" : "fail");
  show(e.summary);
}

function renderRestoreSummary(sum, ok) {
  const e = els.restore;
  const stat = (num, label) =>
    '<div class="stat"><div class="num">' + num + '</div><div class="label">' + label + "</div></div>";

  let html = "<h4>" + (ok ? "Restore complete ✓" : "Restore finished") + "</h4>";
  html += '<div class="summary-grid">';
  html += stat(
    (sum.filesRestored || 0).toLocaleString() + " / " + (sum.totalFiles || 0).toLocaleString(),
    "files restored"
  );
  html += stat(formatBytes(sum.bytesRestored) + " / " + formatBytes(sum.totalBytes), "data restored");
  if (sum.totalDuration) html += stat(formatDuration(sum.totalDuration), "duration");
  html += "</div>";

  e.summary.innerHTML = html;
  e.summary.className = "summary " + (ok ? "ok" : "fail");
  show(e.summary);
}

// ---- SSE event handling ----------------------------------------------------

function connectSSE() {
  const es = new EventSource("/api/events");
  es.onmessage = (e) => {
    let ev;
    try {
      ev = JSON.parse(e.data);
    } catch (_) {
      return;
    }
    handleEvent(ev);
  };
  es.onerror = () => {
    // EventSource reconnects automatically; nothing to do here.
  };
}

function handleEvent(ev) {
  switch (ev.type) {
    case "busy":
      state.busy = !!ev.busy;
      state.busyOp = ev.op || "";
      updateBusyPill();
      applyGlobalState();
      break;

    case "started":
      if (ev.op === "backup" || ev.op === "restore") {
        resetProgress(ev.op);
        show(els[ev.op].card);
        appendLog(ev.op, ev.message || "Started.", "ok");
      }
      break;

    case "status":
      if (ev.op === "backup" || ev.op === "restore") updateProgress(ev.op, ev);
      break;

    case "log":
      if (ev.op === "backup" || ev.op === "restore") {
        appendLog(ev.op, ev.message || "", ev.level || "info");
      }
      break;

    case "summary":
      if (ev.op === "backup") renderBackupSummary(ev.summary || {}, true);
      else if (ev.op === "restore") renderRestoreSummary(ev.summary || {}, true);
      break;

    case "done": {
      const op = ev.op;
      if (op === "backup" || op === "restore") {
        const e = els[op];
        if (ev.ok) {
          e.bar.style.width = "100%";
          e.percent.textContent = "100%";
          e.phase.textContent = "Complete ✓";
        } else {
          e.phase.textContent = "Failed ✗";
          // Mark any shown summary as failed.
          if (!e.summary.classList.contains("hidden")) {
            e.summary.className = "summary fail";
          }
        }
        appendLog(op, ev.message || (ev.ok ? "Done." : "Failed."), ev.ok ? "ok" : "error");
      }
      // A finished backup/restore changes the snapshot list.
      if (op === "backup" || op === "restore") loadSnapshots();
      break;
    }
  }
}

// ---- boot ------------------------------------------------------------------

document.addEventListener("DOMContentLoaded", async () => {
  cacheProgressEls();
  setupTabs();
  setupBackendToggle();

  $("saveSettings").addEventListener("click", saveSettings);
  $("testConn").addEventListener("click", testConnection);
  $("initRepo").addEventListener("click", initRepository);
  $("startBackup").addEventListener("click", startBackup);
  $("startRestore").addEventListener("click", startRestore);
  $("refreshSnaps").addEventListener("click", loadSnapshots);

  connectSSE();
  await loadSettings();
  await loadStatus();
  await loadSnapshots();
});

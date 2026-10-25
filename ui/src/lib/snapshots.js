/** Helpers for reading what a snapshot holds, from the tags this app sets. */

const DB_TAG = "resticweb-db:";
const PRE_RESTORE_TAG = "resticweb-prerestore";

export const ENGINE_LABEL = { postgres: "PostgreSQL", mysql: "MySQL", mariadb: "MariaDB" };

export function engineLabel(engine) {
  return ENGINE_LABEL[engine] || engine || "Database";
}

/** The engine a database snapshot was dumped from, or "" for a file snapshot. */
export function snapshotEngine(snapshot) {
  for (const t of snapshot?.tags || []) {
    if (t.startsWith(DB_TAG)) return t.slice(DB_TAG.length);
  }
  return "";
}

/** A safety copy taken right before a database restore replaced the data. */
export function isSafetySnapshot(snapshot) {
  return (snapshot?.tags || []).includes(PRE_RESTORE_TAG);
}

/** The database a dump snapshot holds ("/shop.sql" → "shop"). */
export function dumpDatabaseName(snapshot) {
  const p = (snapshot?.paths || [])[0] || "";
  return p.replace(/^\//, "").replace(/\.sql$/, "");
}

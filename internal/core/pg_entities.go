package core

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ---- repositories ----------------------------------------------------------

type RepoStore struct {
	Pool *pgxpool.Pool
}

func newRepoStore(pool *pgxpool.Pool) *RepoStore { return &RepoStore{Pool: pool} }

const repoCols = `id, name, created_at, updated_at, backend_type, local_path,
	endpoint, bucket, region, access_key, secret_key, password`

func scanRepo(row interface{ Scan(dest ...any) error }) (Repository, error) {
	var r Repository
	var localPath, endpoint, bucket, region, accessKey, secretKey *string
	err := row.Scan(
		&r.ID, &r.Name, &r.CreatedAt, &r.UpdatedAt, &r.BackendType, &localPath,
		&endpoint, &bucket, &region, &accessKey, &secretKey, &r.Password,
	)
	if err != nil {
		return Repository{}, err
	}
	if localPath != nil {
		r.LocalPath = *localPath
	}
	if endpoint != nil {
		r.Endpoint = *endpoint
	}
	if bucket != nil {
		r.Bucket = *bucket
	}
	if region != nil {
		r.Region = *region
	}
	if accessKey != nil {
		r.AccessKey = *accessKey
	}
	if secretKey != nil {
		r.SecretKey = *secretKey
	}
	return r, nil
}

func (s *RepoStore) List() []Repository {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := s.Pool.Query(ctx, `SELECT `+repoCols+` FROM repositories ORDER BY LOWER(name), id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Repository
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return out
		}
		out = append(out, r)
	}
	return out
}

func (s *RepoStore) Get(id string) (Repository, bool) {
	ctx, cancel := dbCtx()
	defer cancel()
	r, err := scanRepo(s.Pool.QueryRow(ctx, `SELECT `+repoCols+` FROM repositories WHERE id = $1`, id))
	if err != nil {
		return Repository{}, false
	}
	return r, true
}

func (s *RepoStore) Exists(id string) bool {
	_, ok := s.Get(id)
	return ok
}

func (s *RepoStore) Count() int {
	return tableCount(s.Pool, "repositories")
}

func (s *RepoStore) Create(item Repository) (Repository, error) {
	var zero Repository
	name, err := requireUniqueName(s.Pool, "repositories", "repository", item.Name, "")
	if err != nil {
		return zero, err
	}
	// Microsecond truncation matches Postgres timestamptz so Create/Update
	// timestamps compare equal without a round-trip re-read.
	now := time.Now().UTC().Truncate(time.Microsecond)
	item.ID = newID()
	item.Name = name
	item.CreatedAt = now
	item.UpdatedAt = now
	ctx, cancel := dbCtx()
	defer cancel()
	_, err = s.Pool.Exec(ctx, `INSERT INTO repositories (`+repoCols+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item.BackendType, emptyToNil(item.LocalPath),
		emptyToNil(item.Endpoint), emptyToNil(item.Bucket), emptyToNil(item.Region),
		emptyToNil(item.AccessKey), emptyToNil(item.SecretKey), item.Password,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a repository named %q already exists", name)
		}
		return zero, err
	}
	return item, nil
}

func (s *RepoStore) Update(id string, item Repository) (Repository, error) {
	var zero Repository
	prev, ok := s.Get(id)
	if !ok {
		return zero, notFoundf("repository not found")
	}
	name, err := requireUniqueName(s.Pool, "repositories", "repository", item.Name, id)
	if err != nil {
		return zero, err
	}
	item.ID = id
	item.Name = name
	item.CreatedAt = prev.CreatedAt
	item.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.Pool.Exec(ctx, `UPDATE repositories SET
		name=$2, updated_at=$3, backend_type=$4, local_path=$5,
		endpoint=$6, bucket=$7, region=$8, access_key=$9, secret_key=$10, password=$11
		WHERE id=$1`,
		item.ID, item.Name, item.UpdatedAt, item.BackendType, emptyToNil(item.LocalPath),
		emptyToNil(item.Endpoint), emptyToNil(item.Bucket), emptyToNil(item.Region),
		emptyToNil(item.AccessKey), emptyToNil(item.SecretKey), item.Password,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a repository named %q already exists", name)
		}
		return zero, err
	}
	if tag.RowsAffected() == 0 {
		return zero, notFoundf("repository not found")
	}
	return item, nil
}

func (s *RepoStore) Delete(id string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.Pool.Exec(ctx, `DELETE FROM repositories WHERE id = $1`, id)
	if err != nil {
		if isFKViolation(err) {
			return conflictf("this repository is still used by one or more jobs")
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return notFoundf("repository not found")
	}
	return nil
}

// ---- folders ---------------------------------------------------------------

type FolderStore struct {
	Pool *pgxpool.Pool
}

func newFolderStore(pool *pgxpool.Pool) *FolderStore { return &FolderStore{Pool: pool} }

const folderCols = `id, name, created_at, updated_at, path`

func scanFolder(row interface{ Scan(dest ...any) error }) (Folder, error) {
	var f Folder
	err := row.Scan(&f.ID, &f.Name, &f.CreatedAt, &f.UpdatedAt, &f.Path)
	return f, err
}

func (s *FolderStore) List() []Folder {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := s.Pool.Query(ctx, `SELECT `+folderCols+` FROM folders ORDER BY LOWER(name), id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return out
		}
		out = append(out, f)
	}
	return out
}

func (s *FolderStore) Get(id string) (Folder, bool) {
	ctx, cancel := dbCtx()
	defer cancel()
	f, err := scanFolder(s.Pool.QueryRow(ctx, `SELECT `+folderCols+` FROM folders WHERE id = $1`, id))
	if err != nil {
		return Folder{}, false
	}
	return f, true
}

func (s *FolderStore) Exists(id string) bool {
	_, ok := s.Get(id)
	return ok
}

func (s *FolderStore) Count() int {
	return tableCount(s.Pool, "folders")
}

func (s *FolderStore) Create(item Folder) (Folder, error) {
	var zero Folder
	name, err := requireUniqueName(s.Pool, "folders", "folder", item.Name, "")
	if err != nil {
		return zero, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	item.ID = newID()
	item.Name = name
	item.CreatedAt = now
	item.UpdatedAt = now
	ctx, cancel := dbCtx()
	defer cancel()
	_, err = s.Pool.Exec(ctx, `INSERT INTO folders (`+folderCols+`) VALUES ($1,$2,$3,$4,$5)`,
		item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item.Path)
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a folder named %q already exists", name)
		}
		return zero, err
	}
	return item, nil
}

func (s *FolderStore) Update(id string, item Folder) (Folder, error) {
	var zero Folder
	prev, ok := s.Get(id)
	if !ok {
		return zero, notFoundf("folder not found")
	}
	name, err := requireUniqueName(s.Pool, "folders", "folder", item.Name, id)
	if err != nil {
		return zero, err
	}
	item.ID = id
	item.Name = name
	item.CreatedAt = prev.CreatedAt
	item.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.Pool.Exec(ctx, `UPDATE folders SET name=$2, updated_at=$3, path=$4 WHERE id=$1`,
		item.ID, item.Name, item.UpdatedAt, item.Path)
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a folder named %q already exists", name)
		}
		return zero, err
	}
	if tag.RowsAffected() == 0 {
		return zero, notFoundf("folder not found")
	}
	return item, nil
}

func (s *FolderStore) Delete(id string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.Pool.Exec(ctx, `DELETE FROM folders WHERE id = $1`, id)
	if err != nil {
		if isFKViolation(err) {
			return conflictf("this folder is still used by one or more jobs")
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return notFoundf("folder not found")
	}
	return nil
}

// ---- databases -------------------------------------------------------------

type DatabaseStore struct {
	Pool *pgxpool.Pool
}

func newDatabaseStore(pool *pgxpool.Pool) *DatabaseStore { return &DatabaseStore{Pool: pool} }

const databaseCols = `id, name, created_at, updated_at, engine, container, owner_kind, owner_uuid, owner_service, database_name`

func scanDatabase(row interface{ Scan(dest ...any) error }) (Database, error) {
	var d Database
	var kind, uuid, service *string
	err := row.Scan(&d.ID, &d.Name, &d.CreatedAt, &d.UpdatedAt, &d.Engine, &d.Container, &kind, &uuid, &service, &d.DatabaseName)
	if err != nil {
		return Database{}, err
	}
	for _, f := range []struct {
		src *string
		dst *string
	}{{kind, &d.OwnerKind}, {uuid, &d.OwnerUUID}, {service, &d.OwnerService}} {
		if f.src != nil {
			*f.dst = *f.src
		}
	}
	return d, nil
}

func (s *DatabaseStore) List() []Database {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := s.Pool.Query(ctx, `SELECT `+databaseCols+` FROM databases ORDER BY LOWER(name), id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Database
	for rows.Next() {
		d, err := scanDatabase(rows)
		if err != nil {
			return out
		}
		out = append(out, d)
	}
	return out
}

func (s *DatabaseStore) Get(id string) (Database, bool) {
	ctx, cancel := dbCtx()
	defer cancel()
	d, err := scanDatabase(s.Pool.QueryRow(ctx, `SELECT `+databaseCols+` FROM databases WHERE id = $1`, id))
	if err != nil {
		return Database{}, false
	}
	return d, true
}

func (s *DatabaseStore) Exists(id string) bool {
	_, ok := s.Get(id)
	return ok
}

func (s *DatabaseStore) Count() int {
	return tableCount(s.Pool, "databases")
}

func (s *DatabaseStore) Create(item Database) (Database, error) {
	var zero Database
	name, err := requireUniqueName(s.Pool, "databases", "database source", item.Name, "")
	if err != nil {
		return zero, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	item.ID = newID()
	item.Name = name
	item.CreatedAt = now
	item.UpdatedAt = now
	ctx, cancel := dbCtx()
	defer cancel()
	_, err = s.Pool.Exec(ctx, `INSERT INTO databases (`+databaseCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item.Engine, item.Container,
		emptyToNil(item.OwnerKind), emptyToNil(item.OwnerUUID), emptyToNil(item.OwnerService), item.DatabaseName)
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a database source named %q already exists", name)
		}
		return zero, err
	}
	return item, nil
}

func (s *DatabaseStore) Update(id string, item Database) (Database, error) {
	var zero Database
	prev, ok := s.Get(id)
	if !ok {
		return zero, notFoundf("database source not found")
	}
	name, err := requireUniqueName(s.Pool, "databases", "database source", item.Name, id)
	if err != nil {
		return zero, err
	}
	item.ID = id
	item.Name = name
	item.CreatedAt = prev.CreatedAt
	item.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.Pool.Exec(ctx, `UPDATE databases SET name=$2, updated_at=$3, engine=$4, container=$5,
		owner_kind=$6, owner_uuid=$7, owner_service=$8, database_name=$9 WHERE id=$1`,
		item.ID, item.Name, item.UpdatedAt, item.Engine, item.Container,
		emptyToNil(item.OwnerKind), emptyToNil(item.OwnerUUID), emptyToNil(item.OwnerService), item.DatabaseName)
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a database source named %q already exists", name)
		}
		return zero, err
	}
	if tag.RowsAffected() == 0 {
		return zero, notFoundf("database source not found")
	}
	return item, nil
}

// SetContainer records the name a source's container was last found under.
func (s *DatabaseStore) SetContainer(id, container string) {
	ctx, cancel := dbCtx()
	defer cancel()
	_, _ = s.Pool.Exec(ctx, `UPDATE databases SET container=$2 WHERE id=$1`, id, container)
}

func (s *DatabaseStore) Delete(id string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.Pool.Exec(ctx, `DELETE FROM databases WHERE id = $1`, id)
	if err != nil {
		if isFKViolation(err) {
			return conflictf("this database source is still used by one or more jobs")
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return notFoundf("database source not found")
	}
	return nil
}

// ---- jobs ------------------------------------------------------------------

type JobStore struct {
	Pool *pgxpool.Pool
}

func newJobStore(pool *pgxpool.Pool) *JobStore { return &JobStore{Pool: pool} }

const jobCols = `id, name, created_at, updated_at, folder_id, repository_id, schedule, retention, database_id`

func scanJob(row interface{ Scan(dest ...any) error }) (Job, error) {
	var j Job
	var sched []byte
	var ret []byte
	var folderID, databaseID *string
	err := row.Scan(&j.ID, &j.Name, &j.CreatedAt, &j.UpdatedAt, &folderID, &j.RepositoryID, &sched, &ret, &databaseID)
	if err != nil {
		return Job{}, err
	}
	if folderID != nil {
		j.FolderID = *folderID
	}
	if databaseID != nil {
		j.DatabaseID = *databaseID
	}
	if len(sched) > 0 {
		var s JobSchedule
		if json.Unmarshal(sched, &s) == nil {
			j.Schedule = &s
		}
	}
	if len(ret) > 0 {
		var r JobRetention
		if json.Unmarshal(ret, &r) == nil {
			j.Retention = &r
		}
	}
	return j, nil
}

func (s *JobStore) List() []Job {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := s.Pool.Query(ctx, `SELECT `+jobCols+` FROM jobs ORDER BY LOWER(name), id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return out
		}
		out = append(out, j)
	}
	return out
}

func (s *JobStore) Get(id string) (Job, bool) {
	ctx, cancel := dbCtx()
	defer cancel()
	j, err := scanJob(s.Pool.QueryRow(ctx, `SELECT `+jobCols+` FROM jobs WHERE id = $1`, id))
	if err != nil {
		return Job{}, false
	}
	return j, true
}

func (s *JobStore) Exists(id string) bool {
	_, ok := s.Get(id)
	return ok
}

func (s *JobStore) Count() int {
	return tableCount(s.Pool, "jobs")
}

func (s *JobStore) Create(item Job) (Job, error) {
	var zero Job
	name, err := requireUniqueName(s.Pool, "jobs", "job", item.Name, "")
	if err != nil {
		return zero, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	item.ID = newID()
	item.Name = name
	item.CreatedAt = now
	item.UpdatedAt = now
	ctx, cancel := dbCtx()
	defer cancel()
	if item.Retention != nil {
		_ = item.Retention.Validate() // normalize preset counts when valid/disabled
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO jobs (`+jobCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		item.ID, item.Name, item.CreatedAt, item.UpdatedAt, emptyToNil(item.FolderID), item.RepositoryID,
		scheduleJSON(item.Schedule), retentionJSON(item.Retention), emptyToNil(item.DatabaseID))
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a job named %q already exists", name)
		}
		if isFKViolation(err) {
			return zero, validf("the chosen source or repository no longer exists")
		}
		return zero, err
	}
	return item, nil
}

func (s *JobStore) Update(id string, item Job) (Job, error) {
	var zero Job
	prev, ok := s.Get(id)
	if !ok {
		return zero, notFoundf("job not found")
	}
	name, err := requireUniqueName(s.Pool, "jobs", "job", item.Name, id)
	if err != nil {
		return zero, err
	}
	item.ID = id
	item.Name = name
	item.CreatedAt = prev.CreatedAt
	item.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	if item.Retention != nil {
		_ = item.Retention.Validate()
	}
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.Pool.Exec(ctx, `UPDATE jobs SET
		name=$2, updated_at=$3, folder_id=$4, repository_id=$5, schedule=$6, retention=$7, database_id=$8 WHERE id=$1`,
		item.ID, item.Name, item.UpdatedAt, emptyToNil(item.FolderID), item.RepositoryID,
		scheduleJSON(item.Schedule), retentionJSON(item.Retention), emptyToNil(item.DatabaseID))
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a job named %q already exists", name)
		}
		if isFKViolation(err) {
			return zero, validf("the chosen source or repository no longer exists")
		}
		return zero, err
	}
	if tag.RowsAffected() == 0 {
		return zero, notFoundf("job not found")
	}
	return item, nil
}

func (s *JobStore) Delete(id string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.Pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return notFoundf("job not found")
	}
	return nil
}

// ---- shared helpers --------------------------------------------------------

func tableCount(pool *pgxpool.Pool, table string) int {
	ctx, cancel := dbCtx()
	defer cancel()
	var n int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		return 0
	}
	return n
}

func requireUniqueName(pool *pgxpool.Pool, table, kind, name, exceptID string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", validf("a %s name is required", kind)
	}
	ctx, cancel := dbCtx()
	defer cancel()
	var n int
	err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM "+table+" WHERE LOWER(name) = LOWER($1) AND id <> $2",
		name, exceptID,
	).Scan(&n)
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", conflictf("a %s named %q already exists", kind, name)
	}
	return name, nil
}

func emptyToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scheduleJSON(s *JobSchedule) any {
	if s == nil {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return b
}

func retentionJSON(r *JobRetention) any {
	if r == nil {
		return nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil
	}
	return b
}

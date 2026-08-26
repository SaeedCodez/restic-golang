package main

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ---- repositories ----------------------------------------------------------

type repoStore struct {
	pool *pgxpool.Pool
}

func newRepoStore(pool *pgxpool.Pool) *repoStore { return &repoStore{pool: pool} }

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

func (s *repoStore) List() []Repository {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT `+repoCols+` FROM repositories ORDER BY LOWER(name), id`)
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

func (s *repoStore) Get(id string) (Repository, bool) {
	ctx, cancel := dbCtx()
	defer cancel()
	r, err := scanRepo(s.pool.QueryRow(ctx, `SELECT `+repoCols+` FROM repositories WHERE id = $1`, id))
	if err != nil {
		return Repository{}, false
	}
	return r, true
}

func (s *repoStore) Exists(id string) bool {
	_, ok := s.Get(id)
	return ok
}

func (s *repoStore) Count() int {
	return tableCount(s.pool, "repositories")
}

func (s *repoStore) Create(item Repository) (Repository, error) {
	var zero Repository
	name, err := requireUniqueName(s.pool, "repositories", "repository", item.Name, "")
	if err != nil {
		return zero, err
	}
	now := time.Now().UTC()
	item.ID = newID()
	item.Name = name
	item.CreatedAt = now
	item.UpdatedAt = now
	ctx, cancel := dbCtx()
	defer cancel()
	_, err = s.pool.Exec(ctx, `INSERT INTO repositories (`+repoCols+`)
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

func (s *repoStore) Update(id string, item Repository) (Repository, error) {
	var zero Repository
	prev, ok := s.Get(id)
	if !ok {
		return zero, notFoundf("repository not found")
	}
	name, err := requireUniqueName(s.pool, "repositories", "repository", item.Name, id)
	if err != nil {
		return zero, err
	}
	item.ID = id
	item.Name = name
	item.CreatedAt = prev.CreatedAt
	item.UpdatedAt = time.Now().UTC()
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.pool.Exec(ctx, `UPDATE repositories SET
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

func (s *repoStore) Delete(id string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.pool.Exec(ctx, `DELETE FROM repositories WHERE id = $1`, id)
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

type folderStore struct {
	pool *pgxpool.Pool
}

func newFolderStore(pool *pgxpool.Pool) *folderStore { return &folderStore{pool: pool} }

const folderCols = `id, name, created_at, updated_at, path`

func scanFolder(row interface{ Scan(dest ...any) error }) (Folder, error) {
	var f Folder
	err := row.Scan(&f.ID, &f.Name, &f.CreatedAt, &f.UpdatedAt, &f.Path)
	return f, err
}

func (s *folderStore) List() []Folder {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT `+folderCols+` FROM folders ORDER BY LOWER(name), id`)
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

func (s *folderStore) Get(id string) (Folder, bool) {
	ctx, cancel := dbCtx()
	defer cancel()
	f, err := scanFolder(s.pool.QueryRow(ctx, `SELECT `+folderCols+` FROM folders WHERE id = $1`, id))
	if err != nil {
		return Folder{}, false
	}
	return f, true
}

func (s *folderStore) Exists(id string) bool {
	_, ok := s.Get(id)
	return ok
}

func (s *folderStore) Count() int {
	return tableCount(s.pool, "folders")
}

func (s *folderStore) Create(item Folder) (Folder, error) {
	var zero Folder
	name, err := requireUniqueName(s.pool, "folders", "folder", item.Name, "")
	if err != nil {
		return zero, err
	}
	now := time.Now().UTC()
	item.ID = newID()
	item.Name = name
	item.CreatedAt = now
	item.UpdatedAt = now
	ctx, cancel := dbCtx()
	defer cancel()
	_, err = s.pool.Exec(ctx, `INSERT INTO folders (`+folderCols+`) VALUES ($1,$2,$3,$4,$5)`,
		item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item.Path)
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a folder named %q already exists", name)
		}
		return zero, err
	}
	return item, nil
}

func (s *folderStore) Update(id string, item Folder) (Folder, error) {
	var zero Folder
	prev, ok := s.Get(id)
	if !ok {
		return zero, notFoundf("folder not found")
	}
	name, err := requireUniqueName(s.pool, "folders", "folder", item.Name, id)
	if err != nil {
		return zero, err
	}
	item.ID = id
	item.Name = name
	item.CreatedAt = prev.CreatedAt
	item.UpdatedAt = time.Now().UTC()
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.pool.Exec(ctx, `UPDATE folders SET name=$2, updated_at=$3, path=$4 WHERE id=$1`,
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

func (s *folderStore) Delete(id string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.pool.Exec(ctx, `DELETE FROM folders WHERE id = $1`, id)
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

// ---- jobs ------------------------------------------------------------------

type jobStore struct {
	pool *pgxpool.Pool
}

func newJobStore(pool *pgxpool.Pool) *jobStore { return &jobStore{pool: pool} }

const jobCols = `id, name, created_at, updated_at, folder_id, repository_id, schedule`

func scanJob(row interface{ Scan(dest ...any) error }) (Job, error) {
	var j Job
	var sched []byte
	err := row.Scan(&j.ID, &j.Name, &j.CreatedAt, &j.UpdatedAt, &j.FolderID, &j.RepositoryID, &sched)
	if err != nil {
		return Job{}, err
	}
	if len(sched) > 0 {
		var s JobSchedule
		if json.Unmarshal(sched, &s) == nil {
			j.Schedule = &s
		}
	}
	return j, nil
}

func (s *jobStore) List() []Job {
	ctx, cancel := dbCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT `+jobCols+` FROM jobs ORDER BY LOWER(name), id`)
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

func (s *jobStore) Get(id string) (Job, bool) {
	ctx, cancel := dbCtx()
	defer cancel()
	j, err := scanJob(s.pool.QueryRow(ctx, `SELECT `+jobCols+` FROM jobs WHERE id = $1`, id))
	if err != nil {
		return Job{}, false
	}
	return j, true
}

func (s *jobStore) Exists(id string) bool {
	_, ok := s.Get(id)
	return ok
}

func (s *jobStore) Count() int {
	return tableCount(s.pool, "jobs")
}

func (s *jobStore) Create(item Job) (Job, error) {
	var zero Job
	name, err := requireUniqueName(s.pool, "jobs", "job", item.Name, "")
	if err != nil {
		return zero, err
	}
	now := time.Now().UTC()
	item.ID = newID()
	item.Name = name
	item.CreatedAt = now
	item.UpdatedAt = now
	ctx, cancel := dbCtx()
	defer cancel()
	_, err = s.pool.Exec(ctx, `INSERT INTO jobs (`+jobCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		item.ID, item.Name, item.CreatedAt, item.UpdatedAt, item.FolderID, item.RepositoryID, scheduleJSON(item.Schedule))
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a job named %q already exists", name)
		}
		if isFKViolation(err) {
			return zero, validf("the chosen folder or repository no longer exists")
		}
		return zero, err
	}
	return item, nil
}

func (s *jobStore) Update(id string, item Job) (Job, error) {
	var zero Job
	prev, ok := s.Get(id)
	if !ok {
		return zero, notFoundf("job not found")
	}
	name, err := requireUniqueName(s.pool, "jobs", "job", item.Name, id)
	if err != nil {
		return zero, err
	}
	item.ID = id
	item.Name = name
	item.CreatedAt = prev.CreatedAt
	item.UpdatedAt = time.Now().UTC()
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.pool.Exec(ctx, `UPDATE jobs SET
		name=$2, updated_at=$3, folder_id=$4, repository_id=$5, schedule=$6 WHERE id=$1`,
		item.ID, item.Name, item.UpdatedAt, item.FolderID, item.RepositoryID, scheduleJSON(item.Schedule))
	if err != nil {
		if isUniqueViolation(err) {
			return zero, conflictf("a job named %q already exists", name)
		}
		if isFKViolation(err) {
			return zero, validf("the chosen folder or repository no longer exists")
		}
		return zero, err
	}
	if tag.RowsAffected() == 0 {
		return zero, notFoundf("job not found")
	}
	return item, nil
}

func (s *jobStore) Delete(id string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	tag, err := s.pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, id)
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

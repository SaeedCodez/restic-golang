package core

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// This file serves Docker/Coolify discovery: which sites and databases run on
// this host, what databases a server holds, and one-step "back this up" job
// creation from a discovered volume or database.

// dockerTimeout bounds discovery calls; the Engine answers over a local socket.
const dockerTimeout = 30 * time.Second

func dockerContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), dockerTimeout)
}

// requireDocker returns false (after writing a friendly error) if the Docker
// Engine cannot be reached.
func (s *Server) requireDocker(w http.ResponseWriter, r *http.Request) bool {
	ctx, cancel := dockerContext(r)
	defer cancel()
	if err := s.app.Docker.Ping(ctx); err != nil {
		errorJSON(w, http.StatusServiceUnavailable, "no_docker", err.Error())
		return false
	}
	return true
}

func (s *Server) handleDockerStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := dockerContext(r)
	defer cancel()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "docker": s.app.Discovery.Status(ctx)})
}

func (s *Server) handleDockerResources(w http.ResponseWriter, r *http.Request) {
	if !s.requireDocker(w, r) {
		return
	}
	ctx, cancel := dockerContext(r)
	defer cancel()
	res, err := s.app.Discovery.Resources(ctx)
	if err != nil {
		errorJSON(w, http.StatusBadGateway, "docker_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "resources": res})
}

// handleDockerContainerDatabases lists the databases inside a database server
// container, by running the engine's client there.
func (s *Server) handleDockerContainerDatabases(w http.ResponseWriter, r *http.Request) {
	if !s.requireDocker(w, r) {
		return
	}
	ctx, cancel := dockerContext(r)
	defer cancel()
	engine, dbs, cont, err := s.app.ListContainerDatabases(ctx, r.PathValue("id"))
	if err != nil {
		writeDockerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "container": cont.Name, "engine": engine, "databases": dbs,
		"defaultDatabase": defaultDatabaseOf(engine, cont.Env),
	})
}

// handleDockerContainersUsing lists the running containers that use a local
// path, i.e. what a restore with stopContainers would stop.
func (s *Server) handleDockerContainersUsing(w http.ResponseWriter, r *http.Request) {
	if !s.requireDocker(w, r) {
		return
	}
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" || !filepath.IsAbs(p) {
		errorJSON(w, http.StatusBadRequest, "bad_request", "an absolute path is required")
		return
	}
	ctx, cancel := dockerContext(r)
	defer cancel()
	conts, err := s.app.Discovery.ContainersUsingPath(ctx, p)
	if err != nil {
		errorJSON(w, http.StatusBadGateway, "docker_error", err.Error())
		return
	}
	out := make([]ResourceContainer, 0, len(conts))
	for _, c := range conts {
		out = append(out, ResourceContainer{ID: c.ID, Name: c.Name, Service: containerService(c), Image: c.Image, State: c.State})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "containers": out})
}

// handleDockerJobCreate creates a backup job straight from discovery: for a
// volume, a folder of its path (reusing one that already exists); for a
// database, a database source on its container. Either way, then the job.
func (s *Server) handleDockerJobCreate(w http.ResponseWriter, r *http.Request) {
	var req QuickJobRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Kind == SourceDatabase && !s.requireDocker(w, r) {
		return
	}
	ctx, cancel := dockerContext(r)
	defer cancel()
	job, err := s.app.CreateQuickJob(ctx, req)
	if err != nil {
		writeDockerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": s.viewOf(job)})
}

// writeDockerError maps store errors as usual and anything else (the Engine or
// a database command failing) to 502.
func writeDockerError(w http.ResponseWriter, err error) {
	switch err.(type) {
	case *ConflictError, *NotFoundError, *ValidationError:
		writeStoreError(w, err)
	default:
		errorJSON(w, http.StatusBadGateway, "docker_error", err.Error())
	}
}

// ---- App operations shared by the HTTP API and the CLI -------------------------

// ListContainerDatabases runs the engine's client inside a database container
// and returns the databases it holds (system databases left out).
func (a *App) ListContainerDatabases(ctx context.Context, container string) (string, []string, Container, error) {
	cont, err := a.Docker.Inspect(ctx, container)
	if err != nil {
		if isDockerNotFound(err) {
			return "", nil, Container{}, notFoundf("container %q not found", container)
		}
		return "", nil, Container{}, err
	}
	engine := detectEngine(cont.Image, cont.Labels)
	if engine == "" {
		return "", nil, cont, validf("%s", errNoDatabaseEngine.Error())
	}
	if !cont.Running() {
		return engine, nil, cont, validf("the container %q is not running", cont.Name)
	}
	cmd, err := listDatabasesCmd(engine)
	if err != nil {
		return engine, nil, cont, err
	}
	var out, errOut strings.Builder
	code, err := a.Docker.Exec(ctx, cont.ID, cmd, nil, &out, &errOut)
	if err != nil {
		return engine, nil, cont, err
	}
	if code != 0 {
		return engine, nil, cont, validf("listing databases failed (exit code %d): %s", code, lastLine(errOut.String()))
	}
	dbs := parseDatabaseList(engine, out.String())
	if dbs == nil {
		dbs = []string{}
	}
	return engine, dbs, cont, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// QuickJobRequest is a one-step "back this up" from discovery.
type QuickJobRequest struct {
	Kind string `json:"kind"` // folder | database
	// Path is the folder to back up (kind "folder").
	Path string `json:"path"`
	// Container and Database pick the database (kind "database").
	Container string `json:"container"`
	Database  string `json:"database"`
	// Name is the job's name; SourceName the folder's or database source's
	// (defaults to Name).
	Name         string        `json:"name"`
	SourceName   string        `json:"sourceName"`
	RepositoryID string        `json:"repositoryId"`
	Schedule     *JobSchedule  `json:"schedule,omitempty"`
	Retention    *JobRetention `json:"retention,omitempty"`
}

// CreateQuickJob creates (or reuses) the source for a discovered volume or
// database and a job for it. A source created here is removed again if the job
// cannot be created, so a failed attempt leaves nothing behind.
func (a *App) CreateQuickJob(ctx context.Context, req QuickJobRequest) (Job, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return Job{}, validf("a job name is required")
	}
	if !a.Repos.Exists(req.RepositoryID) {
		return Job{}, validf("please choose a storage repository")
	}
	sourceName := strings.TrimSpace(req.SourceName)
	if sourceName == "" {
		sourceName = name
	}
	job := Job{Meta: Meta{Name: name}, RepositoryID: req.RepositoryID, Schedule: req.Schedule, Retention: req.Retention}
	var undo func()

	switch req.Kind {
	case SourceFolder:
		p := filepath.Clean(strings.TrimSpace(req.Path))
		if p == "." || !filepath.IsAbs(p) {
			return Job{}, validf("an absolute folder path is required")
		}
		for _, f := range a.Folders.List() {
			if filepath.Clean(f.Path) == p {
				job.FolderID = f.ID
				break
			}
		}
		if job.FolderID == "" {
			f, err := a.Folders.Create(Folder{Meta: Meta{Name: sourceName}, Path: p})
			if err != nil {
				return Job{}, err
			}
			job.FolderID = f.ID
			undo = func() { _ = a.Folders.Delete(f.ID) }
		}
	case SourceDatabase:
		src, existing, err := a.DatabaseFromContainer(ctx, req.Container, req.Database, sourceName)
		if err != nil {
			return Job{}, err
		}
		if existing {
			job.DatabaseID = src.ID
			break
		}
		created, err := a.Databases.Create(src)
		if err != nil {
			return Job{}, err
		}
		job.DatabaseID = created.ID
		undo = func() { _ = a.Databases.Delete(created.ID) }
	default:
		return Job{}, validf(`kind must be "folder" or "database"`)
	}

	if err := a.ValidateJobRefs(&job); err != nil {
		if undo != nil {
			undo()
		}
		return Job{}, validf("%s", err.Error())
	}
	created, err := a.Jobs.Create(job)
	if err != nil {
		if undo != nil {
			undo()
		}
		return Job{}, err
	}
	return created, nil
}

// DatabaseFromContainer describes database `database` in a container as a
// Database source: the engine and owning resource are read from the container.
// If a source for that database already exists it is returned with existing
// set; otherwise the new source (named name) is returned unsaved.
func (a *App) DatabaseFromContainer(ctx context.Context, container, database, name string) (Database, bool, error) {
	database = strings.TrimSpace(database)
	if err := validateDBName(database); err != nil {
		return Database{}, false, validf("%s", err.Error())
	}
	cont, err := a.Docker.Inspect(ctx, strings.TrimSpace(container))
	if err != nil {
		if isDockerNotFound(err) {
			return Database{}, false, notFoundf("container %q not found", container)
		}
		return Database{}, false, err
	}
	engine := detectEngine(cont.Image, cont.Labels)
	if engine == "" {
		return Database{}, false, validf("%s", errNoDatabaseEngine.Error())
	}
	kind, uuid := containerOwner(cont)
	src := Database{
		Meta: Meta{Name: strings.TrimSpace(name)}, Engine: engine, Container: cont.Name,
		OwnerKind: kind, OwnerUUID: uuid, OwnerService: containerService(cont),
		DatabaseName: database,
	}
	dc := discoveredContainer{Container: cont, kind: kind, uuid: uuid, service: src.OwnerService}
	for _, s := range a.Databases.List() {
		if s.DatabaseName == database && databaseMatchesContainer(s, dc) {
			return s, true, nil
		}
	}
	if err := src.Validate(); err != nil {
		return Database{}, false, validf("%s", err.Error())
	}
	return src, false, nil
}

// SnapshotTree lists a snapshot's files once and keeps the tree for browsing.
func (a *App) SnapshotTree(ctx context.Context, repo *Repository, snapshotID string) (*snapshotTree, error) {
	if snapshotID == "" {
		return nil, validf("please choose a snapshot")
	}
	key := snapshotTreeKey(repo.ID, snapshotID)
	if t, ok := a.Snapshots.Get(key); ok {
		return t, nil
	}
	nodes, err := a.Runner.List(ctx, repo, snapshotID)
	if err != nil {
		return nil, err
	}
	t := buildSnapshotTree(nodes)
	a.Snapshots.Put(key, t)
	return t, nil
}

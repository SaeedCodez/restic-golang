package core

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// This file turns the Docker host's containers into the sites and databases the
// UI offers for backup. Coolify labels every container it runs with the owning
// resource (application, service or standalone database), its project and its
// environment, so containers group into the resources the user knows from the
// Coolify dashboard. Containers Coolify did not start group by compose project.

// Resource kinds, as Coolify names them, plus plain Docker fallbacks.
const (
	ownerApplication = "application"
	ownerService     = "service"
	ownerDatabase    = "database"
	ownerCompose     = "compose"
	ownerContainer   = "container"
)

// DockerStatus says whether discovery and database backups can work.
type DockerStatus struct {
	Available bool   `json:"available"`
	Coolify   bool   `json:"coolify"`
	Host      string `json:"host,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Resource is one Coolify resource (or compose project) on the Docker host.
type Resource struct {
	Key         string `json:"key"` // kind:uuid, stable across redeploys
	Kind        string `json:"kind"`
	UUID        string `json:"uuid,omitempty"`
	Name        string `json:"name"`
	Project     string `json:"project,omitempty"`
	Environment string `json:"environment,omitempty"`
	// Self marks the resource this app itself runs in.
	Self       bool                `json:"self,omitempty"`
	Domains    []string            `json:"domains"`
	Containers []ResourceContainer `json:"containers"`
	Volumes    []ResourceVolume    `json:"volumes"`
	Databases  []ResourceDatabase  `json:"databases"`
	// LinkedDatabases are databases this resource's apps are configured to use,
	// often on a shared database server in another resource.
	LinkedDatabases []LinkedDatabase `json:"linkedDatabases"`
}

// ResourceContainer is one container of a resource.
type ResourceContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Service string `json:"service,omitempty"`
	Image   string `json:"image"`
	State   string `json:"state"`
}

// ResourceVolume is a volume or bind mount the resource's containers use.
type ResourceVolume struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // volume | bind
	HostPath string `json:"hostPath"`
	// Path is where this app reaches the data; "" means it is not mounted here.
	Path       string   `json:"path,omitempty"`
	MountedAt  []string `json:"mountedAt"`
	Containers []string `json:"containers"`
	// DatabaseData marks a database's data directory: copying it while the
	// server runs is not a consistent backup, so a dump is offered instead.
	DatabaseData bool     `json:"databaseData,omitempty"`
	FolderID     string   `json:"folderId,omitempty"`
	Jobs         []JobRef `json:"jobs,omitempty"`
}

// ResourceDatabase is a database server container the app can dump.
type ResourceDatabase struct {
	Container   string `json:"container"`
	ContainerID string `json:"containerId"`
	Engine      string `json:"engine"`
	Image       string `json:"image"`
	State       string `json:"state"`
	// DefaultDatabase is the database the container was created with.
	DefaultDatabase string `json:"defaultDatabase,omitempty"`
	OwnerKind       string `json:"ownerKind,omitempty"`
	OwnerUUID       string `json:"ownerUuid,omitempty"`
	OwnerService    string `json:"ownerService,omitempty"`
	// Sources are the Database sources already defined on this container.
	Sources []DatabaseRef `json:"sources,omitempty"`
}

// LinkedDatabase is a database an app's environment points at.
type LinkedDatabase struct {
	Host     string `json:"host"`
	Database string `json:"database"`
	// Container is the database container the host resolved to; "" if unknown.
	Container string `json:"container,omitempty"`
	Engine    string `json:"engine,omitempty"`
	Resource  string `json:"resource,omitempty"` // key of the resource that runs it
}

// JobRef names a job, for "already backed up by" hints.
type JobRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DatabaseRef is an existing Database source and the jobs that use it.
type DatabaseRef struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Database string   `json:"database"`
	Jobs     []JobRef `json:"jobs,omitempty"`
}

// containerOwner identifies the resource that owns a container, the way Coolify
// itself does: the coolify.<kind>Uuid label, or for containers from before that
// label the compose project (which Coolify sets to the resource uuid). Plain
// compose projects and lone containers are their own fallback.
func containerOwner(c Container) (kind, uuid string) {
	l := c.Labels
	for _, k := range []string{ownerService, ownerApplication, ownerDatabase} {
		if u := l["coolify."+k+"Uuid"]; u != "" {
			return k, u
		}
	}
	project := l["com.docker.compose.project"]
	if k := l["coolify.type"]; k == ownerService || k == ownerApplication || k == ownerDatabase {
		if project != "" {
			return k, project
		}
		return k, c.Name
	}
	if project != "" {
		return ownerCompose, project
	}
	return ownerContainer, c.Name
}

// containerService is the container's part within its resource: the compose
// service, or Coolify's sub-service name.
func containerService(c Container) string {
	if s := c.Labels["com.docker.compose.service"]; s != "" {
		return s
	}
	return c.Labels["coolify.service.subName"]
}

func isCoolifyManaged(c Container) bool { return c.Labels["coolify.managed"] == "true" }

// hostRulePattern matches the hosts in a Traefik rule such as
// Host(`a.example.com`) || Host(`b.example.com`, `c.example.com`).
var (
	hostRulePattern = regexp.MustCompile("Host\\(([^)]*)\\)")
	backtickPattern = regexp.MustCompile("`([^`]+)`")
)

// containerDomains reads the public hostnames from proxy labels (Traefik routers
// or Caddy), which is how a site is recognised in a list.
func containerDomains(c Container) []string {
	var out []string
	for k, v := range c.Labels {
		switch {
		case strings.HasPrefix(k, "traefik.http.routers.") && strings.HasSuffix(k, ".rule"):
			for _, m := range hostRulePattern.FindAllStringSubmatch(v, -1) {
				for _, h := range backtickPattern.FindAllStringSubmatch(m[1], -1) {
					out = append(out, strings.ToLower(h[1]))
				}
			}
		case strings.HasPrefix(k, "caddy_") && !strings.Contains(k, "."):
			for _, f := range strings.Fields(strings.ReplaceAll(v, ",", " ")) {
				if u, err := url.Parse(f); err == nil && u.Host != "" {
					out = append(out, strings.ToLower(u.Hostname()))
				}
			}
		}
	}
	return out
}

// ---- path mapping -----------------------------------------------------------

// pathMap translates Docker host paths to where this app sees them, from this
// app's own mounts: with /var/lib/docker/volumes mounted at /docker-volumes, a
// volume at /var/lib/docker/volumes/x/_data is backed up as /docker-volumes/x/_data.
type pathMap []pathMapping

type pathMapping struct{ Host, Local string }

// defaultPathMap is used when this app cannot inspect its own container; it is
// the mount the shipped docker-compose.yaml sets up.
func defaultPathMap() pathMap {
	if fi, err := os.Stat("/docker-volumes"); err == nil && fi.IsDir() {
		return pathMap{{Host: "/var/lib/docker/volumes", Local: "/docker-volumes"}}
	}
	return nil
}

// pathMapFromMounts builds the map from a container's own mounts.
func pathMapFromMounts(mounts []Mount) pathMap {
	var m pathMap
	for _, mt := range mounts {
		if (mt.Type == "bind" || mt.Type == "volume") && mt.Source != "" && mt.Destination != "" {
			m = append(m, pathMapping{Host: filepath.Clean(mt.Source), Local: filepath.Clean(mt.Destination)})
		}
	}
	return m
}

// toLocal returns the local path for a host path, using the most specific
// mount that contains it.
func (m pathMap) toLocal(hostPath string) (string, bool) {
	hostPath = filepath.Clean(hostPath)
	best, bestLen := "", -1
	for _, mp := range m {
		if rel, ok := pathWithin(hostPath, mp.Host); ok && len(mp.Host) > bestLen {
			best, bestLen = filepath.Join(mp.Local, rel), len(mp.Host)
		}
	}
	return best, bestLen >= 0
}

// toHost is the reverse of toLocal: where on the Docker host a local path lives.
func (m pathMap) toHost(localPath string) (string, bool) {
	localPath = filepath.Clean(localPath)
	best, bestLen := "", -1
	for _, mp := range m {
		if rel, ok := pathWithin(localPath, mp.Local); ok && len(mp.Local) > bestLen {
			best, bestLen = filepath.Join(mp.Host, rel), len(mp.Local)
		}
	}
	return best, bestLen >= 0
}

// pathWithin reports whether p is base or inside it, and the relative rest.
func pathWithin(p, base string) (string, bool) {
	if base == "/" {
		return strings.TrimPrefix(p, "/"), true
	}
	if p == base {
		return "", true
	}
	if strings.HasPrefix(p, base+"/") {
		return p[len(base)+1:], true
	}
	return "", false
}

// ---- linked databases --------------------------------------------------------

// dbEnvPairs are host/name variable pairs common apps read their database from.
var dbEnvPairs = [][2]string{
	{"WORDPRESS_DB_HOST", "WORDPRESS_DB_NAME"},
	{"DB_HOST", "DB_DATABASE"},
	{"DB_HOST", "DB_NAME"},
	{"DATABASE_HOST", "DATABASE_NAME"},
	{"MYSQL_HOST", "MYSQL_DATABASE"},
	{"MARIADB_HOST", "MARIADB_DATABASE"},
	{"POSTGRES_HOST", "POSTGRES_DB"},
	{"PGHOST", "PGDATABASE"},
}

// dbURLVars hold connection URLs (postgres://user:pass@host:5432/db).
var dbURLVars = []string{"DATABASE_URL", "DB_URL", "POSTGRES_URL", "POSTGRESQL_URL", "MYSQL_URL", "MARIADB_URL"}

// linkedDatabasesFromEnv finds the database host and name an app container is
// configured with. Only hosts and names are read; credentials never leave here.
func linkedDatabasesFromEnv(env []string) []LinkedDatabase {
	var out []LinkedDatabase
	seen := map[string]bool{}
	add := func(host, db string) {
		host = strings.TrimSpace(host)
		db = strings.TrimSpace(db)
		if h, _, ok := strings.Cut(host, ":"); ok {
			host = h
		}
		if host == "" || db == "" || !dbNamePattern.MatchString(db) {
			return
		}
		key := host + "/" + db
		if !seen[key] {
			seen[key] = true
			out = append(out, LinkedDatabase{Host: host, Database: db})
		}
	}
	for _, p := range dbEnvPairs {
		add(envValue(env, p[0]), envValue(env, p[1]))
	}
	for _, k := range dbURLVars {
		raw := envValue(env, k)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			continue
		}
		add(u.Hostname(), strings.TrimPrefix(u.Path, "/"))
	}
	return out
}

// ---- discovery ----------------------------------------------------------------

// discoveredContainer is an inspected container with its owner worked out.
type discoveredContainer struct {
	Container
	kind, uuid, service string
}

// Discovery inspects the Docker host. It is cheap enough to run per request
// (one list plus one inspect per container over a local socket).
type Discovery struct {
	app *App
	// selfID finds this app's own container id; tests replace it.
	selfID func() string
}

// Status reports whether Docker is reachable and whether Coolify manages it.
func (d *Discovery) Status(ctx context.Context) DockerStatus {
	st := DockerStatus{}
	if h, ok := d.app.Docker.(interface{ DockerHost() string }); ok {
		st.Host = h.DockerHost()
	}
	if err := d.app.Docker.Ping(ctx); err != nil {
		st.Message = err.Error()
		return st
	}
	st.Available = true
	list, err := d.app.Docker.Containers(ctx)
	if err != nil {
		st.Message = err.Error()
		return st
	}
	for _, c := range list {
		if isCoolifyManaged(c) {
			st.Coolify = true
			break
		}
	}
	return st
}

// inspectAll lists and inspects every container (inspect adds the env and
// network aliases discovery needs), a few at a time.
func (d *Discovery) inspectAll(ctx context.Context) ([]discoveredContainer, error) {
	list, err := d.app.Docker.Containers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]discoveredContainer, len(list))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, c := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			full, err := d.app.Docker.Inspect(ctx, c.ID)
			if err != nil {
				full = c // gone in between, or not inspectable: keep the list view
			}
			if full.Labels == nil {
				full.Labels = c.Labels
			}
			kind, uuid := containerOwner(full)
			out[i] = discoveredContainer{Container: full, kind: kind, uuid: uuid, service: containerService(full)}
		}()
	}
	wg.Wait()
	return out, nil
}

// pathMap works out how this app sees host paths: from its own container's
// mounts when it can find itself, else the compose default.
func (d *Discovery) pathMap(ctx context.Context, all []discoveredContainer) (pathMap, string) {
	find := d.selfID
	if find == nil {
		find = selfContainerID
	}
	if id := find(); id != "" {
		for _, c := range all {
			if strings.HasPrefix(c.ID, id) {
				return pathMapFromMounts(c.Mounts), c.ID
			}
		}
		if c, err := d.app.Docker.Inspect(ctx, id); err == nil {
			return pathMapFromMounts(c.Mounts), c.ID
		}
	}
	return defaultPathMap(), ""
}

// Resources returns every resource on the Docker host with its volumes and
// databases, annotated with the folders, database sources and jobs that already
// back them up.
func (d *Discovery) Resources(ctx context.Context) ([]Resource, error) {
	all, err := d.inspectAll(ctx)
	if err != nil {
		return nil, err
	}
	pm, selfID := d.pathMap(ctx, all)

	folderByPath := map[string]Folder{}
	for _, f := range d.app.Folders.List() {
		folderByPath[filepath.Clean(f.Path)] = f
	}
	jobsByFolder := map[string][]JobRef{}
	jobsByDatabase := map[string][]JobRef{}
	for _, j := range d.app.Jobs.List() {
		ref := JobRef{ID: j.ID, Name: j.Name}
		if j.FolderID != "" {
			jobsByFolder[j.FolderID] = append(jobsByFolder[j.FolderID], ref)
		}
		if j.DatabaseID != "" {
			jobsByDatabase[j.DatabaseID] = append(jobsByDatabase[j.DatabaseID], ref)
		}
	}
	sources := d.app.Databases.List()

	byKey := map[string]*Resource{}
	var order []string
	for _, c := range all {
		key := c.kind + ":" + c.uuid
		r := byKey[key]
		if r == nil {
			r = &Resource{Key: key, Kind: c.kind, Name: c.uuid}
			if c.kind != ownerCompose && c.kind != ownerContainer {
				r.UUID = c.uuid
			}
			byKey[key] = r
			order = append(order, key)
		}
		if n := c.Labels["coolify.resourceName"]; n != "" && r.Name == c.uuid {
			r.Name = n
		}
		if r.Project == "" {
			r.Project = c.Labels["coolify.projectName"]
		}
		if r.Environment == "" {
			r.Environment = c.Labels["coolify.environmentName"]
		}
		if selfID != "" && c.ID == selfID {
			r.Self = true
		}
		r.Domains = append(r.Domains, containerDomains(c.Container)...)
		r.Containers = append(r.Containers, ResourceContainer{ID: c.ID, Name: c.Name, Service: c.service, Image: c.Image, State: c.State})
	}

	for _, key := range order {
		r := byKey[key]
		r.Domains = uniqueSorted(r.Domains)
	}

	// Volumes, database servers and linked databases, per resource.
	for _, c := range all {
		r := byKey[c.kind+":"+c.uuid]
		engine := detectEngine(c.Image, c.Labels)
		for _, m := range c.Mounts {
			if m.Type != "volume" && m.Type != "bind" {
				continue
			}
			// This app's own bind mounts are how it reaches other sites' data,
			// not data of its own.
			if m.Type == "bind" && (skipBindMount(m) || (selfID != "" && c.ID == selfID)) {
				continue
			}
			addVolume(r, c, m, engine != "", pm, folderByPath, jobsByFolder)
		}
		if engine != "" {
			db := ResourceDatabase{
				Container: c.Name, ContainerID: c.ID, Engine: engine, Image: c.Image, State: c.State,
				DefaultDatabase: defaultDatabaseOf(engine, c.Env),
				OwnerKind:       c.kind, OwnerUUID: c.uuid, OwnerService: c.service,
			}
			for _, s := range sources {
				if databaseMatchesContainer(s, c) {
					db.Sources = append(db.Sources, DatabaseRef{ID: s.ID, Name: s.Name, Database: s.DatabaseName, Jobs: jobsByDatabase[s.ID]})
				}
			}
			r.Databases = append(r.Databases, db)
		} else {
			r.LinkedDatabases = append(r.LinkedDatabases, linkedDatabasesFromEnv(c.Env)...)
		}
	}

	// Resolve linked database hosts to containers: a container name, a network
	// alias, or a compose service in the same project.
	for _, key := range order {
		r := byKey[key]
		var resolved []LinkedDatabase
		seen := map[string]bool{}
		for _, ld := range r.LinkedDatabases {
			for _, c := range all {
				engine := detectEngine(c.Image, c.Labels)
				if engine == "" {
					continue
				}
				if c.Name == ld.Host || containsString(c.Aliases, ld.Host) ||
					(c.kind+":"+c.uuid == key && c.service == ld.Host) {
					ld.Container, ld.Engine, ld.Resource = c.Name, engine, c.kind+":"+c.uuid
					break
				}
			}
			k := ld.Host + "/" + ld.Database
			if !seen[k] {
				seen[k] = true
				resolved = append(resolved, ld)
			}
		}
		r.LinkedDatabases = resolved
	}

	out := make([]Resource, 0, len(order))
	for _, key := range order {
		r := byKey[key]
		sort.Slice(r.Containers, func(i, j int) bool { return r.Containers[i].Name < r.Containers[j].Name })
		sort.Slice(r.Volumes, func(i, j int) bool { return r.Volumes[i].Name < r.Volumes[j].Name })
		if r.Domains == nil {
			r.Domains = []string{}
		}
		if r.Volumes == nil {
			r.Volumes = []ResourceVolume{}
		}
		if r.Databases == nil {
			r.Databases = []ResourceDatabase{}
		}
		if r.LinkedDatabases == nil {
			r.LinkedDatabases = []LinkedDatabase{}
		}
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ac, bc := a.Kind != ownerCompose && a.Kind != ownerContainer, b.Kind != ownerCompose && b.Kind != ownerContainer; ac != bc {
			return ac
		}
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		return a.Name < b.Name
	})
	return out, nil
}

// skipBindMount drops host plumbing that is not site data: the Docker socket
// and storage, single config files Coolify writes, and system paths.
func skipBindMount(m Mount) bool {
	src := filepath.Clean(m.Source)
	switch {
	case strings.HasSuffix(src, ".sock"):
		return true
	case src == "/" || strings.HasPrefix(src, "/proc") || strings.HasPrefix(src, "/sys") ||
		strings.HasPrefix(src, "/etc") || strings.HasPrefix(src, "/dev") || strings.HasPrefix(src, "/run"):
		return true
	case src == "/var/lib/docker" || strings.HasPrefix(src, "/var/lib/docker/"):
		// Docker's own storage (e.g. this app's mount of every volume).
		return true
	}
	// Coolify file storages are config files next to the compose file, named
	// with an extension; data directories are not.
	return filepath.Ext(src) != "" && strings.HasPrefix(src, "/data/coolify/")
}

func addVolume(r *Resource, c discoveredContainer, m Mount, dbContainer bool, pm pathMap,
	folderByPath map[string]Folder, jobsByFolder map[string][]JobRef) {
	name := m.Name
	if m.Type == "bind" || name == "" {
		name = m.Source
	}
	for i := range r.Volumes {
		v := &r.Volumes[i]
		if v.HostPath == m.Source {
			v.MountedAt = appendUnique(v.MountedAt, m.Destination)
			v.Containers = appendUnique(v.Containers, c.Name)
			v.DatabaseData = v.DatabaseData || dbContainer
			return
		}
	}
	v := ResourceVolume{
		Name: name, Type: m.Type, HostPath: m.Source,
		MountedAt: []string{m.Destination}, Containers: []string{c.Name}, DatabaseData: dbContainer,
	}
	if local, ok := pm.toLocal(m.Source); ok {
		if fi, err := os.Stat(local); err == nil && !fi.IsDir() {
			return // a single mounted file (config), not site data
		}
		v.Path = local
		if f, ok := folderByPath[filepath.Clean(local)]; ok {
			v.FolderID = f.ID
			v.Jobs = jobsByFolder[f.ID]
		}
	}
	r.Volumes = append(r.Volumes, v)
}

// databaseMatchesContainer reports whether a Database source points at c.
func databaseMatchesContainer(s Database, c discoveredContainer) bool {
	if s.OwnerUUID != "" {
		return s.OwnerKind == c.kind && s.OwnerUUID == c.uuid && (s.OwnerService == "" || s.OwnerService == c.service)
	}
	return s.Container == c.Name
}

// ResolveDatabaseContainer finds the container a Database source runs in now.
// Containers are matched by their owning resource first (Coolify can recreate a
// container under a new name on redeploy), then by the stored name; a running
// container wins over a stopped one.
func (d *Discovery) ResolveDatabaseContainer(ctx context.Context, s Database) (Container, error) {
	list, err := d.app.Docker.Containers(ctx)
	if err != nil {
		return Container{}, err
	}
	// Only a container running the source's engine is a candidate: a resource
	// with several parts must not resolve to, say, its web container.
	var byOwner, byName []Container
	for _, c := range list {
		if detectEngine(c.Image, c.Labels) != s.Engine {
			continue
		}
		kind, uuid := containerOwner(c)
		dc := discoveredContainer{Container: c, kind: kind, uuid: uuid, service: containerService(c)}
		switch {
		case s.OwnerUUID != "" && databaseMatchesContainer(s, dc):
			byOwner = append(byOwner, c)
		case c.Name == s.Container:
			byName = append(byName, c)
		}
	}
	candidates := byOwner
	if len(candidates) == 0 {
		candidates = byName
	}
	var running []Container
	for _, c := range candidates {
		if c.Running() {
			running = append(running, c)
		}
	}
	switch {
	case len(candidates) == 0:
		return Container{}, validf("the %s container %q was not found on this Docker host", engineLabel(s.Engine), s.Container)
	case len(running) == 0:
		return Container{}, validf("the database container %q is not running (%s)", candidates[0].Name, candidates[0].State)
	case len(running) > 1:
		names := make([]string, len(running))
		for i, c := range running {
			names[i] = c.Name
		}
		return Container{}, validf("%d running %s containers match this database source (%s); edit the source to name one", len(running), engineLabel(s.Engine), strings.Join(names, ", "))
	}
	return d.app.Docker.Inspect(ctx, running[0].ID)
}

// broadHostDirs are host directories a container may mount whole (monitoring
// agents, Coolify itself) without being a user of every site beneath them.
var broadHostDirs = map[string]bool{
	"/": true, "/data": true, "/data/coolify": true, "/data/coolify/applications": true,
	"/data/coolify/services": true, "/data/coolify/databases": true, "/var": true, "/var/lib": true,
	"/home": true, "/root": true, "/srv": true, "/opt": true, "/mnt": true,
}

// isCoolifyInfra reports whether a container is part of Coolify itself (its
// app, proxy, database, realtime or sentinel), which a restore must never stop.
func isCoolifyInfra(c Container) bool {
	if c.Labels["coolify.proxy"] == "true" {
		return true
	}
	owned := false
	for _, k := range []string{ownerService, ownerApplication, ownerDatabase} {
		if c.Labels["coolify."+k+"Uuid"] != "" || c.Labels["coolify."+k+"Id"] != "" {
			owned = true
		}
	}
	return !owned && (c.Name == "coolify" || strings.HasPrefix(c.Name, "coolify-"))
}

// ContainersUsingPath returns the running containers that write to the host
// location behind a local path (e.g. the site containers of a volume), so a
// restore into it can stop them first. Never included: this app's own stack,
// Coolify's own containers, read-only mounts, and containers that only mount
// a broad parent directory. Without knowing its own container the app cannot
// tell which containers are its own, so it refuses.
func (d *Discovery) ContainersUsingPath(ctx context.Context, localPath string) ([]Container, error) {
	all, err := d.inspectAll(ctx)
	if err != nil {
		return nil, err
	}
	pm, selfID := d.pathMap(ctx, all)
	if selfID == "" {
		return nil, errors.New("this app could not identify its own container, so it will not stop containers for a restore")
	}
	hostPath, ok := pm.toHost(localPath)
	if !ok {
		return nil, nil
	}
	selfKey := ""
	for _, c := range all {
		if c.ID == selfID {
			selfKey = c.kind + ":" + c.uuid
		}
	}
	var out []Container
	for _, c := range all {
		if !c.Running() || c.kind+":"+c.uuid == selfKey || isCoolifyInfra(c.Container) {
			continue
		}
		for _, m := range c.Mounts {
			if (m.Type != "volume" && m.Type != "bind") || !m.RW {
				continue
			}
			src := filepath.Clean(m.Source)
			_, over := pathWithin(src, hostPath)
			_, under := pathWithin(hostPath, src)
			// A named volume's source is its own data directory, so a container
			// mounting it uses every path below; a bind of a broad parent does not.
			if over || (under && (m.Type == "volume" || !broadHostDirs[src] && !pathIsDockerStorage(src))) {
				out = append(out, c.Container)
				break
			}
		}
	}
	return out, nil
}

func pathIsDockerStorage(p string) bool {
	return p == "/var/lib/docker" || strings.HasPrefix(p, "/var/lib/docker/")
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func appendUnique(list []string, s string) []string {
	if containsString(list, s) {
		return list
	}
	return append(list, s)
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

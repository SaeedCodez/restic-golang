package core

import (
	"net/http"
	"strings"
	"testing"
)

func TestQuickJobFromDiscoveredVolumeAndDatabase(t *testing.T) {
	app, fd := discoveryApp(t)
	fd.execFn = func(c Container, cmd []string, stdin string) (string, string, int) {
		if dbCmdKind(cmd) == "list" {
			return "information_schema\nmysql\nshop\nwordpress_test\n", "", 0
		}
		return "", "", 0
	}
	h := routesFor(t, app)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})

	var dbs struct {
		Engine          string   `json:"engine"`
		Databases       []string `json:"databases"`
		DefaultDatabase string   `json:"defaultDatabase"`
	}
	if code := doJSON(t, h, "GET", "/api/docker/containers/mdb1/databases", nil, &dbs); code != http.StatusOK {
		t.Fatalf("databases: %d", code)
	}
	if dbs.Engine != EngineMariaDB || strings.Join(dbs.Databases, ",") != "shop,wordpress_test" || dbs.DefaultDatabase != "wordpress_test" {
		t.Fatalf("databases = %+v", dbs)
	}

	var created struct {
		Job jobView `json:"job"`
	}
	code := doJSON(t, h, "POST", "/api/docker/jobs", map[string]any{
		"kind": "folder", "path": "/docker-volumes/svc1_wordpress-files/_data", "name": "shop files", "repositoryId": repo.ID,
		"schedule": map[string]any{"enabled": true, "kind": "daily", "at": "02:00"},
	}, &created)
	if code != http.StatusOK || created.Job.SourceKind != SourceFolder || created.Job.FolderPath != "/docker-volumes/svc1_wordpress-files/_data" {
		t.Fatalf("folder job: %d %+v", code, created.Job)
	}
	if created.Job.Schedule == nil || created.Job.Schedule.At != "02:00" {
		t.Fatalf("schedule lost: %+v", created.Job.Schedule)
	}

	code = doJSON(t, h, "POST", "/api/docker/jobs", map[string]any{
		"kind": "database", "container": "mdb1", "database": "shop", "name": "shop database", "repositoryId": repo.ID,
	}, &created)
	if code != http.StatusOK || created.Job.SourceKind != SourceDatabase || created.Job.Engine != EngineMariaDB ||
		created.Job.Database != "shop" || created.Job.SourceLabel != "mariadb://mdb1/shop" {
		t.Fatalf("database job: %d %+v", code, created.Job)
	}
	src, _ := app.Databases.Get(created.Job.DatabaseID)
	if src.OwnerKind != "database" || src.OwnerUUID != "mdb1" {
		t.Fatalf("source owner not recorded: %+v", src)
	}

	// A second job on the same database reuses its source; a name clash leaves
	// no stray source behind.
	before := app.Databases.Count()
	code = doJSON(t, h, "POST", "/api/docker/jobs", map[string]any{
		"kind": "database", "container": "mdb1", "database": "shop", "name": "shop database 2", "repositoryId": repo.ID,
	}, &created)
	if code != http.StatusOK || app.Databases.Count() != before {
		t.Fatalf("reuse: %d, sources %d -> %d", code, before, app.Databases.Count())
	}
	code = doJSON(t, h, "POST", "/api/docker/jobs", map[string]any{
		"kind": "database", "container": "mdb1", "database": "wordpress_test", "name": "shop database",
		"sourceName": "wp test db", "repositoryId": repo.ID,
	}, nil)
	if code != http.StatusConflict || app.Databases.Count() != before {
		t.Fatalf("clash: %d, sources %d -> %d", code, before, app.Databases.Count())
	}

	// The database source cannot be deleted while jobs use it.
	if code := doJSON(t, h, "DELETE", "/api/databases/"+src.ID, nil, nil); code != http.StatusConflict {
		t.Fatalf("delete in use: %d", code)
	}

	// Not a database container.
	if code := doJSON(t, h, "POST", "/api/docker/jobs", map[string]any{
		"kind": "database", "container": "wordpress-svc1", "database": "shop", "name": "x", "repositoryId": repo.ID,
	}, nil); code != http.StatusBadRequest {
		t.Fatalf("non-database container: %d", code)
	}
}

func TestDockerUnavailableIsExplained(t *testing.T) {
	app := testApp(t, &fakeRunner{installed: true})
	app.Docker = &fakeDocker{pingErr: errNoSocket}
	h := routesFor(t, app)
	var st struct {
		Docker DockerStatus `json:"docker"`
	}
	if code := doJSON(t, h, "GET", "/api/docker/status", nil, &st); code != http.StatusOK || st.Docker.Available || st.Docker.Message == "" {
		t.Fatalf("status: %d %+v", code, st.Docker)
	}
	var e struct {
		Code string `json:"code"`
	}
	if code := doJSON(t, h, "GET", "/api/docker/resources", nil, &e); code != http.StatusServiceUnavailable || e.Code != "no_docker" {
		t.Fatalf("resources: %d %+v", code, e)
	}
}

var errNoSocket = &dockerAPIError{Status: 0, Message: "the Docker socket is not available"}

func TestDatabaseSourceCRUDAndJobValidation(t *testing.T) {
	app := testApp(t, &fakeRunner{installed: true})
	h := routesFor(t, app)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})

	var resp struct {
		Database Database `json:"database"`
	}
	if code := doJSON(t, h, "POST", "/api/databases", map[string]any{
		"name": "app db", "engine": "postgres", "container": "pg1", "database": "app",
	}, &resp); code != http.StatusOK || resp.Database.ID == "" {
		t.Fatalf("create: %d", code)
	}
	if code := doJSON(t, h, "POST", "/api/databases", map[string]any{
		"name": "bad", "engine": "postgres", "container": "pg1", "database": "x'; DROP",
	}, nil); code != http.StatusBadRequest {
		t.Fatalf("unsafe name accepted: %d", code)
	}

	// A job takes exactly one source.
	for _, body := range []map[string]any{
		{"name": "j1", "repositoryId": repo.ID},
		{"name": "j2", "repositoryId": repo.ID, "databaseId": resp.Database.ID, "folderId": "f"},
		{"name": "j3", "repositoryId": repo.ID, "databaseId": "nope"},
	} {
		if code := doJSON(t, h, "POST", "/api/jobs", body, nil); code != http.StatusBadRequest {
			t.Errorf("%v: code %d, want 400", body["name"], code)
		}
	}
	var job struct {
		Job jobView `json:"job"`
	}
	if code := doJSON(t, h, "POST", "/api/jobs", map[string]any{"name": "ok", "repositoryId": repo.ID, "databaseId": resp.Database.ID}, &job); code != http.StatusOK {
		t.Fatalf("job create: %d", code)
	}
	if job.Job.SourceKind != SourceDatabase || job.Job.DatabaseName != "app db" || job.Job.FolderID != "" {
		t.Fatalf("job = %+v", job.Job)
	}
}

func TestSnapshotLsEndpointCachesTheListing(t *testing.T) {
	fake := &fakeRunner{installed: true, lsNodes: []LsNode{
		{Path: "/data", Name: "data", Type: "dir"},
		{Path: "/data/a.txt", Name: "a.txt", Type: "file", Size: 3},
	}}
	app := testApp(t, fake)
	h := routesFor(t, app)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})

	var ls struct {
		Path    string   `json:"path"`
		Entries []LsNode `json:"entries"`
	}
	for i := 0; i < 2; i++ {
		if code := doJSON(t, h, "GET", "/api/repositories/"+repo.ID+"/snapshots/abc/ls?path=/data", nil, &ls); code != http.StatusOK {
			t.Fatalf("ls: %d", code)
		}
	}
	if ls.Path != "/data" || len(ls.Entries) != 1 || ls.Entries[0].Name != "a.txt" {
		t.Fatalf("ls = %+v", ls)
	}
	if fake.lsCalls != 1 {
		t.Fatalf("restic ls ran %d times, want 1", fake.lsCalls)
	}
	if code := doJSON(t, h, "GET", "/api/repositories/"+repo.ID+"/snapshots/abc/ls?path=/nope", nil, nil); code != http.StatusNotFound {
		t.Fatalf("missing folder: %d", code)
	}
}

func TestRestoreDatabaseEndpointStartsRun(t *testing.T) {
	fake := &fakeRunner{installed: true,
		snaps:    []Snapshot{dbSnapshot("aaaaaaaa1111", EngineMariaDB, "shop.sql")},
		dumpData: map[string][]byte{"aaaaaaaa1111 /shop.sql": []byte("x")},
	}
	app, _, repo, db, _ := dbJobApp(t, fake)
	h := routesFor(t, app)
	var resp struct {
		RunID string `json:"runId"`
		Run   Run    `json:"run"`
	}
	code := doJSON(t, h, "POST", "/api/repositories/"+repo.ID+"/restore-db", map[string]any{
		"snapshotId": "aaaaaaaa1111", "databaseId": db.ID, "into": "shop_copy",
	}, &resp)
	if code != http.StatusAccepted || resp.Run.Kind != KindRestoreDB || resp.Run.Params["safetyBackup"] != "true" {
		t.Fatalf("restore-db: %d %+v", code, resp.Run)
	}
}

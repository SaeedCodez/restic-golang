package core

import (
	"context"
	"strings"
	"testing"
)

// coolifyHost is a Docker host laid out the way Coolify runs things: a
// WordPress service whose site uses a shared standalone MariaDB, a service with
// its own MariaDB, a standalone PostgreSQL, a compose project Coolify did not
// start, and this app's own container (which mounts the volumes directory).
func coolifyHost() *fakeDocker {
	vol := func(name, dest string) Mount {
		return Mount{Type: "volume", Name: name, Source: "/var/lib/docker/volumes/" + name + "/_data", Destination: dest, RW: true}
	}
	return &fakeDocker{containers: []Container{
		{
			ID: "self0000000000000000", Name: "app-rw0000", Image: "restic-web", State: "running",
			Labels: map[string]string{"coolify.managed": "true", "coolify.type": "application", "coolify.applicationId": "7", "com.docker.compose.project": "rw0000", "com.docker.compose.service": "app"},
			Mounts: []Mount{
				{Type: "bind", Source: "/var/lib/docker/volumes", Destination: "/docker-volumes", RW: true},
				{Type: "bind", Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock", RW: true},
				{Type: "bind", Source: "/data/coolify/services", Destination: "/data/coolify/services", RW: true},
			},
		},
		{
			ID: "wp00000000000000aaaa", Name: "wordpress-svc1", Image: "wordpress:6.6", State: "running",
			Labels: map[string]string{
				"coolify.managed": "true", "coolify.type": "service", "coolify.serviceUuid": "svc1",
				"coolify.resourceName": "digipharma-wordpress", "coolify.projectName": "digipharma", "coolify.environmentName": "production",
				"com.docker.compose.project": "svc1", "com.docker.compose.service": "wordpress",
				"traefik.http.routers.http-0-svc1.rule":  "Host(`shop.example.com`) && PathPrefix(`/`)",
				"traefik.http.routers.https-0-svc1.rule": "Host(`shop.example.com`) || Host(`www.shop.example.com`)",
			},
			Env: []string{"WORDPRESS_DB_HOST=mdb1:3306", "WORDPRESS_DB_NAME=shop", "WORDPRESS_DB_PASSWORD=sekrit"},
			Mounts: []Mount{
				vol("svc1_wordpress-files", "/var/www/html"),
				{Type: "bind", Source: "/data/coolify/services/svc1/zz-php.ini", Destination: "/usr/local/etc/php/conf.d/zz.ini"},
			},
		},
		{
			ID: "mdb10000000000000000", Name: "mdb1", Image: "mariadb:11", State: "running",
			Labels: map[string]string{
				"coolify.managed": "true", "coolify.type": "database", "coolify.databaseUuid": "mdb1",
				"coolify.resourceName": "shared-mariadb", "coolify.projectName": "shared-database", "coolify.database.subType": "mariadb",
			},
			Env:     []string{"MARIADB_DATABASE=wordpress_test", "MARIADB_ROOT_PASSWORD=rootpw"},
			Aliases: []string{"mdb1"},
			Mounts:  []Mount{vol("mariadb-data-mdb1", "/var/lib/mysql")},
		},
		{
			ID: "pelk0000000000000000", Name: "wordpress-svc2", Image: "litespeedtech/openlitespeed:1.8", State: "running",
			Labels: map[string]string{
				"coolify.managed": "true", "coolify.serviceUuid": "svc2", "coolify.resourceName": "pelk-wordpress",
				"coolify.projectName": "pelk", "com.docker.compose.project": "svc2", "com.docker.compose.service": "wordpress",
			},
			Env:    []string{"WORDPRESS_DB_HOST=mariadb", "WORDPRESS_DB_NAME=pelk"},
			Mounts: []Mount{vol("svc2_wordpress-files", "/var/www/vhosts/localhost/html")},
		},
		{
			ID: "pelkdb00000000000000", Name: "mariadb-svc2", Image: "mariadb:11", State: "running",
			Labels: map[string]string{
				"coolify.managed": "true", "coolify.serviceUuid": "svc2", "coolify.resourceName": "pelk-wordpress",
				"coolify.projectName": "pelk", "com.docker.compose.project": "svc2", "com.docker.compose.service": "mariadb",
				"coolify.service.subType": "database",
			},
			Env:    []string{"MYSQL_DATABASE=pelk"},
			Mounts: []Mount{vol("svc2_mariadb-data", "/var/lib/mysql")},
		},
		{
			ID: "pg000000000000000000", Name: "pg1", Image: "postgres:16-alpine", State: "exited",
			Labels: map[string]string{"coolify.managed": "true", "coolify.databaseUuid": "pg1", "coolify.resourceName": "iranreps-postgres", "coolify.projectName": "iranreps"},
			Env:    []string{"POSTGRES_USER=iranreps", "POSTGRES_DB=iranreps"},
			Mounts: []Mount{vol("postgres-data-pg1", "/var/lib/postgresql/data")},
		},
		{
			ID: "plain000000000000000", Name: "grafana-1", Image: "grafana/grafana", State: "running",
			Labels: map[string]string{"com.docker.compose.project": "monitoring", "com.docker.compose.service": "grafana"},
			Mounts: []Mount{vol("monitoring_grafana", "/var/lib/grafana")},
		},
	}}
}

func discoveryApp(t *testing.T) (*App, *fakeDocker) {
	t.Helper()
	app := testApp(t, &fakeRunner{installed: true})
	fd := coolifyHost()
	app.Docker = fd
	app.Discovery.selfID = func() string { return "self00000000" }
	return app, fd
}

func findResource(t *testing.T, res []Resource, key string) Resource {
	t.Helper()
	for _, r := range res {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("resource %s not found among %d", key, len(res))
	return Resource{}
}

func TestDiscoveryGroupsCoolifyResources(t *testing.T) {
	app, _ := discoveryApp(t)
	repo, _ := app.Repos.Create(Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	folder, _ := app.Folders.Create(Folder{Meta: Meta{Name: "shop files"}, Path: "/docker-volumes/svc1_wordpress-files/_data"})
	job, _ := app.Jobs.Create(Job{Meta: Meta{Name: "shop"}, FolderID: folder.ID, RepositoryID: repo.ID})

	res, err := app.Discovery.Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	shop := findResource(t, res, "service:svc1")
	if shop.Name != "digipharma-wordpress" || shop.Project != "digipharma" || shop.Environment != "production" {
		t.Fatalf("shop = %+v", shop)
	}
	if strings.Join(shop.Domains, ",") != "shop.example.com,www.shop.example.com" {
		t.Fatalf("domains = %v", shop.Domains)
	}
	// The config file bind mount is not site data; the files volume is, reached
	// through this app's own /docker-volumes mount, and already backed up.
	if len(shop.Volumes) != 1 {
		t.Fatalf("volumes = %+v", shop.Volumes)
	}
	v := shop.Volumes[0]
	if v.Path != "/docker-volumes/svc1_wordpress-files/_data" || v.FolderID != folder.ID || len(v.Jobs) != 1 || v.Jobs[0].ID != job.ID {
		t.Fatalf("volume = %+v", v)
	}
	if v.DatabaseData || v.MountedAt[0] != "/var/www/html" {
		t.Fatalf("volume = %+v", v)
	}
	// The site uses a database on the shared server, found by its host name.
	if len(shop.LinkedDatabases) != 1 {
		t.Fatalf("linked = %+v", shop.LinkedDatabases)
	}
	ld := shop.LinkedDatabases[0]
	if ld.Container != "mdb1" || ld.Database != "shop" || ld.Engine != EngineMariaDB || ld.Resource != "database:mdb1" {
		t.Fatalf("linked = %+v", ld)
	}

	shared := findResource(t, res, "database:mdb1")
	if len(shared.Databases) != 1 || shared.Databases[0].Engine != EngineMariaDB || shared.Databases[0].DefaultDatabase != "wordpress_test" {
		t.Fatalf("shared = %+v", shared.Databases)
	}
	if len(shared.Volumes) != 1 || !shared.Volumes[0].DatabaseData {
		t.Fatalf("a database's data directory must be flagged: %+v", shared.Volumes)
	}

	// A service with its own database: the site container links to it by its
	// compose service name.
	pelk := findResource(t, res, "service:svc2")
	if len(pelk.Containers) != 2 || len(pelk.Databases) != 1 || pelk.Databases[0].OwnerService != "mariadb" {
		t.Fatalf("pelk = %+v", pelk)
	}
	if len(pelk.LinkedDatabases) != 1 || pelk.LinkedDatabases[0].Container != "mariadb-svc2" {
		t.Fatalf("pelk linked = %+v", pelk.LinkedDatabases)
	}

	pg := findResource(t, res, "database:pg1")
	if pg.Databases[0].Engine != EnginePostgres || pg.Databases[0].DefaultDatabase != "iranreps" || pg.Databases[0].State != "exited" {
		t.Fatalf("pg = %+v", pg.Databases)
	}

	self := findResource(t, res, "application:rw0000")
	if !self.Self {
		t.Fatalf("own resource not marked: %+v", self)
	}
	if len(self.Volumes) != 0 {
		t.Fatalf("this app's own bind mounts are not backup candidates: %+v", self.Volumes)
	}

	// Coolify resources come before plain compose projects.
	if res[len(res)-1].Key != "compose:monitoring" {
		t.Fatalf("last resource = %s", res[len(res)-1].Key)
	}
}

func TestDiscoveryNeverReturnsSecrets(t *testing.T) {
	app, _ := discoveryApp(t)
	h := routesFor(t, app)
	code, body := rawGet(t, h, "/api/docker/resources")
	if code != 200 {
		t.Fatalf("code=%d body=%s", code, body)
	}
	for _, secret := range []string{"sekrit", "rootpw", "WORDPRESS_DB_PASSWORD"} {
		if strings.Contains(body, secret) {
			t.Fatalf("resources leaked %q: %s", secret, body)
		}
	}
}

func TestResolveDatabaseContainerFollowsRedeploy(t *testing.T) {
	app, fd := discoveryApp(t)
	src := Database{Meta: Meta{Name: "pelk db"}, Engine: EngineMariaDB, Container: "mariadb-svc2-old",
		OwnerKind: "service", OwnerUUID: "svc2", OwnerService: "mariadb", DatabaseName: "pelk"}
	c, err := app.Discovery.ResolveDatabaseContainer(context.Background(), src)
	if err != nil || c.Name != "mariadb-svc2" {
		t.Fatalf("resolve by owner: %+v %v", c, err)
	}

	// A source without owner labels falls back to the container name.
	byName := Database{Container: "mdb1", Engine: EngineMariaDB, DatabaseName: "shop"}
	if c, err := app.Discovery.ResolveDatabaseContainer(context.Background(), byName); err != nil || c.Name != "mdb1" {
		t.Fatalf("resolve by name: %+v %v", c, err)
	}

	stopped := Database{Container: "pg1", OwnerKind: "database", OwnerUUID: "pg1", Engine: EnginePostgres, DatabaseName: "iranreps"}
	if _, err := app.Discovery.ResolveDatabaseContainer(context.Background(), stopped); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("stopped container err = %v", err)
	}

	fd.containers = fd.containers[:1]
	if _, err := app.Discovery.ResolveDatabaseContainer(context.Background(), byName); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing container err = %v", err)
	}
}

func TestContainersUsingPath(t *testing.T) {
	app, _ := discoveryApp(t)
	got, err := app.Discovery.ContainersUsingPath(context.Background(), "/docker-volumes/svc1_wordpress-files/_data")
	if err != nil {
		t.Fatal(err)
	}
	// The site container, never this app itself (which mounts all volumes).
	if len(got) != 1 || got[0].Name != "wordpress-svc1" {
		t.Fatalf("got %+v", got)
	}
	// A subfolder of the volume is used by the same container.
	got, _ = app.Discovery.ContainersUsingPath(context.Background(), "/docker-volumes/svc1_wordpress-files/_data/wp-content")
	if len(got) != 1 {
		t.Fatalf("subfolder: %+v", got)
	}
	// A path this app does not see through a mount maps to no container.
	got, _ = app.Discovery.ContainersUsingPath(context.Background(), "/srv/elsewhere")
	if len(got) != 0 {
		t.Fatalf("unmapped: %+v", got)
	}
}

func TestContainersUsingPathSkipsInfrastructure(t *testing.T) {
	app, fd := discoveryApp(t)
	broad := []Mount{
		{Type: "bind", Source: "/var/lib/docker/volumes", Destination: "/vols", RW: true},
		{Type: "bind", Source: "/", Destination: "/rootfs", RW: true},
	}
	fd.containers = append(fd.containers,
		Container{ID: "coolify0000000000000", Name: "coolify", Image: "coollabsio/coolify", State: "running",
			Mounts: []Mount{{Type: "bind", Source: "/data/coolify/services", Destination: "/var/www/html/storage/services", RW: true}}},
		Container{ID: "cadvisor000000000000", Name: "cadvisor", Image: "gcr.io/cadvisor", State: "running", Mounts: broad},
		Container{ID: "reader00000000000000", Name: "reader", Image: "alpine", State: "running",
			Mounts: []Mount{{Type: "volume", Name: "svc1_wordpress-files", Source: "/var/lib/docker/volumes/svc1_wordpress-files/_data", Destination: "/ro", RW: false}}},
	)
	got, err := app.Discovery.ContainersUsingPath(context.Background(), "/docker-volumes/svc1_wordpress-files/_data")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "wordpress-svc1" {
		t.Fatalf("got %+v", got)
	}

	// Without knowing its own container, the app refuses to stop anything.
	app.Discovery.selfID = func() string { return "" }
	if _, err := app.Discovery.ContainersUsingPath(context.Background(), "/docker-volumes/svc1_wordpress-files/_data"); err == nil {
		t.Fatal("expected a refusal without self-detection")
	}
}

func TestResolveDatabaseContainerChecksEngineAndAmbiguity(t *testing.T) {
	app, fd := discoveryApp(t)
	// The source says PostgreSQL; the only container with that name runs MariaDB.
	wrong := Database{Container: "mdb1", Engine: EnginePostgres, DatabaseName: "x"}
	if _, err := app.Discovery.ResolveDatabaseContainer(context.Background(), wrong); err == nil {
		t.Fatal("a container of another engine must not match")
	}
	// Two running MariaDB parts of one service and no part named: ambiguous.
	fd.containers = append(fd.containers, Container{ID: "pelkdb20000000000000", Name: "mariadb2-svc2", Image: "mariadb:11", State: "running",
		Labels: map[string]string{"coolify.serviceUuid": "svc2", "com.docker.compose.service": "mariadb2"}})
	vague := Database{Container: "mariadb-svc2", Engine: EngineMariaDB, OwnerKind: "service", OwnerUUID: "svc2", DatabaseName: "pelk"}
	if _, err := app.Discovery.ResolveDatabaseContainer(context.Background(), vague); err == nil || !strings.Contains(err.Error(), "2 running") {
		t.Fatalf("ambiguous err = %v", err)
	}
}

func TestPathMapPrefersMostSpecificMount(t *testing.T) {
	pm := pathMap{
		{Host: "/var/lib/docker/volumes", Local: "/docker-volumes"},
		{Host: "/var/lib/docker/volumes/special/_data", Local: "/special"},
	}
	for host, want := range map[string]string{
		"/var/lib/docker/volumes/x/_data":         "/docker-volumes/x/_data",
		"/var/lib/docker/volumes/special/_data/a": "/special/a",
		"/var/lib/docker/volumes":                 "/docker-volumes",
	} {
		if got, ok := pm.toLocal(host); !ok || got != want {
			t.Errorf("toLocal(%s) = %q %v, want %q", host, got, ok, want)
		}
	}
	if _, ok := pm.toLocal("/var/lib/docker/volumesX/y"); ok {
		t.Error("a sibling with a common prefix must not match")
	}
	if got, ok := pm.toHost("/docker-volumes/x/_data"); !ok || got != "/var/lib/docker/volumes/x/_data" {
		t.Errorf("toHost = %q %v", got, ok)
	}
}

func TestLinkedDatabasesFromEnv(t *testing.T) {
	got := linkedDatabasesFromEnv([]string{
		"DATABASE_URL=postgres://u:p@pg1:5432/app?sslmode=disable",
		"DB_HOST=mysql.internal", "DB_DATABASE=laravel",
		"WORDPRESS_DB_HOST=db:3306", "WORDPRESS_DB_NAME=wp; DROP TABLE x",
	})
	var keys []string
	for _, l := range got {
		keys = append(keys, l.Host+"/"+l.Database)
	}
	// An unsafe database name is ignored rather than offered.
	if strings.Join(keys, ",") != "mysql.internal/laravel,pg1/app" {
		t.Fatalf("got %v", keys)
	}
}

func TestDetectEngine(t *testing.T) {
	cases := map[string]string{
		"postgres:16-alpine":                       EnginePostgres,
		"docker.io/library/postgres:15":            EnginePostgres,
		"postgis/postgis:16-3.4":                   EnginePostgres,
		"timescale/timescaledb:latest-pg16":        EnginePostgres,
		"mariadb:11":                               EngineMariaDB,
		"bitnami/mariadb:10.11":                    EngineMariaDB,
		"mysql:8.4":                                EngineMySQL,
		"percona/percona-server:8.0":               EngineMySQL,
		"redis:7":                                  "",
		"wordpress:6":                              "",
		"ghcr.io/baosystems/postgis:16@sha256:abc": EnginePostgres,
	}
	for img, want := range cases {
		if got := detectEngine(img, nil); got != want {
			t.Errorf("detectEngine(%s) = %q, want %q", img, got, want)
		}
	}
	if got := detectEngine("custom:1", map[string]string{"coolify.database.subType": "postgresql"}); got != EnginePostgres {
		t.Errorf("label: %q", got)
	}
}

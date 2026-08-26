package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

func (c *CLI) cmdRepo(args []string) int {
	if len(args) == 0 || wantHelp(args) {
		fmt.Fprint(os.Stdout, helpRepo())
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	switch args[0] {
	case "list", "ls":
		return c.repoList()
	case "get", "show":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoGet(args[1])
	case "create", "add":
		return c.repoCreate(args[1:])
	case "update", "edit":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoUpdate(args[1], args[2:])
	case "delete", "rm", "remove":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoDelete(args[1])
	case "test":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoTest(args[1])
	case "init":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		rest, wait, follow := parseWaitFollow(args[2:])
		_ = rest
		return c.repoInit(args[1], wait, follow)
	case "unlock":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoUnlock(args[1])
	case "snapshots", "snaps":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoSnapshots(args[1])
	case "restore":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoRestore(args[1], args[2:])
	case "download":
		if err := requireArgs(args[1:], 1, helpRepo()); err != nil {
			return c.fail(err)
		}
		return c.repoDownload(args[1], args[2:])
	default:
		return c.fail(usagef("unknown repo command %q\n\n%s", args[0], helpRepo()))
	}
}

func (c *CLI) repoList() int {
	items, err := c.listRepos()
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		rows := make([]any, 0, len(items))
		for _, it := range items {
			rows = append(rows, it.Raw)
		}
		return c.writeJSON(map[string]any{"ok": true, "repositories": rows})
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		backend := strField(it.Raw, "backendType")
		loc := strField(it.Raw, "localPath")
		if backend == "S3" {
			loc = strField(it.Raw, "endpoint") + "/" + strField(it.Raw, "bucket")
		}
		rows = append(rows, []string{
			it.ID,
			it.Name,
			backend,
			loc,
			yn(boolField(it.Raw, "hasPassword")),
		})
	}
	c.printTable([]string{"ID", "NAME", "BACKEND", "LOCATION", "PASSWORD"}, rows)
	return exitOK
}

func (c *CLI) repoGet(query string) int {
	ref, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodGet, "/api/repositories/"+ref.ID, nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	r := asMap(m["repository"])
	fmt.Printf("id:\t%s\n", strField(r, "id"))
	fmt.Printf("name:\t%s\n", strField(r, "name"))
	fmt.Printf("backendType:\t%s\n", strField(r, "backendType"))
	if strField(r, "backendType") == "Local" {
		fmt.Printf("localPath:\t%s\n", strField(r, "localPath"))
	} else {
		fmt.Printf("endpoint:\t%s\n", strField(r, "endpoint"))
		fmt.Printf("bucket:\t%s\n", strField(r, "bucket"))
		fmt.Printf("region:\t%s\n", dash(strField(r, "region")))
		fmt.Printf("accessKey:\t%s\n", dash(strField(r, "accessKey")))
		fmt.Printf("hasSecretKey:\t%v\n", boolField(r, "hasSecretKey"))
	}
	fmt.Printf("hasPassword:\t%v\n", boolField(r, "hasPassword"))
	return exitOK
}

func (c *CLI) repoCreate(args []string) int {
	fs := newFlagSet("repo create")
	name := fs.String("name", "", "repository name")
	backend := fs.String("backend", "", "Local or S3")
	pass := fs.String("password", "", "restic repository password")
	path := fs.String("path", "", "local repository directory")
	endpoint := fs.String("endpoint", "", "S3 endpoint URL")
	bucket := fs.String("bucket", "", "S3 bucket")
	region := fs.String("region", "", "S3 region")
	accessKey := fs.String("access-key", "", "S3 access key")
	secretKey := fs.String("secret-key", "", "S3 secret key")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}
	b := strings.TrimSpace(*backend)
	if b == "local" {
		b = "Local"
	}
	if b == "s3" {
		b = "S3"
	}
	if *name == "" || b == "" || *pass == "" {
		return c.fail(usagef("--name, --backend, and --password are required\n\n%s", helpRepo()))
	}
	body := map[string]any{
		"name":        *name,
		"backendType": b,
		"password":    *pass,
	}
	switch b {
	case "Local":
		if *path == "" {
			return c.fail(usagef("--path is required for Local backend"))
		}
		body["localPath"] = *path
	case "S3":
		if *endpoint == "" || *bucket == "" || *accessKey == "" || *secretKey == "" {
			return c.fail(usagef("S3 requires --endpoint --bucket --access-key --secret-key"))
		}
		body["endpoint"] = *endpoint
		body["bucket"] = *bucket
		body["region"] = *region
		body["accessKey"] = *accessKey
		body["secretKey"] = *secretKey
	default:
		return c.fail(usagef(`--backend must be "Local" or "S3"`))
	}
	status, m, err := c.doJSON(http.MethodPost, "/api/repositories", body)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	r := asMap(m["repository"])
	c.note("Created repository %s (%s)", strField(r, "name"), strField(r, "id"))
	fmt.Println(strField(r, "id"))
	return exitOK
}

func (c *CLI) repoUpdate(query string, args []string) int {
	ref, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	fs := newFlagSet("repo update")
	name := fs.String("name", "", "new name")
	backend := fs.String("backend", "", "Local or S3")
	pass := fs.String("password", "", "new restic password (omit to keep)")
	path := fs.String("path", "", "local path")
	endpoint := fs.String("endpoint", "", "S3 endpoint")
	bucket := fs.String("bucket", "", "S3 bucket")
	region := fs.String("region", "", "S3 region")
	accessKey := fs.String("access-key", "", "S3 access key")
	secretKey := fs.String("secret-key", "", "S3 secret key")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}

	body := map[string]any{
		"name":        ref.Name,
		"backendType": strField(ref.Raw, "backendType"),
		"localPath":   strField(ref.Raw, "localPath"),
		"endpoint":    strField(ref.Raw, "endpoint"),
		"bucket":      strField(ref.Raw, "bucket"),
		"region":      strField(ref.Raw, "region"),
		"accessKey":   strField(ref.Raw, "accessKey"),
	}
	if *name != "" {
		body["name"] = *name
	}
	if *backend != "" {
		b := *backend
		if b == "local" {
			b = "Local"
		}
		if b == "s3" {
			b = "S3"
		}
		body["backendType"] = b
	}
	if *path != "" {
		body["localPath"] = *path
	}
	if *endpoint != "" {
		body["endpoint"] = *endpoint
	}
	if *bucket != "" {
		body["bucket"] = *bucket
	}
	if *region != "" {
		body["region"] = *region
	}
	if *accessKey != "" {
		body["accessKey"] = *accessKey
	}
	if *pass != "" {
		body["password"] = *pass
	}
	if *secretKey != "" {
		body["secretKey"] = *secretKey
	}

	status, m, err := c.doJSON(http.MethodPut, "/api/repositories/"+ref.ID, body)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	return c.okMessage("Repository updated.", map[string]any{"id": ref.ID})
}

func (c *CLI) repoDelete(query string) int {
	ref, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodDelete, "/api/repositories/"+ref.ID, nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	return c.okMessage(fmt.Sprintf("Deleted repository %s.", ref.Name), map[string]any{"id": ref.ID})
}

func (c *CLI) repoTest(query string) int {
	ref, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodPost, "/api/repositories/"+ref.ID+"/test", map[string]any{})
	if err != nil {
		return c.fail(err)
	}
	// test endpoint uses ok for restic reachability, not HTTP success alone
	if status >= 400 {
		return c.fail(&apiError{Status: status, Code: codeOf(m), Message: messageOf(m), Body: m})
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	fmt.Printf("ok:\t%v\n", boolField(m, "ok"))
	fmt.Printf("initialized:\t%v\n", boolField(m, "initialized"))
	fmt.Printf("message:\t%s\n", dash(strField(m, "message")))
	if d := strField(m, "detail"); d != "" {
		fmt.Printf("detail:\t%s\n", d)
	}
	if !boolField(m, "ok") {
		return exitError
	}
	return exitOK
}

func (c *CLI) repoInit(query string, wait, follow bool) int {
	ref, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodPost, "/api/repositories/"+ref.ID+"/init", map[string]any{})
	if err != nil {
		return c.fail(err)
	}
	return c.startAndMaybeWait(status, m, wait, follow)
}

func (c *CLI) repoUnlock(query string) int {
	ref, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodPost, "/api/repositories/"+ref.ID+"/unlock", map[string]any{})
	if err != nil {
		return c.fail(err)
	}
	if status >= 400 || codeOf(m) == "unlock_failed" {
		return c.fail(&apiError{Status: status, Code: codeOf(m), Message: messageOf(m), Body: m})
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	return c.okMessage(strField(m, "message"), nil)
}

func (c *CLI) repoSnapshots(query string) int {
	ref, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	return c.printSnapshots("/api/repositories/" + ref.ID + "/snapshots")
}

func (c *CLI) printSnapshots(path string) int {
	status, m, err := c.doJSON(http.MethodGet, path, nil)
	if err != nil {
		return c.fail(err)
	}
	if codeOf(m) != "" && !truthy(m["ok"]) {
		return c.fail(&apiError{Status: status, Code: codeOf(m), Message: messageOf(m), Body: m})
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	rows := [][]string{}
	for _, item := range asSlice(m["snapshots"]) {
		s := asMap(item)
		tags := ""
		if t := asSlice(s["tags"]); len(t) > 0 {
			parts := make([]string, 0, len(t))
			for _, x := range t {
				parts = append(parts, fmt.Sprint(x))
			}
			tags = strings.Join(parts, ",")
		}
		rows = append(rows, []string{
			dash(strField(s, "shortId", "id")),
			timeField(s, "time"),
			dash(strField(s, "hostname")),
			dash(tags),
		})
	}
	c.printTable([]string{"SNAPSHOT", "TIME", "HOST", "TAGS"}, rows)
	return exitOK
}

func (c *CLI) repoRestore(query string, args []string) int {
	ref, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	args, wait, follow := parseWaitFollow(args)
	fs := newFlagSet("repo restore")
	snap := fs.String("snapshot", "", "snapshot id")
	target := fs.String("target", "", "restore destination directory")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}
	if *snap == "" || *target == "" {
		return c.fail(usagef("--snapshot and --target are required"))
	}
	status, m, err := c.doJSON(http.MethodPost, "/api/repositories/"+ref.ID+"/restore", map[string]string{
		"snapshotId": *snap,
		"target":     *target,
	})
	if err != nil {
		return c.fail(err)
	}
	return c.startAndMaybeWait(status, m, wait, follow)
}

func (c *CLI) repoDownload(query string, args []string) int {
	ref, err := c.resolveRepo(query)
	if err != nil {
		return c.fail(err)
	}
	args, wait, follow := parseWaitFollow(args)
	fs := newFlagSet("repo download")
	snap := fs.String("snapshot", "", "snapshot id")
	if err := parseFlagSet(fs, args, helpRepo()); err != nil {
		return c.fail(err)
	}
	if *snap == "" {
		return c.fail(usagef("--snapshot is required"))
	}
	status, m, err := c.doJSON(http.MethodPost, "/api/repositories/"+ref.ID+"/download", map[string]string{
		"snapshotId": *snap,
	})
	if err != nil {
		return c.fail(err)
	}
	return c.startAndMaybeWait(status, m, wait, follow)
}

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"restic-web/internal/core"
)

func (c *CLI) cmdDocker(args []string) int {
	if len(args) == 0 || wantHelp(args) {
		fmt.Fprint(os.Stdout, helpDocker())
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	switch args[0] {
	case "status":
		return c.dockerStatus()
	case "resources", "sites", "list", "ls":
		return c.dockerResources()
	case "databases", "dbs":
		if err := requireArgs(args[1:], 1, helpDocker()); err != nil {
			return c.fail(err)
		}
		return c.dockerDatabases(args[1])
	default:
		return c.fail(usagef("unknown docker command %q\n\n%s", args[0], helpDocker()))
	}
}

func (c *CLI) dockerStatus() int {
	ctx, cancel := dockerContext()
	defer cancel()
	st := c.app.Discovery.Status(ctx)
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": st.Available, "docker": st})
	}
	fmt.Printf("available:\t%s\n", yn(st.Available))
	fmt.Printf("coolify:\t%s\n", yn(st.Coolify))
	fmt.Printf("host:\t%s\n", dash(st.Host))
	if st.Message != "" {
		fmt.Printf("message:\t%s\n", st.Message)
	}
	if !st.Available {
		return exitError
	}
	return exitOK
}

func (c *CLI) dockerResources() int {
	ctx, cancel := dockerContext()
	defer cancel()
	if err := c.app.Docker.Ping(ctx); err != nil {
		return c.fail(&apiError{Code: "no_docker", Message: err.Error()})
	}
	res, err := c.app.Discovery.Resources(ctx)
	if err != nil {
		return c.fail(&apiError{Code: "docker_error", Message: err.Error()})
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "resources": res})
	}
	rows := [][]string{}
	for _, r := range res {
		var vols, dbs []string
		for _, v := range r.Volumes {
			if v.DatabaseData || v.Path == "" {
				continue
			}
			mark := ""
			if len(v.Jobs) > 0 {
				mark = "*"
			}
			vols = append(vols, v.Path+mark)
		}
		for _, d := range r.Databases {
			dbs = append(dbs, d.Engine+":"+d.Container)
		}
		for _, l := range r.LinkedDatabases {
			if l.Container != "" {
				dbs = append(dbs, "uses "+l.Database+"@"+l.Container)
			}
		}
		rows = append(rows, []string{r.Key, r.Name, dash(r.Project), dash(strings.Join(r.Domains, " ")),
			dash(strings.Join(vols, " ")), dash(strings.Join(dbs, " "))})
	}
	c.printTable([]string{"RESOURCE", "NAME", "PROJECT", "DOMAINS", "FOLDERS (*=backed up)", "DATABASES"}, rows)
	return exitOK
}

func (c *CLI) dockerDatabases(container string) int {
	ctx, cancel := dockerContext()
	defer cancel()
	engine, dbs, cont, err := c.app.ListContainerDatabases(ctx, container)
	if err != nil {
		var nf *core.NotFoundError
		var ve *core.ValidationError
		if errors.As(err, &nf) || errors.As(err, &ve) {
			return c.fail(err)
		}
		return c.fail(&apiError{Code: "docker_error", Message: err.Error()})
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "container": cont.Name, "engine": engine, "databases": dbs})
	}
	fmt.Printf("%s (%s), %d database(s):\n", cont.Name, engine, len(dbs))
	for _, d := range dbs {
		fmt.Println("  " + d)
	}
	return exitOK
}

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"restic-web/internal/core"
)

func (c *CLI) cmdDatabase(args []string) int {
	if len(args) == 0 || wantHelp(args) {
		fmt.Fprint(os.Stdout, helpDatabase())
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	switch args[0] {
	case "list", "ls":
		return c.databaseList()
	case "get", "show":
		if err := requireArgs(args[1:], 1, helpDatabase()); err != nil {
			return c.fail(err)
		}
		return c.databaseGet(args[1])
	case "create", "add":
		return c.databaseCreate(args[1:])
	case "update", "edit":
		if err := requireArgs(args[1:], 1, helpDatabase()); err != nil {
			return c.fail(err)
		}
		return c.databaseUpdate(args[1], args[2:])
	case "delete", "rm", "remove":
		if err := requireArgs(args[1:], 1, helpDatabase()); err != nil {
			return c.fail(err)
		}
		return c.databaseDelete(args[1])
	default:
		return c.fail(usagef("unknown database command %q\n\n%s", args[0], helpDatabase()))
	}
}

// dockerContext bounds a call to the Docker Engine.
func dockerContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func (c *CLI) databaseList() int {
	items := c.app.Databases.List()
	if items == nil {
		items = []core.Database{}
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "databases": items})
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		rows = append(rows, []string{it.ID, it.Name, it.Engine, it.Container, it.DatabaseName})
	}
	c.printTable([]string{"ID", "NAME", "ENGINE", "CONTAINER", "DATABASE"}, rows)
	return exitOK
}

func (c *CLI) databaseGet(query string) int {
	d, err := c.resolveDatabase(query)
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "database": d})
	}
	fmt.Printf("id:\t%s\n", d.ID)
	fmt.Printf("name:\t%s\n", d.Name)
	fmt.Printf("engine:\t%s\n", d.Engine)
	fmt.Printf("container:\t%s\n", d.Container)
	fmt.Printf("database:\t%s\n", d.DatabaseName)
	if d.OwnerUUID != "" {
		fmt.Printf("owner:\t%s %s %s\n", d.OwnerKind, d.OwnerUUID, d.OwnerService)
	}
	fmt.Printf("createdAt:\t%s\n", formatTime(d.CreatedAt))
	return exitOK
}

// databaseCreate defines a database source. The engine and owning resource are
// read from the container through Docker unless --engine is given.
func (c *CLI) databaseCreate(args []string) int {
	fs := newFlagSet("database create")
	name := fs.String("name", "", "database source name")
	container := fs.String("container", "", "database container name or id")
	// --database is the global Postgres URL flag, so the database is --db.
	database := fs.String("db", "", "database inside the server")
	engine := fs.String("engine", "", "postgres|mysql|mariadb (default: detect from the container)")
	if err := parseFlagSet(fs, args, helpDatabase()); err != nil {
		return c.fail(err)
	}
	if *name == "" || *container == "" || *database == "" {
		return c.fail(usagef("--name, --container and --db are required\n\n%s", helpDatabase()))
	}
	var src core.Database
	if *engine != "" {
		src = core.Database{Meta: core.Meta{Name: *name}, Engine: *engine, Container: *container, DatabaseName: *database}
		if err := src.Validate(); err != nil {
			return c.fail(&core.ValidationError{Msg: err.Error()})
		}
	} else {
		ctx, cancel := dockerContext()
		defer cancel()
		found, existing, err := c.app.DatabaseFromContainer(ctx, *container, *database, *name)
		if err != nil {
			return c.fail(err)
		}
		if existing {
			return c.fail(&core.ConflictError{Msg: fmt.Sprintf("database source %q already backs up %s on %s", found.Name, found.DatabaseName, found.Container)})
		}
		src = found
	}
	created, err := c.app.Databases.Create(src)
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "database": created})
	}
	c.note("Created database source %s (%s)", created.Name, created.ID)
	fmt.Println(created.ID)
	return exitOK
}

func (c *CLI) databaseUpdate(query string, args []string) int {
	existing, err := c.resolveDatabase(query)
	if err != nil {
		return c.fail(err)
	}
	fs := newFlagSet("database update")
	name := fs.String("name", "", "new name")
	database := fs.String("db", "", "new database name")
	if err := parseFlagSet(fs, args, helpDatabase()); err != nil {
		return c.fail(err)
	}
	upd := existing
	if *name != "" {
		upd.Name = *name
	}
	if *database != "" {
		upd.DatabaseName = *database
	}
	if err := upd.Validate(); err != nil {
		return c.fail(&core.ValidationError{Msg: err.Error()})
	}
	updated, err := c.app.Databases.Update(existing.ID, upd)
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "database": updated})
	}
	return c.okMessage("Database source updated.", map[string]any{"id": existing.ID})
}

func (c *CLI) databaseDelete(query string) int {
	d, err := c.resolveDatabase(query)
	if err != nil {
		return c.fail(err)
	}
	if using := c.app.JobsUsingDatabase(d.ID); len(using) > 0 {
		return c.fail(&core.ConflictError{
			Msg: "This database source is used by " + jobNames(using) + ". Delete or edit those jobs first.",
		})
	}
	if err := c.app.Databases.Delete(d.ID); err != nil {
		return c.fail(err)
	}
	return c.okMessage(fmt.Sprintf("Deleted database source %s.", d.Name), map[string]any{"id": d.ID})
}

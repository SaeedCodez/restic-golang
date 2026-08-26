package main

import (
	"fmt"
	"os"

	"restic-web/internal/core"
)

func (c *CLI) cmdFolder(args []string) int {
	if len(args) == 0 || wantHelp(args) {
		fmt.Fprint(os.Stdout, helpFolder())
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	switch args[0] {
	case "list", "ls":
		return c.folderList()
	case "get", "show":
		if err := requireArgs(args[1:], 1, helpFolder()); err != nil {
			return c.fail(err)
		}
		return c.folderGet(args[1])
	case "create", "add":
		return c.folderCreate(args[1:])
	case "update", "edit":
		if err := requireArgs(args[1:], 1, helpFolder()); err != nil {
			return c.fail(err)
		}
		return c.folderUpdate(args[1], args[2:])
	case "delete", "rm", "remove":
		if err := requireArgs(args[1:], 1, helpFolder()); err != nil {
			return c.fail(err)
		}
		return c.folderDelete(args[1])
	default:
		return c.fail(usagef("unknown folder command %q\n\n%s", args[0], helpFolder()))
	}
}

func (c *CLI) folderList() int {
	items := c.app.Folders.List()
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "folders": items})
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		rows = append(rows, []string{it.ID, it.Name, it.Path})
	}
	c.printTable([]string{"ID", "NAME", "PATH"}, rows)
	return exitOK
}

func (c *CLI) folderGet(query string) int {
	f, err := c.resolveFolder(query)
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "folder": f})
	}
	fmt.Printf("id:\t%s\n", f.ID)
	fmt.Printf("name:\t%s\n", f.Name)
	fmt.Printf("path:\t%s\n", f.Path)
	fmt.Printf("createdAt:\t%s\n", formatTime(f.CreatedAt))
	fmt.Printf("updatedAt:\t%s\n", formatTime(f.UpdatedAt))
	return exitOK
}

func (c *CLI) folderCreate(args []string) int {
	fs := newFlagSet("folder create")
	name := fs.String("name", "", "folder name")
	path := fs.String("path", "", "absolute source path")
	if err := parseFlagSet(fs, args, helpFolder()); err != nil {
		return c.fail(err)
	}
	if *name == "" || *path == "" {
		return c.fail(usagef("--name and --path are required\n\n%s", helpFolder()))
	}
	f := core.Folder{Meta: core.Meta{Name: *name}, Path: *path}
	if err := f.Validate(); err != nil {
		return c.fail(&core.ValidationError{Msg: err.Error()})
	}
	created, err := c.app.Folders.Create(f)
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "folder": created})
	}
	c.note("Created folder %s (%s)", created.Name, created.ID)
	fmt.Println(created.ID)
	return exitOK
}

func (c *CLI) folderUpdate(query string, args []string) int {
	existing, err := c.resolveFolder(query)
	if err != nil {
		return c.fail(err)
	}
	fs := newFlagSet("folder update")
	name := fs.String("name", "", "new name")
	path := fs.String("path", "", "new path")
	if err := parseFlagSet(fs, args, helpFolder()); err != nil {
		return c.fail(err)
	}
	upd := existing
	if *name != "" {
		upd.Name = *name
	}
	if *path != "" {
		upd.Path = *path
	}
	if err := upd.Validate(); err != nil {
		return c.fail(&core.ValidationError{Msg: err.Error()})
	}
	updated, err := c.app.Folders.Update(existing.ID, upd)
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSON(map[string]any{"ok": true, "folder": updated})
	}
	return c.okMessage("Folder updated.", map[string]any{"id": existing.ID})
}

func (c *CLI) folderDelete(query string) int {
	f, err := c.resolveFolder(query)
	if err != nil {
		return c.fail(err)
	}
	if using := c.app.JobsUsingFolder(f.ID); len(using) > 0 {
		return c.fail(&core.ConflictError{
			Msg: "This folder is used by " + jobNames(using) + ". Delete or edit those jobs first.",
		})
	}
	if err := c.app.Folders.Delete(f.ID); err != nil {
		return c.fail(err)
	}
	return c.okMessage(fmt.Sprintf("Deleted folder %s.", f.Name), map[string]any{"id": f.ID})
}

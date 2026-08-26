package main

import (
	"fmt"
	"net/http"
	"os"
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
	items, err := c.listFolders()
	if err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		rows := make([]any, 0, len(items))
		for _, it := range items {
			rows = append(rows, it.Raw)
		}
		return c.writeJSON(map[string]any{"ok": true, "folders": rows})
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		rows = append(rows, []string{it.ID, it.Name, strField(it.Raw, "path")})
	}
	c.printTable([]string{"ID", "NAME", "PATH"}, rows)
	return exitOK
}

func (c *CLI) folderGet(query string) int {
	ref, err := c.resolveFolder(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodGet, "/api/folders/"+ref.ID, nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	f := asMap(m["folder"])
	fmt.Printf("id:\t%s\n", strField(f, "id"))
	fmt.Printf("name:\t%s\n", strField(f, "name"))
	fmt.Printf("path:\t%s\n", strField(f, "path"))
	fmt.Printf("createdAt:\t%s\n", timeField(f, "createdAt"))
	fmt.Printf("updatedAt:\t%s\n", timeField(f, "updatedAt"))
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
	status, m, err := c.doJSON(http.MethodPost, "/api/folders", map[string]string{
		"name": *name,
		"path": *path,
	})
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	f := asMap(m["folder"])
	c.note("Created folder %s (%s)", strField(f, "name"), strField(f, "id"))
	fmt.Println(strField(f, "id"))
	return exitOK
}

func (c *CLI) folderUpdate(query string, args []string) int {
	ref, err := c.resolveFolder(query)
	if err != nil {
		return c.fail(err)
	}
	fs := newFlagSet("folder update")
	name := fs.String("name", "", "new name")
	path := fs.String("path", "", "new path")
	if err := parseFlagSet(fs, args, helpFolder()); err != nil {
		return c.fail(err)
	}
	body := map[string]string{
		"name": ref.Name,
		"path": strField(ref.Raw, "path"),
	}
	if *name != "" {
		body["name"] = *name
	}
	if *path != "" {
		body["path"] = *path
	}
	status, m, err := c.doJSON(http.MethodPut, "/api/folders/"+ref.ID, body)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	return c.okMessage("Folder updated.", map[string]any{"id": ref.ID})
}

func (c *CLI) folderDelete(query string) int {
	ref, err := c.resolveFolder(query)
	if err != nil {
		return c.fail(err)
	}
	status, m, err := c.doJSON(http.MethodDelete, "/api/folders/"+ref.ID, nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	return c.okMessage(fmt.Sprintf("Deleted folder %s.", ref.Name), map[string]any{"id": ref.ID})
}

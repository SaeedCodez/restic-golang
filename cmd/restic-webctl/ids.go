package main

import (
	"fmt"
	"net/http"
	"strings"
)

type entityRef struct {
	ID   string
	Name string
	Raw  map[string]any
}

func (c *CLI) listFolders() ([]entityRef, error) {
	status, m, err := c.doJSON(http.MethodGet, "/api/folders", nil)
	if err != nil {
		return nil, err
	}
	if err := c.requireOK(status, m); err != nil {
		return nil, err
	}
	var out []entityRef
	for _, item := range asSlice(m["folders"]) {
		fm := asMap(item)
		out = append(out, entityRef{ID: strField(fm, "id"), Name: strField(fm, "name"), Raw: fm})
	}
	return out, nil
}

func (c *CLI) listRepos() ([]entityRef, error) {
	status, m, err := c.doJSON(http.MethodGet, "/api/repositories", nil)
	if err != nil {
		return nil, err
	}
	if err := c.requireOK(status, m); err != nil {
		return nil, err
	}
	var out []entityRef
	for _, item := range asSlice(m["repositories"]) {
		fm := asMap(item)
		out = append(out, entityRef{ID: strField(fm, "id"), Name: strField(fm, "name"), Raw: fm})
	}
	return out, nil
}

func (c *CLI) listJobs() ([]entityRef, error) {
	status, m, err := c.doJSON(http.MethodGet, "/api/jobs", nil)
	if err != nil {
		return nil, err
	}
	if err := c.requireOK(status, m); err != nil {
		return nil, err
	}
	var out []entityRef
	for _, item := range asSlice(m["jobs"]) {
		fm := asMap(item)
		out = append(out, entityRef{ID: strField(fm, "id"), Name: strField(fm, "name"), Raw: fm})
	}
	return out, nil
}

func resolveRef(kind, query string, items []entityRef) (entityRef, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return entityRef{}, usagef("missing %s id or name", kind)
	}
	var exactID, exactName, prefixID, prefixName []entityRef
	for _, it := range items {
		if it.ID == q {
			exactID = append(exactID, it)
		}
		if strings.EqualFold(it.Name, q) {
			exactName = append(exactName, it)
		}
		if strings.HasPrefix(it.ID, q) {
			prefixID = append(prefixID, it)
		}
		if strings.HasPrefix(strings.ToLower(it.Name), strings.ToLower(q)) {
			prefixName = append(prefixName, it)
		}
	}
	switch {
	case len(exactID) == 1:
		return exactID[0], nil
	case len(exactName) == 1:
		return exactName[0], nil
	case len(exactName) > 1:
		return entityRef{}, usagef("ambiguous %s name %q matches %d entries", kind, q, len(exactName))
	case len(prefixID) == 1:
		return prefixID[0], nil
	case len(prefixID) > 1:
		return entityRef{}, usagef("ambiguous %s id prefix %q (%d matches)", kind, q, len(prefixID))
	case len(prefixName) == 1:
		return prefixName[0], nil
	case len(prefixName) > 1:
		return entityRef{}, usagef("ambiguous %s name prefix %q (%d matches)", kind, q, len(prefixName))
	default:
		return entityRef{}, &apiError{Status: 404, Code: "not_found", Message: fmt.Sprintf("%s %q not found", kind, q)}
	}
}

func (c *CLI) resolveFolder(query string) (entityRef, error) {
	items, err := c.listFolders()
	if err != nil {
		return entityRef{}, err
	}
	return resolveRef("folder", query, items)
}

func (c *CLI) resolveRepo(query string) (entityRef, error) {
	items, err := c.listRepos()
	if err != nil {
		return entityRef{}, err
	}
	return resolveRef("repository", query, items)
}

func (c *CLI) resolveJob(query string) (entityRef, error) {
	items, err := c.listJobs()
	if err != nil {
		return entityRef{}, err
	}
	return resolveRef("job", query, items)
}

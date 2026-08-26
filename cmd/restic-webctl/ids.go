package main

import (
	"fmt"
	"strings"

	"restic-web/internal/core"
)

type entityRef struct {
	ID   string
	Name string
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
		return entityRef{}, &apiError{Code: "not_found", Message: fmt.Sprintf("%s %q not found", kind, q)}
	}
}

func (c *CLI) resolveFolder(query string) (core.Folder, error) {
	items := c.app.Folders.List()
	refs := make([]entityRef, 0, len(items))
	for _, it := range items {
		refs = append(refs, entityRef{ID: it.ID, Name: it.Name})
	}
	ref, err := resolveRef("folder", query, refs)
	if err != nil {
		return core.Folder{}, err
	}
	f, ok := c.app.Folders.Get(ref.ID)
	if !ok {
		return core.Folder{}, &apiError{Code: "not_found", Message: fmt.Sprintf("folder %q not found", query)}
	}
	return f, nil
}

func (c *CLI) resolveRepo(query string) (core.Repository, error) {
	items := c.app.Repos.List()
	refs := make([]entityRef, 0, len(items))
	for _, it := range items {
		refs = append(refs, entityRef{ID: it.ID, Name: it.Name})
	}
	ref, err := resolveRef("repository", query, refs)
	if err != nil {
		return core.Repository{}, err
	}
	r, ok := c.app.Repos.Get(ref.ID)
	if !ok {
		return core.Repository{}, &apiError{Code: "not_found", Message: fmt.Sprintf("repository %q not found", query)}
	}
	return r, nil
}

func (c *CLI) resolveJob(query string) (core.Job, error) {
	items := c.app.Jobs.List()
	refs := make([]entityRef, 0, len(items))
	for _, it := range items {
		refs = append(refs, entityRef{ID: it.ID, Name: it.Name})
	}
	ref, err := resolveRef("job", query, refs)
	if err != nil {
		return core.Job{}, err
	}
	j, ok := c.app.Jobs.Get(ref.ID)
	if !ok {
		return core.Job{}, &apiError{Code: "not_found", Message: fmt.Sprintf("job %q not found", query)}
	}
	return j, nil
}

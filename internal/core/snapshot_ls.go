package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
)

// This file backs the snapshot browser: `restic ls --json` lists every node of
// a snapshot, which is indexed once into a directory tree so the UI can walk it
// level by level without asking restic again. Snapshots never change, so a
// listed tree stays valid until the snapshot is forgotten.

// LsNode is one file, directory or link in a snapshot.
type LsNode struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Type  string `json:"type"` // dir | file | symlink | …
	Size  int64  `json:"size"`
	MTime string `json:"mtime,omitempty"`
}

// lsLine mirrors one line of `restic ls --json`: a snapshot header first, then
// one node per line. restic 0.17+ tags lines with message_type, older versions
// with struct_type.
type lsLine struct {
	MessageType string `json:"message_type"`
	StructType  string `json:"struct_type"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	MTime       string `json:"mtime"`
}

// decodeLs parses `restic ls --json` output into nodes.
func decodeLs(out []byte) ([]LsNode, error) {
	var nodes []LsNode
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var l lsLine
		if err := json.Unmarshal(line, &l); err != nil {
			return nil, fmt.Errorf("could not parse restic ls output: %w", err)
		}
		kind := l.MessageType
		if kind == "" {
			kind = l.StructType
		}
		if kind != "node" || l.Path == "" {
			continue
		}
		nodes = append(nodes, LsNode{Path: path.Clean(l.Path), Name: l.Name, Type: l.Type, Size: l.Size, MTime: l.MTime})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("could not read restic ls output: %w", err)
	}
	return nodes, nil
}

// snapshotTree indexes a snapshot's nodes by parent directory. Directory sizes
// are the total size of the files below them.
type snapshotTree struct {
	children map[string][]LsNode
	nodes    map[string]LsNode
}

func buildSnapshotTree(nodes []LsNode) *snapshotTree {
	t := &snapshotTree{children: map[string][]LsNode{}, nodes: map[string]LsNode{}}
	for _, n := range nodes {
		t.nodes[n.Path] = n
	}
	// Parents of the backed-up paths are listed by restic, but make sure every
	// ancestor exists so the tree is walkable from "/".
	for _, n := range nodes {
		for dir := path.Dir(n.Path); dir != "/" && dir != "."; dir = path.Dir(dir) {
			if _, ok := t.nodes[dir]; ok {
				break
			}
			t.nodes[dir] = LsNode{Path: dir, Name: path.Base(dir), Type: "dir"}
		}
	}
	sizes := map[string]int64{}
	for _, n := range t.nodes {
		if n.Type == "dir" {
			continue
		}
		for dir := path.Dir(n.Path); ; dir = path.Dir(dir) {
			sizes[dir] += n.Size
			if dir == "/" || dir == "." {
				break
			}
		}
	}
	for p, n := range t.nodes {
		if n.Type == "dir" {
			n.Size = sizes[p]
			t.nodes[p] = n
		}
		parent := path.Dir(p)
		t.children[parent] = append(t.children[parent], t.nodes[p])
	}
	for _, list := range t.children {
		sort.Slice(list, func(i, j int) bool {
			if (list[i].Type == "dir") != (list[j].Type == "dir") {
				return list[i].Type == "dir"
			}
			return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		})
	}
	return t
}

// List returns the entries directly inside dir, and whether dir exists.
func (t *snapshotTree) List(dir string) ([]LsNode, bool) {
	dir = path.Clean("/" + strings.TrimSpace(dir))
	if dir != "/" {
		n, ok := t.nodes[dir]
		if !ok || n.Type != "dir" {
			return nil, false
		}
	}
	list := t.children[dir]
	if list == nil {
		list = []LsNode{}
	}
	return list, true
}

// Has reports whether p is a node of the snapshot.
func (t *snapshotTree) Has(p string) bool {
	_, ok := t.nodes[path.Clean(p)]
	return ok
}

// snapshotTreeCache keeps the most recently browsed snapshot trees, keyed by
// repository and snapshot id. A few entries are enough for one user clicking
// through a snapshot; a large tree costs memory, so the cache stays small.
type snapshotTreeCache struct {
	mu      sync.Mutex
	order   []string
	entries map[string]*snapshotTree
	max     int
}

func newSnapshotTreeCache() *snapshotTreeCache {
	return &snapshotTreeCache{entries: map[string]*snapshotTree{}, max: 4}
}

func snapshotTreeKey(repoID, snapshotID string) string { return repoID + "/" + snapshotID }

func (c *snapshotTreeCache) Get(key string) (*snapshotTree, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.entries[key]
	if ok {
		c.touch(key)
	}
	return t, ok
}

func (c *snapshotTreeCache) Put(key string, t *snapshotTree) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; !ok && len(c.order) >= c.max {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	c.entries[key] = t
	c.touch(key)
}

func (c *snapshotTreeCache) touch(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
}

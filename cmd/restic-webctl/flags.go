package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// newFlagSet builds a FlagSet that does not call os.Exit on error.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

func parseFlagSet(fs *flag.FlagSet, args []string, usage string) error {
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return usagef("%s", strings.TrimSpace(usage))
		}
		return usagef("%v\n\n%s", err, strings.TrimSpace(usage))
	}
	return nil
}

func parseCSVInts(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil {
			return nil, usagef("invalid integer %q", p)
		}
		out = append(out, n)
	}
	return out, nil
}

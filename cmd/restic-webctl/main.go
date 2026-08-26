// Command restic-webctl is the control CLI for a running restic-web server.
// It talks to the JSON HTTP API (default http://127.0.0.1:8080) so it can
// start/stop runs through the live coordinator — the same surface as the web UI.
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cfg, rest, err := parseGlobal(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n%s\n", err, shortUsage())
		return exitUsage
	}
	if cfg.help && len(rest) == 0 {
		fmt.Fprint(os.Stdout, fullUsage())
		return exitOK
	}
	if len(rest) == 0 {
		fmt.Fprint(os.Stderr, shortUsage())
		return exitUsage
	}

	cli, err := newCLI(cfg)
	if err != nil {
		return cliFail(cfg, err)
	}
	defer cli.close()

	return cli.dispatch(rest)
}

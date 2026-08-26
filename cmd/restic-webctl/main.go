// Command restic-webctl is the control CLI for restic-web. It opens the same
// Postgres-backed core.App as the web server and drives repositories, jobs, and
// runs directly — no HTTP API and no session auth.
package main

import (
	"fmt"
	"os"

	"restic-web/internal/core"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	_ = core.LoadDotEnv(".env")

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
	// Relocatable --help was stripped from args; put it back so
	// "restic-webctl job --help" reaches the command's help path.
	if cfg.help {
		rest = append(rest, "--help")
	}

	// Command help should not require a database connection.
	if wantHelp(rest[1:]) || (len(rest) == 1 && cfg.help) {
		cli := &CLI{cfg: cfg}
		return cli.dispatch(rest)
	}

	cli, err := newCLI(cfg)
	if err != nil {
		return cliFail(cfg, err)
	}
	defer cli.close()

	return cli.dispatch(rest)
}

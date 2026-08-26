package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func (c *CLI) cmdAuth(args []string) int {
	if len(args) == 0 || wantHelp(args) {
		fmt.Fprint(os.Stdout, helpAuth())
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	switch args[0] {
	case "status":
		if len(args) > 1 {
			return c.fail(usagef("unexpected arguments: %v\n\n%s", args[1:], helpAuth()))
		}
		return c.authStatus()
	case "setup":
		return c.authSetup(args[1:])
	case "passwd", "password", "change-password":
		return c.authPasswd(args[1:])
	default:
		return c.fail(usagef("unknown auth command %q\n\n%s", args[0], helpAuth()))
	}
}

func (c *CLI) authStatus() int {
	configured := c.app.Auth.Configured()
	payload := map[string]any{
		"ok":            true,
		"setupRequired": !configured,
		"authenticated": false,
	}
	if c.cfg.json {
		return c.writeJSON(payload)
	}
	fmt.Printf("setupRequired:\t%v\n", !configured)
	fmt.Printf("authenticated:\tN/A (CLI uses database directly)\n")
	return exitOK
}

func (c *CLI) authSetup(args []string) int {
	fs := newFlagSet("auth setup")
	pass := fs.String("password", "", "initial login password")
	if err := parseFlagSet(fs, args, helpAuth()); err != nil {
		return c.fail(err)
	}
	password := *pass
	if password == "" {
		var err error
		password, err = promptPassword("New password: ")
		if err != nil {
			return c.fail(err)
		}
	}
	if err := c.app.Auth.SetupPassword(password); err != nil {
		return c.fail(err)
	}
	return c.okMessage("Password set.", nil)
}

func (c *CLI) authPasswd(args []string) int {
	fs := newFlagSet("auth passwd")
	cur := fs.String("current", "", "current password")
	neu := fs.String("new", "", "new password")
	if err := parseFlagSet(fs, args, helpAuth()); err != nil {
		return c.fail(err)
	}
	current := *cur
	newPass := *neu
	var err error
	if current == "" {
		current, err = promptPassword("Current password: ")
		if err != nil {
			return c.fail(err)
		}
	}
	if newPass == "" {
		newPass, err = promptPassword("New password: ")
		if err != nil {
			return c.fail(err)
		}
	}
	if err := c.app.Auth.ChangePassword(current, newPass); err != nil {
		return c.fail(err)
	}
	return c.okMessage("Password changed.", nil)
}

func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fmt.Fprint(os.Stderr, "(input may echo) ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", usagef("could not read password: %v (set --password)", err)
	}
	s := strings.TrimRight(line, "\r\n")
	if s == "" {
		return "", usagef("password must not be empty")
	}
	return s, nil
}

package main

import (
	"bufio"
	"fmt"
	"net/http"
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
	case "login":
		return c.authLogin(args[1:])
	case "logout":
		if len(args) > 1 {
			return c.fail(usagef("unexpected arguments: %v\n\n%s", args[1:], helpAuth()))
		}
		return c.authLogout()
	case "passwd", "password", "change-password":
		return c.authPasswd(args[1:])
	default:
		return c.fail(usagef("unknown auth command %q\n\n%s", args[0], helpAuth()))
	}
}

func (c *CLI) authStatus() int {
	status, m, err := c.doJSONOnce(http.MethodGet, "/api/auth/status", nil)
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	if c.cfg.json {
		return c.writeJSONRaw(m)
	}
	fmt.Printf("setupRequired:\t%v\n", boolField(m, "setupRequired"))
	fmt.Printf("authenticated:\t%v\n", boolField(m, "authenticated"))
	return exitOK
}

func (c *CLI) authSetup(args []string) int {
	fs := newFlagSet("auth setup")
	pass := fs.String("password", c.cfg.password, "initial login password")
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
	if err := c.setup(password); err != nil {
		return c.fail(err)
	}
	return c.okMessage("Password set; session saved.", nil)
}

func (c *CLI) authLogin(args []string) int {
	fs := newFlagSet("auth login")
	pass := fs.String("password", c.cfg.password, "login password")
	if err := parseFlagSet(fs, args, helpAuth()); err != nil {
		return c.fail(err)
	}
	password := *pass
	if password == "" {
		var err error
		password, err = promptPassword("Password: ")
		if err != nil {
			return c.fail(err)
		}
	}
	if err := c.login(password); err != nil {
		return c.fail(err)
	}
	return c.okMessage("Logged in; session saved.", nil)
}

func (c *CLI) authLogout() int {
	_, _, _ = c.doJSONOnce(http.MethodPost, "/api/auth/logout", map[string]any{})
	c.clearSession()
	return c.okMessage("Logged out.", nil)
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
	status, m, err := c.doJSON(http.MethodPost, "/api/auth/password", map[string]string{
		"currentPassword": current,
		"newPassword":     newPass,
	})
	if err != nil {
		return c.fail(err)
	}
	if err := c.requireOK(status, m); err != nil {
		return c.fail(err)
	}
	return c.okMessage("Password changed.", nil)
}

func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fmt.Fprint(os.Stderr, "(input may echo) ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", usagef("could not read password: %v (set --password or RESTIC_WEB_PASSWORD)", err)
	}
	s := strings.TrimRight(line, "\r\n")
	if s == "" {
		return "", usagef("password must not be empty; set --password or RESTIC_WEB_PASSWORD")
	}
	return s, nil
}

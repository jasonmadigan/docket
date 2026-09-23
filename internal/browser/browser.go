package browser

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
)

// Remote is true in an ssh session, where a browser would open on the
// wrong machine.
func Remote() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
}

// Valid admits only web links: check urls come from third-party apps.
func Valid(link string) bool {
	u, err := url.Parse(link)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func Open(link string) error {
	if !Valid(link) {
		return fmt.Errorf("refusing to open %q", link)
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, link).Run()
}

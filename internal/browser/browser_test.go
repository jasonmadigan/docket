package browser

import "testing"

func TestValid(t *testing.T) {
	for link, want := range map[string]bool{
		"https://github.com/acme/a/pull/1": true,
		"http://ci.example/run/7":          true,
		"javascript:alert(1)":              false,
		"file:///etc/passwd":               false,
		"ssh://host":                       false,
		"https://bad\x7f":                  false,
		"https:///no-host":                 false,
		"":                                 false,
	} {
		if got := Valid(link); got != want {
			t.Errorf("Valid(%q) = %v, want %v", link, got, want)
		}
	}
}

func TestOpenRefusesInvalidLinks(t *testing.T) {
	if err := Open("file:///etc/passwd"); err == nil {
		t.Fatal("Open accepted a file link")
	}
}

func TestRemote(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	if Remote() {
		t.Fatal("remote without ssh variables")
	}
	t.Setenv("SSH_TTY", "/dev/ttys001")
	if !Remote() {
		t.Fatal("not remote with SSH_TTY set")
	}
}

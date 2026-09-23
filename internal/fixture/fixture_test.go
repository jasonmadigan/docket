package fixture

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// goldens must survive end-of-file and trailing-whitespace fixers.
func TestGoldenFilesAreLintClean(t *testing.T) {
	trailing := regexp.MustCompile(`(?m)[ \t]+$`)
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".golden") {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(string(body), "\n") {
			t.Errorf("%s: no final newline", path)
		}
		if trailing.Match(body) {
			t.Errorf("%s: trailing whitespace", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

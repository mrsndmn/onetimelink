package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gjfy no longer sends email notifications, and with them went the only place
// that started an external process. Keeping it that way removes a whole class
// of problems (argument injection, unsanitised client data reaching a shell,
// fork storms under load), so this test fails if a subprocess or mail
// dependency creeps back in.
func TestNoSubprocessOrMailDependency(t *testing.T) {
	forbidden := []string{`"os/exec"`, `"net/smtp"`}

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, f := range forbidden {
			if strings.Contains(string(src), f) {
				t.Errorf("%s imports %s: gjfy must not start subprocesses or send mail", path, f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

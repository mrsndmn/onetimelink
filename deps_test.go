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

// gjfy builds on the standard library alone. For a service that holds secrets
// in memory, every third party package is supply chain surface: a compromised
// release of any of them runs in the same process as the secrets.
//
// Upstream pulled in cobra (plus pflag, and mousetrap in the module graph) to
// parse six flags — around 12k lines of foreign code against 1.6k of our own.
// If a dependency is ever added back, it should be a deliberate decision, so
// this test makes it a loud one.
func TestNoExternalDependencies(t *testing.T) {
	src, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "require") {
			t.Errorf("go.mod has a require directive: %q", line)
		}
	}

	// go.sum only exists once something is downloaded.
	if info, err := os.Stat("go.sum"); err == nil && info.Size() > 0 {
		t.Errorf("go.sum is not empty (%d bytes): a dependency crept in", info.Size())
	}
}

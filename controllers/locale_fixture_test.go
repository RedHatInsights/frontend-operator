package controllers

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"sigs.k8s.io/yaml"
)

// The fixture under tests/fixtures/localization is shared with Chrome
// (RHCLOUD-52084) and chrome-service (RHCLOUD-52093). Regenerate the expected
// files with UPDATE_FIXTURES=1 go test ./controllers -run TestLocaleFixture,
// then review the diff: it is the contract.
const localeFixtureDir = "../tests/fixtures/localization"

func loadYAMLFixture(t *testing.T, name string, into any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(localeFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.UnmarshalStrict(data, into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func checkJSONFixture(t *testing.T, name string, got any) {
	t.Helper()
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(localeFixtureDir, name)
	if os.Getenv("UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, data) {
		t.Errorf("%s differs from generated output:\n%s", name, data)
	}
}

func TestLocaleFixture(t *testing.T) {
	env := &crd.FrontendEnvironment{}
	loadYAMLFixture(t, "environment.yaml", env)
	fe := crd.Frontend{}
	loadYAMLFixture(t, "frontend.yaml", &fe)
	feList := &crd.FrontendList{Items: []crd.Frontend{fe}}

	bundles, _, err := setupBundlesData(feList, *env)
	if err != nil {
		t.Fatal(err)
	}
	checkJSONFixture(t, "bundles.json", bundles)
	localized := len(env.Spec.EnabledLocales) > 0
	checkJSONFixture(t, "widget-registry.json", setupWidgetRegistry(feList, localized))
	checkJSONFixture(t, "base-widget-dashboard-templates.json", setupBaseWidgetDashboardTemplates(feList, localized))

	for _, locale := range []string{"fr", "ja"} {
		built := buildLocaleMessages(locale, feList, env)
		if len(built.Collisions) > 0 || len(built.Unknown) > 0 {
			t.Errorf("%s: unexpected collisions %v or unknown keys %v", locale, built.Collisions, built.Unknown)
		}
		checkJSONFixture(t, messagesFileName(locale), built.Messages)
	}

	advertised := map[string]bool{"en": true, "fr": true, "ja": true}
	checkJSONFixture(t, "locales.json", localesJSONEntries(env.Spec.EnabledLocales, advertised))
}

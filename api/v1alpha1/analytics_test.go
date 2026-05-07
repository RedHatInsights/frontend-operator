package v1alpha1

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAnalyticsJSONRoundTrip(t *testing.T) {
	analytics := Analytics{
		APIKey:               "prod-key",
		APIKeyDev:            "dev-key",
		AutocaptureAPIKey:    "autocap-prod",
		AutocaptureAPIKeyDev: "autocap-dev",
	}

	data, err := json.Marshal(analytics)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal to map failed: %v", err)
	}

	expected := map[string]string{
		"APIKey":               "prod-key",
		"APIKeyDev":            "dev-key",
		"autocaptureAPIKey":    "autocap-prod",
		"autocaptureAPIKeyDev": "autocap-dev",
	}
	for key, want := range expected {
		got, ok := parsed[key]
		if !ok {
			t.Errorf("missing key %q in JSON output", key)
			continue
		}
		if got != want {
			t.Errorf("key %q = %q, want %q", key, got, want)
		}
	}

	var roundTripped Analytics
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("json.Unmarshal to struct failed: %v", err)
	}
	if roundTripped != analytics {
		t.Errorf("round-trip mismatch: got %+v, want %+v", roundTripped, analytics)
	}
}

func TestAnalyticsJSONOmitsEmptyAutocaptureFields(t *testing.T) {
	analytics := Analytics{
		APIKey:    "prod-key",
		APIKeyDev: "dev-key",
	}

	data, err := json.Marshal(analytics)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	for _, key := range []string{"autocaptureAPIKey", "autocaptureAPIKeyDev"} {
		if _, ok := parsed[key]; ok {
			t.Errorf("expected %q to be omitted when empty, but it was present", key)
		}
	}
}

func TestAnalyticsYAMLRoundTrip(t *testing.T) {
	analytics := Analytics{
		APIKey:               "prod-key",
		APIKeyDev:            "dev-key",
		AutocaptureAPIKey:    "autocap-prod",
		AutocaptureAPIKeyDev: "autocap-dev",
	}

	data, err := yaml.Marshal(analytics)
	if err != nil {
		t.Fatalf("yaml.Marshal failed: %v", err)
	}

	var roundTripped Analytics
	if err := yaml.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}
	if roundTripped != analytics {
		t.Errorf("YAML round-trip mismatch: got %+v, want %+v", roundTripped, analytics)
	}
}

func TestAnalyticsBackwardCompatibility(t *testing.T) {
	// Existing JSON without new fields should unmarshal without error
	input := `{"APIKey":"prod-key","APIKeyDev":"dev-key"}`
	var analytics Analytics
	if err := json.Unmarshal([]byte(input), &analytics); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if analytics.APIKey != "prod-key" {
		t.Errorf("APIKey = %q, want %q", analytics.APIKey, "prod-key")
	}
	if analytics.APIKeyDev != "dev-key" {
		t.Errorf("APIKeyDev = %q, want %q", analytics.APIKeyDev, "dev-key")
	}
	if analytics.AutocaptureAPIKey != "" {
		t.Errorf("AutocaptureAPIKey should be empty, got %q", analytics.AutocaptureAPIKey)
	}
	if analytics.AutocaptureAPIKeyDev != "" {
		t.Errorf("AutocaptureAPIKeyDev should be empty, got %q", analytics.AutocaptureAPIKeyDev)
	}
}

func TestFedModuleAnalyticsInJSON(t *testing.T) {
	// Verify analytics with autocapture fields appears in FedModule JSON
	module := FedModule{
		ManifestLocation: "/apps/test/fed-mods.json",
		Analytics: &Analytics{
			APIKey:               "prod-key",
			APIKeyDev:            "dev-key",
			AutocaptureAPIKey:    "autocap-prod",
			AutocaptureAPIKeyDev: "autocap-dev",
		},
	}

	data, err := json.Marshal(module)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	analyticsData, ok := parsed["analytics"].(map[string]interface{})
	if !ok {
		t.Fatal("analytics field missing or not an object in FedModule JSON")
	}

	expected := map[string]string{
		"APIKey":               "prod-key",
		"APIKeyDev":            "dev-key",
		"autocaptureAPIKey":    "autocap-prod",
		"autocaptureAPIKeyDev": "autocap-dev",
	}
	for key, want := range expected {
		got, ok := analyticsData[key]
		if !ok {
			t.Errorf("analytics missing key %q", key)
			continue
		}
		if got != want {
			t.Errorf("analytics[%q] = %q, want %q", key, got, want)
		}
	}
}

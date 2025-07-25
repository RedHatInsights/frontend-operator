package controllers

import (
	"encoding/json"
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetupAPISpecs(t *testing.T) {
	// Create test frontend list with API specs to test sorting
	feList := &crd.FrontendList{
		Items: []crd.Frontend{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-frontend-1",
				},
				Spec: crd.FrontendSpec{
					API: &crd.APIInfo{
						Specs: []crd.APISpecInfo{
							{
								URL:          "https://console.redhat.com/api/test1/v2/openapi.json",
								BundleLabels: []string{"insights"},
								ServiceRef:   "service-b",
							},
							{
								URL:          "https://console.redhat.com/api/test1/v1/openapi.json",
								BundleLabels: []string{"insights"},
								ServiceRef:   "service-a",
							},
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-frontend-2",
				},
				Spec: crd.FrontendSpec{
					API: &crd.APIInfo{
						Specs: []crd.APISpecInfo{
							{
								URL:          "https://console.redhat.com/api/test2/v3/openapi.json",
								BundleLabels: []string{"ansible"},
								ServiceRef:   "", // Empty serviceRef - should be sorted last
							},
							{
								URL:          "https://console.redhat.com/api/test2/v1/openapi.json",
								BundleLabels: []string{"ansible"},
								ServiceRef:   "", // Empty serviceRef - should be sorted last
							},
							{
								URL:          "https://console.redhat.com/api/test2/v2/openapi.json",
								BundleLabels: []string{"ansible"},
								ServiceRef:   "service-a",
							},
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-frontend-3",
				},
				Spec: crd.FrontendSpec{
					// No API field - should be skipped
				},
			},
		},
	}

	apiSpecs := setupAPISpecs(feList)

	// Convert to JSON for easier inspection
	jsonData, err := json.MarshalIndent(apiSpecs, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal result: %v", err)
	}

	t.Logf("Generated API specs config:\n%s", string(jsonData))

	// Basic validation
	if apiSpecs == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should have 4 API specs total (2 from frontend-1, 3 from frontend-2, 0 from frontend-3)
	expectedSpecs := 5
	if len(apiSpecs) != expectedSpecs {
		t.Errorf("Expected %d API specs, got %d", expectedSpecs, len(apiSpecs))
	}

	// Test sorting: should be sorted by ServiceRef (empty last), then by URL
	expectedOrder := []struct {
		ServiceRef string
		URL        string
	}{
		{"service-a", "https://console.redhat.com/api/test1/v1/openapi.json"},
		{"service-a", "https://console.redhat.com/api/test2/v2/openapi.json"},
		{"service-b", "https://console.redhat.com/api/test1/v2/openapi.json"},
		{"", "https://console.redhat.com/api/test2/v1/openapi.json"},
		{"", "https://console.redhat.com/api/test2/v3/openapi.json"},
	}

	if len(apiSpecs) != len(expectedOrder) {
		t.Fatalf("Expected %d specs, got %d", len(expectedOrder), len(apiSpecs))
	}

	for i, expected := range expectedOrder {
		if apiSpecs[i].ServiceRef != expected.ServiceRef {
			t.Errorf("Position %d: expected ServiceRef '%s', got '%s'", i, expected.ServiceRef, apiSpecs[i].ServiceRef)
		}
		if apiSpecs[i].URL != expected.URL {
			t.Errorf("Position %d: expected URL '%s', got '%s'", i, expected.URL, apiSpecs[i].URL)
		}
	}

	// Test that all specs have the expected properties
	for i, spec := range apiSpecs {
		if spec.URL == "" {
			t.Errorf("Spec %d has empty URL", i)
		}
		if len(spec.BundleLabels) == 0 {
			t.Errorf("Spec %d has no bundle labels", i)
		}
	}
}

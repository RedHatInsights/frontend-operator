package controllers

import (
	"testing"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSortNavItemsByPosition(t *testing.T) {
	tests := []struct {
		name     string
		input    []crd.ChromeNavItem
		expected []crd.ChromeNavItem
	}{
		{
			name: "sorts by position ascending",
			input: []crd.ChromeNavItem{
				{ID: "item3", Position: uintPtr(500)},
				{ID: "item1", Position: uintPtr(100)},
				{ID: "item2", Position: uintPtr(300)},
			},
			expected: []crd.ChromeNavItem{
				{ID: "item1", Position: uintPtr(100)},
				{ID: "item2", Position: uintPtr(300)},
				{ID: "item3", Position: uintPtr(500)},
			},
		},
		{
			name: "sorts by ID when positions are equal",
			input: []crd.ChromeNavItem{
				{ID: "inventory", Position: uintPtr(300)},
				{ID: "imageBuilder", Position: uintPtr(300)},
				{ID: "advisor", Position: uintPtr(300)},
			},
			expected: []crd.ChromeNavItem{
				{ID: "advisor", Position: uintPtr(300)},
				{ID: "imageBuilder", Position: uintPtr(300)},
				{ID: "inventory", Position: uintPtr(300)},
			},
		},
		{
			name: "handles mixed positions and equal positions",
			input: []crd.ChromeNavItem{
				{ID: "operations", Position: uintPtr(500)},
				{ID: "inventory", Position: uintPtr(300)},
				{ID: "imageBuilder", Position: uintPtr(300)},
				{ID: "overview", Position: uintPtr(100)},
			},
			expected: []crd.ChromeNavItem{
				{ID: "overview", Position: uintPtr(100)},
				{ID: "imageBuilder", Position: uintPtr(300)},
				{ID: "inventory", Position: uintPtr(300)},
				{ID: "operations", Position: uintPtr(500)},
			},
		},
		{
			name: "handles nil positions (defaults to 0)",
			input: []crd.ChromeNavItem{
				{ID: "item2", Position: uintPtr(300)},
				{ID: "item1", Position: nil},
				{ID: "item3", Position: uintPtr(100)},
			},
			expected: []crd.ChromeNavItem{
				{ID: "item1", Position: nil},
				{ID: "item3", Position: uintPtr(100)},
				{ID: "item2", Position: uintPtr(300)},
			},
		},
		{
			name: "sorts nested Routes recursively",
			input: []crd.ChromeNavItem{
				{
					ID:         "parent",
					Position:   uintPtr(100),
					Expandable: true,
					Routes: []crd.ChromeNavItem{
						{ID: "route2", Position: uintPtr(300)},
						{ID: "route1", Position: uintPtr(100)},
					},
				},
			},
			expected: []crd.ChromeNavItem{
				{
					ID:         "parent",
					Position:   uintPtr(100),
					Expandable: true,
					Routes: []crd.ChromeNavItem{
						{ID: "route1", Position: uintPtr(100)},
						{ID: "route2", Position: uintPtr(300)},
					},
				},
			},
		},
		{
			name: "sorts nested NavItems recursively",
			input: []crd.ChromeNavItem{
				{
					ID:       "group",
					GroupID:  "test-group",
					Position: uintPtr(100),
					NavItems: []crd.ChromeNavItem{
						{ID: "nav2", Position: uintPtr(300)},
						{ID: "nav1", Position: uintPtr(300)},
					},
				},
			},
			expected: []crd.ChromeNavItem{
				{
					ID:       "group",
					GroupID:  "test-group",
					Position: uintPtr(100),
					NavItems: []crd.ChromeNavItem{
						{ID: "nav1", Position: uintPtr(300)},
						{ID: "nav2", Position: uintPtr(300)},
					},
				},
			},
		},
		{
			name: "complex nested structure with multiple levels",
			input: []crd.ChromeNavItem{
				{
					ID:         "parent2",
					Position:   uintPtr(300),
					Expandable: true,
					Routes: []crd.ChromeNavItem{
						{ID: "zroute", Position: uintPtr(300)},
						{ID: "aroute", Position: uintPtr(300)},
					},
				},
				{
					ID:         "parent1",
					Position:   uintPtr(100),
					Expandable: true,
					Routes: []crd.ChromeNavItem{
						{ID: "route2", Position: uintPtr(200)},
						{ID: "route1", Position: uintPtr(100)},
					},
				},
			},
			expected: []crd.ChromeNavItem{
				{
					ID:         "parent1",
					Position:   uintPtr(100),
					Expandable: true,
					Routes: []crd.ChromeNavItem{
						{ID: "route1", Position: uintPtr(100)},
						{ID: "route2", Position: uintPtr(200)},
					},
				},
				{
					ID:         "parent2",
					Position:   uintPtr(300),
					Expandable: true,
					Routes: []crd.ChromeNavItem{
						{ID: "aroute", Position: uintPtr(300)},
						{ID: "zroute", Position: uintPtr(300)},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sortNavItemsByPosition(tt.input)

			// Compare the results
			if !compareNavItems(result, tt.expected) {
				t.Errorf("sortNavItemsByPosition() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// uintPtr returns a pointer to a uint
func uintPtr(u uint) *uint {
	return &u
}

// compareNavItems recursively compares two slices of ChromeNavItem
func compareNavItems(a, b []crd.ChromeNavItem) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}

		// Compare positions
		if (a[i].Position == nil) != (b[i].Position == nil) {
			return false
		}
		if a[i].Position != nil && b[i].Position != nil && *a[i].Position != *b[i].Position {
			return false
		}

		// Recursively compare nested items
		if a[i].Expandable != b[i].Expandable {
			return false
		}
		if a[i].Expandable && !compareNavItems(a[i].Routes, b[i].Routes) {
			return false
		}

		if a[i].GroupID != b[i].GroupID {
			return false
		}
		if len(a[i].NavItems) > 0 || len(b[i].NavItems) > 0 {
			if !compareNavItems(a[i].NavItems, b[i].NavItems) {
				return false
			}
		}
	}

	return true
}

// TestSetupBundlesDataSorting tests the full setupBundlesData function with realistic data
// This test replicates the issue found in production where items with the same position
// were appearing in different orders in different reconciliations
func TestSetupBundlesDataSorting(t *testing.T) {
	// Create test data that mimics the production issue:
	// imageBuilder and Inventory both have position 300
	feEnvironment := crd.FrontendEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-env",
		},
		Spec: crd.FrontendEnvironmentSpec{
			Bundles: &[]crd.FrontendBundles{
				{
					ID:          "insights",
					Title:       "Insights",
					Description: "Red Hat Insights",
				},
			},
		},
	}

	frontendList := &crd.FrontendList{
		Items: []crd.Frontend{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "image-builder",
				},
				Spec: crd.FrontendSpec{
					FeoConfigEnabled: true,
					BundleSegments: []*crd.BundleSegment{
						{
							BundleID:  "insights",
							SegmentID: "imageBuilder",
							Position:  300,
							NavItems: &[]crd.ChromeNavItem{
								{
									Href:    "/insights/image-builder",
									Title:   "Image builder",
									ID:      "imageBuilder",
									Product: "Red Hat Insights",
								},
							},
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "inventory",
				},
				Spec: crd.FrontendSpec{
					FeoConfigEnabled: true,
					BundleSegments: []*crd.BundleSegment{
						{
							BundleID:  "insights",
							SegmentID: "inventory",
							Position:  300,
							NavItems: &[]crd.ChromeNavItem{
								{
									Href:    "/insights/inventory",
									Title:   "Inventory",
									ID:      "Inventory",
									Product: "Red Hat Insights",
								},
							},
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "advisor",
				},
				Spec: crd.FrontendSpec{
					FeoConfigEnabled: true,
					BundleSegments: []*crd.BundleSegment{
						{
							BundleID:  "insights",
							SegmentID: "advisor",
							Position:  100,
							NavItems: &[]crd.ChromeNavItem{
								{
									Href:    "/insights/advisor",
									Title:   "Advisor",
									ID:      "advisor",
									Product: "Red Hat Insights",
								},
							},
						},
					},
				},
			},
		},
	}

	// Run setupBundlesData multiple times to ensure consistent ordering
	for i := 0; i < 5; i++ {
		bundles, _, err := setupBundlesData(frontendList, feEnvironment)
		if err != nil {
			t.Fatalf("setupBundlesData() error = %v", err)
		}

		if len(bundles) != 1 {
			t.Fatalf("Expected 1 bundle, got %d", len(bundles))
		}

		insightsBundle := bundles[0]
		if len(insightsBundle.NavItems) != 3 {
			t.Fatalf("Expected 3 nav items, got %d", len(insightsBundle.NavItems))
		}

		// Verify the items are sorted by position first, then by ID
		// Expected order: advisor (100), Inventory (300, ID starts with 'I'), imageBuilder (300, ID starts with 'i')
		expectedOrder := []string{"advisor", "Inventory", "imageBuilder"}
		for j, navItem := range insightsBundle.NavItems {
			if navItem.ID != expectedOrder[j] {
				t.Errorf("Iteration %d: NavItem at position %d has ID %s, expected %s", i, j, navItem.ID, expectedOrder[j])
			}
		}

		// Verify positions
		if *insightsBundle.NavItems[0].Position != 100 {
			t.Errorf("First item should have position 100, got %d", *insightsBundle.NavItems[0].Position)
		}
		if *insightsBundle.NavItems[1].Position != 300 {
			t.Errorf("Second item should have position 300, got %d", *insightsBundle.NavItems[1].Position)
		}
		if *insightsBundle.NavItems[2].Position != 300 {
			t.Errorf("Third item should have position 300, got %d", *insightsBundle.NavItems[2].Position)
		}
	}
}

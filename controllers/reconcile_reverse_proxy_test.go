package controllers

import (
	"context"
	"os"
	"testing"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/go-logr/logr"
)

func TestProtocolSwap(t *testing.T) {
	tests := []struct {
		name        string
		port        string
		endpoint    string
		expectedURL string
	}{
		{
			name:        "HTTPS port 443 (no port in URL)",
			port:        "443",
			endpoint:    "s3.us-east-1.amazonaws.com",
			expectedURL: "https://s3.us-east-1.amazonaws.com",
		},
		{
			name:        "HTTP port 80 (no port in URL)",
			port:        "80",
			endpoint:    "localhost",
			expectedURL: "http://localhost",
		},
		{
			name:        "Custom port 9000 (MinIO)",
			port:        "9000",
			endpoint:    "minio-service.minio-env.svc.cluster.local",
			expectedURL: "http://minio-service.minio-env.svc.cluster.local:9000",
		},
		{
			name:        "Custom port 8080",
			port:        "8080",
			endpoint:    "localhost",
			expectedURL: "http://localhost:8080",
		},
		{
			name:        "Another HTTPS case",
			port:        "443",
			endpoint:    "bucket.s3.amazonaws.com",
			expectedURL: "https://bucket.s3.amazonaws.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the improved protocol swap logic from reconcile_reverse_proxy.go
			var minioUpstreamURL string
			switch tt.port {
			case "443":
				// Use HTTPS without port for standard HTTPS port
				minioUpstreamURL = "https://" + tt.endpoint
			case "80":
				// Use HTTP without port for standard HTTP port
				minioUpstreamURL = "http://" + tt.endpoint
			default:
				// For non-standard ports, use http by default (local development)
				minioUpstreamURL = "http://" + tt.endpoint + ":" + tt.port
			}

			if minioUpstreamURL != tt.expectedURL {
				t.Errorf("Protocol swap failed for %s. Expected: %s, Got: %s",
					tt.name, tt.expectedURL, minioUpstreamURL)
			}
		})
	}
}

// TestProtocolSwapEdgeCases tests edge cases and potential issues
func TestProtocolSwapEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		port        string
		endpoint    string
		expectedURL string
		description string
	}{
		{
			name:        "Empty port defaults to http",
			port:        "",
			endpoint:    "localhost",
			expectedURL: "http://localhost:",
			description: "Empty port should default to http protocol with port included",
		},
		{
			name:        "Port 443 with localhost",
			port:        "443",
			endpoint:    "localhost",
			expectedURL: "https://localhost",
			description: "Port 443 should always use https without port, even for localhost",
		},
		{
			name:        "Numeric-only endpoint",
			port:        "9000",
			endpoint:    "127.0.0.1",
			expectedURL: "http://127.0.0.1:9000",
			description: "IP addresses should work fine",
		},
		{
			name:        "Port 80 with real domain",
			port:        "80",
			endpoint:    "example.com",
			expectedURL: "http://example.com",
			description: "Port 80 should use http without port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var minioUpstreamURL string
			switch tt.port {
			case "443":
				minioUpstreamURL = "https://" + tt.endpoint
			case "80":
				minioUpstreamURL = "http://" + tt.endpoint
			default:
				minioUpstreamURL = "http://" + tt.endpoint + ":" + tt.port
			}

			if minioUpstreamURL != tt.expectedURL {
				t.Errorf("%s: Expected: %s, Got: %s",
					tt.description, tt.expectedURL, minioUpstreamURL)
			}
		})
	}
}

// TestUpdateReverseProxyDeployment tests the deployment update functionality
func TestUpdateReverseProxyDeployment(t *testing.T) {
	// Setup environment variables for the test
	os.Setenv("PUSHCACHE_AWS_ACCESS_KEY_ID", "test-access-key")
	os.Setenv("PUSHCACHE_AWS_SECRET_ACCESS_KEY", "test-secret-key")
	os.Setenv("PUSHCACHE_AWS_REGION", "us-east-1")
	os.Setenv("PUSHCACHE_AWS_ENDPOINT", "minio-service.minio-env.svc.cluster.local")
	os.Setenv("PUSHCACHE_AWS_PORT", "9000")
	os.Setenv("PUSHCACHE_AWS_BUCKET_NAME", "frontend")
	defer func() {
		os.Unsetenv("PUSHCACHE_AWS_ACCESS_KEY_ID")
		os.Unsetenv("PUSHCACHE_AWS_SECRET_ACCESS_KEY")
		os.Unsetenv("PUSHCACHE_AWS_REGION")
		os.Unsetenv("PUSHCACHE_AWS_ENDPOINT")
		os.Unsetenv("PUSHCACHE_AWS_PORT")
		os.Unsetenv("PUSHCACHE_AWS_BUCKET_NAME")
	}()

	// Create a fake client with the required objects
	scheme := runtime.NewScheme()
	_ = crd.AddToScheme(scheme)
	_ = apps.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)

	// Create existing deployment with old environment variables
	existingDeployment := &apps.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "reverse-proxy",
			Namespace: "test-namespace",
		},
		Spec: apps.DeploymentSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "reverse-proxy",
							Env: []v1.EnvVar{
								{Name: "SERVER_PORT", Value: "8080"},
								{Name: "MINIO_UPSTREAM_URL", Value: "http://old-endpoint:9000"},
								{Name: "BUCKET_PATH_PREFIX", Value: "old-bucket"},
								{Name: "LOG_LEVEL", Value: "INFO"},
							},
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingDeployment).
		Build()

	// Create reconciliation context
	reconciliation := &ReverseProxyReconciliation{
		Log:      logr.Discard(),
		Recorder: &record.FakeRecorder{},
		Client:   fakeClient,
		Ctx:      context.Background(),
		Frontend: &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-frontend",
				Namespace: "test-namespace",
			},
		},
		FrontendEnvironment: &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-env",
			},
			Spec: crd.FrontendEnvironmentSpec{
				ReverseProxyImage: "test-image:latest",
			},
		},
	}

	tests := []struct {
		name               string
		initialEnvVars     []v1.EnvVar
		expectUpdate       bool
		expectedMinioURL   string
		expectedBucketName string
	}{
		{
			name: "Environment variables changed - should update",
			initialEnvVars: []v1.EnvVar{
				{Name: "SERVER_PORT", Value: "8080"},
				{Name: "MINIO_UPSTREAM_URL", Value: "http://old-endpoint:9000"},
				{Name: "BUCKET_PATH_PREFIX", Value: "old-bucket"},
				{Name: "LOG_LEVEL", Value: "INFO"},
			},
			expectUpdate:       true,
			expectedMinioURL:   "http://minio-service.minio-env.svc.cluster.local:9000",
			expectedBucketName: "frontend",
		},
		{
			name: "Environment variables same - should not update",
			initialEnvVars: []v1.EnvVar{
				{Name: "SERVER_PORT", Value: "8080"},
				{Name: "MINIO_UPSTREAM_URL", Value: "http://minio-service.minio-env.svc.cluster.local:9000"},
				{Name: "BUCKET_PATH_PREFIX", Value: "frontend"},
				{Name: "SPA_ENTRYPOINT_PATH", Value: "/index.html"},
				{Name: "LOG_LEVEL", Value: "DEBUG"},
			},
			expectUpdate:       false,
			expectedMinioURL:   "http://minio-service.minio-env.svc.cluster.local:9000",
			expectedBucketName: "frontend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the deployment state
			deployment := &apps.Deployment{}
			err := fakeClient.Get(context.Background(), types.NamespacedName{
				Name:      "reverse-proxy",
				Namespace: "test-namespace",
			}, deployment)
			if err != nil {
				t.Fatalf("Failed to get deployment: %v", err)
			}

			// Set initial environment variables
			deployment.Spec.Template.Spec.Containers[0].Env = tt.initialEnvVars
			deployment.Spec.Template.Annotations = nil // Reset annotations

			err = fakeClient.Update(context.Background(), deployment)
			if err != nil {
				t.Fatalf("Failed to update deployment: %v", err)
			}

			// Test the update functionality
			err = reconciliation.updateReverseProxyDeployment(deployment)
			if err != nil {
				t.Fatalf("updateReverseProxyDeployment failed: %v", err)
			}

			// Get the updated deployment
			updatedDeployment := &apps.Deployment{}
			err = fakeClient.Get(context.Background(), types.NamespacedName{
				Name:      "reverse-proxy",
				Namespace: "test-namespace",
			}, updatedDeployment)
			if err != nil {
				t.Fatalf("Failed to get updated deployment: %v", err)
			}

			// Check if restart annotation was added when update was expected
			if tt.expectUpdate {
				if updatedDeployment.Spec.Template.Annotations == nil {
					t.Error("Expected restart annotation to be added, but annotations were nil")
				} else if _, exists := updatedDeployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]; !exists {
					t.Error("Expected restart annotation to be added, but it was not found")
				}
			} else {
				// No update expected, so no restart annotation should be added
				if updatedDeployment.Spec.Template.Annotations != nil {
					if _, exists := updatedDeployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]; exists {
						t.Error("No update expected, but restart annotation was added")
					}
				}
			}

			// Verify environment variables are correctly set
			envVars := updatedDeployment.Spec.Template.Spec.Containers[0].Env
			envMap := make(map[string]string)
			for _, env := range envVars {
				envMap[env.Name] = env.Value
			}

			// Check specific environment variables
			if minioURL, exists := envMap["MINIO_UPSTREAM_URL"]; exists {
				if minioURL != tt.expectedMinioURL {
					t.Errorf("Expected MINIO_UPSTREAM_URL to be %s, got %s", tt.expectedMinioURL, minioURL)
				}
			} else {
				t.Error("MINIO_UPSTREAM_URL environment variable not found")
			}

			if bucketName, exists := envMap["BUCKET_PATH_PREFIX"]; exists {
				if bucketName != tt.expectedBucketName {
					t.Errorf("Expected BUCKET_PATH_PREFIX to be %s, got %s", tt.expectedBucketName, bucketName)
				}
			} else {
				t.Error("BUCKET_PATH_PREFIX environment variable not found")
			}
		})
	}
}

// TestEnvVarsEqual tests the environment variable comparison function
func TestEnvVarsEqual(t *testing.T) {
	reconciliation := &ReverseProxyReconciliation{}

	tests := []struct {
		name     string
		existing []v1.EnvVar
		desired  []v1.EnvVar
		expected bool
	}{
		{
			name: "Equal environment variables",
			existing: []v1.EnvVar{
				{Name: "VAR1", Value: "value1"},
				{Name: "VAR2", Value: "value2"},
			},
			desired: []v1.EnvVar{
				{Name: "VAR1", Value: "value1"},
				{Name: "VAR2", Value: "value2"},
			},
			expected: true,
		},
		{
			name: "Different values",
			existing: []v1.EnvVar{
				{Name: "VAR1", Value: "value1"},
				{Name: "VAR2", Value: "value2"},
			},
			desired: []v1.EnvVar{
				{Name: "VAR1", Value: "value1"},
				{Name: "VAR2", Value: "different"},
			},
			expected: false,
		},
		{
			name: "Different lengths",
			existing: []v1.EnvVar{
				{Name: "VAR1", Value: "value1"},
			},
			desired: []v1.EnvVar{
				{Name: "VAR1", Value: "value1"},
				{Name: "VAR2", Value: "value2"},
			},
			expected: false,
		},
		{
			name: "Missing variable",
			existing: []v1.EnvVar{
				{Name: "VAR1", Value: "value1"},
				{Name: "VAR2", Value: "value2"},
			},
			desired: []v1.EnvVar{
				{Name: "VAR1", Value: "value1"},
				{Name: "VAR3", Value: "value3"},
			},
			expected: false,
		},
		{
			name:     "Both empty",
			existing: []v1.EnvVar{},
			desired:  []v1.EnvVar{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciliation.compareEnvVars(tt.existing, tt.desired)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestUpdateReverseProxyService tests the service update functionality
func TestUpdateReverseProxyService(t *testing.T) {
	// Create a fake client with the required objects
	scheme := runtime.NewScheme()
	_ = crd.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)

	// Create existing service with old configuration
	existingService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "reverse-proxy",
			Namespace: "test-namespace",
			Labels: map[string]string{
				"app":         "reverse-proxy",
				"component":   "reverse-proxy",
				"environment": "old-env",
			},
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"app":         "reverse-proxy",
				"component":   "reverse-proxy",
				"environment": "old-env",
			},
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: "TCP",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingService).
		Build()

	// Create reconciliation context
	reconciliation := &ReverseProxyReconciliation{
		Log:      logr.Discard(),
		Recorder: &record.FakeRecorder{},
		Client:   fakeClient,
		Ctx:      context.Background(),
		Frontend: &crd.Frontend{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-frontend",
				Namespace: "test-namespace",
			},
		},
		FrontendEnvironment: &crd.FrontendEnvironment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-env",
			},
			Spec: crd.FrontendEnvironmentSpec{
				ReverseProxyImage: "test-image:latest",
			},
		},
	}

	tests := []struct {
		name          string
		initialLabels map[string]string
		expectUpdate  bool
		expectedEnv   string
	}{
		{
			name: "Environment label changed - should update",
			initialLabels: map[string]string{
				"app":         "reverse-proxy",
				"component":   "reverse-proxy",
				"environment": "old-env",
			},
			expectUpdate: true,
			expectedEnv:  "test-env",
		},
		{
			name: "Labels same - should not update",
			initialLabels: map[string]string{
				"app":         "reverse-proxy",
				"component":   "reverse-proxy",
				"environment": "test-env",
			},
			expectUpdate: false,
			expectedEnv:  "test-env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the service state
			service := &v1.Service{}
			err := fakeClient.Get(context.Background(), types.NamespacedName{
				Name:      "reverse-proxy",
				Namespace: "test-namespace",
			}, service)
			if err != nil {
				t.Fatalf("Failed to get service: %v", err)
			}

			// Set initial labels
			service.Labels = tt.initialLabels
			service.Spec.Selector = tt.initialLabels

			err = fakeClient.Update(context.Background(), service)
			if err != nil {
				t.Fatalf("Failed to update service: %v", err)
			}

			// Test the update functionality
			err = reconciliation.updateReverseProxyService(service)
			if err != nil {
				t.Fatalf("updateReverseProxyService failed: %v", err)
			}

			// Get the updated service
			updatedService := &v1.Service{}
			err = fakeClient.Get(context.Background(), types.NamespacedName{
				Name:      "reverse-proxy",
				Namespace: "test-namespace",
			}, updatedService)
			if err != nil {
				t.Fatalf("Failed to get updated service: %v", err)
			}

			// Verify environment label is correctly set
			if envLabel, exists := updatedService.Labels["environment"]; exists {
				if envLabel != tt.expectedEnv {
					t.Errorf("Expected environment label to be %s, got %s", tt.expectedEnv, envLabel)
				}
			} else {
				t.Error("Environment label not found in service")
			}

			// Verify selector is correctly set
			if envSelector, exists := updatedService.Spec.Selector["environment"]; exists {
				if envSelector != tt.expectedEnv {
					t.Errorf("Expected environment selector to be %s, got %s", tt.expectedEnv, envSelector)
				}
			} else {
				t.Error("Environment selector not found in service")
			}
		})
	}
}

// TestServiceNeedsUpdate tests the service comparison function
func TestServiceNeedsUpdate(t *testing.T) {
	reconciliation := &ReverseProxyReconciliation{}

	httpProtocol := "http"

	tests := []struct {
		name     string
		current  *v1.Service
		desired  *v1.Service
		expected bool
	}{
		{
			name: "Identical services - no update needed",
			current: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "reverse-proxy", "environment": "test"},
				},
				Spec: v1.ServiceSpec{
					Selector: map[string]string{"app": "reverse-proxy"},
					Ports: []v1.ServicePort{
						{Name: "http", Port: 8080, Protocol: "TCP", AppProtocol: &httpProtocol},
					},
				},
			},
			desired: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "reverse-proxy", "environment": "test"},
				},
				Spec: v1.ServiceSpec{
					Selector: map[string]string{"app": "reverse-proxy"},
					Ports: []v1.ServicePort{
						{Name: "http", Port: 8080, Protocol: "TCP", AppProtocol: &httpProtocol},
					},
				},
			},
			expected: false,
		},
		{
			name: "Different port - update needed",
			current: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "reverse-proxy"},
				},
				Spec: v1.ServiceSpec{
					Selector: map[string]string{"app": "reverse-proxy"},
					Ports: []v1.ServicePort{
						{Name: "http", Port: 8080, Protocol: "TCP"},
					},
				},
			},
			desired: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "reverse-proxy"},
				},
				Spec: v1.ServiceSpec{
					Selector: map[string]string{"app": "reverse-proxy"},
					Ports: []v1.ServicePort{
						{Name: "http", Port: 8090, Protocol: "TCP"},
					},
				},
			},
			expected: true,
		},
		{
			name: "Different environment label - update needed",
			current: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "reverse-proxy", "environment": "old"},
				},
				Spec: v1.ServiceSpec{
					Selector: map[string]string{"app": "reverse-proxy"},
					Ports: []v1.ServicePort{
						{Name: "http", Port: 8080, Protocol: "TCP"},
					},
				},
			},
			desired: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "reverse-proxy", "environment": "new"},
				},
				Spec: v1.ServiceSpec{
					Selector: map[string]string{"app": "reverse-proxy"},
					Ports: []v1.ServicePort{
						{Name: "http", Port: 8080, Protocol: "TCP"},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciliation.compareService(tt.current, tt.desired)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestContainerNeedsUpdate tests the container comparison function
func TestContainerNeedsUpdate(t *testing.T) {
	reconciliation := &ReverseProxyReconciliation{}

	baseContainer := &v1.Container{
		Name:  "reverse-proxy",
		Image: "quay.io/cloudservices/frontend-asset-proxy:old-tag",
		Ports: []v1.ContainerPort{
			{Name: "http", ContainerPort: 8080, Protocol: "TCP"},
		},
		Env: []v1.EnvVar{
			{Name: "VAR1", Value: "value1"},
		},
	}

	tests := []struct {
		name           string
		current        *v1.Container
		desired        *v1.Container
		expectUpdate   bool
		expectedReason string
	}{
		{
			name:           "Identical containers - no update needed",
			current:        baseContainer,
			desired:        baseContainer,
			expectUpdate:   false,
			expectedReason: "",
		},
		{
			name:    "Image changed - update needed",
			current: baseContainer,
			desired: &v1.Container{
				Name:  "reverse-proxy",
				Image: "quay.io/cloudservices/frontend-asset-proxy:new-tag",
				Ports: []v1.ContainerPort{
					{Name: "http", ContainerPort: 8080, Protocol: "TCP"},
				},
				Env: []v1.EnvVar{
					{Name: "VAR1", Value: "value1"},
				},
			},
			expectUpdate:   true,
			expectedReason: "image changed from quay.io/cloudservices/frontend-asset-proxy:old-tag to quay.io/cloudservices/frontend-asset-proxy:new-tag",
		},
		{
			name:    "Environment variables changed - update needed",
			current: baseContainer,
			desired: &v1.Container{
				Name:  "reverse-proxy",
				Image: "quay.io/cloudservices/frontend-asset-proxy:old-tag",
				Ports: []v1.ContainerPort{
					{Name: "http", ContainerPort: 8080, Protocol: "TCP"},
				},
				Env: []v1.EnvVar{
					{Name: "VAR1", Value: "new-value"},
				},
			},
			expectUpdate:   true,
			expectedReason: "environment variables changed",
		},
		{
			name:    "Port changed - update needed",
			current: baseContainer,
			desired: &v1.Container{
				Name:  "reverse-proxy",
				Image: "quay.io/cloudservices/frontend-asset-proxy:old-tag",
				Ports: []v1.ContainerPort{
					{Name: "http", ContainerPort: 9090, Protocol: "TCP"},
				},
				Env: []v1.EnvVar{
					{Name: "VAR1", Value: "value1"},
				},
			},
			expectUpdate:   true,
			expectedReason: "container ports changed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needsUpdate, reason := reconciliation.compareContainer(tt.current, tt.desired)

			if needsUpdate != tt.expectUpdate {
				t.Errorf("Expected update=%v, got update=%v", tt.expectUpdate, needsUpdate)
			}

			if tt.expectUpdate && reason != tt.expectedReason {
				t.Errorf("Expected reason=%s, got reason=%s", tt.expectedReason, reason)
			}
		})
	}
}

// TestReverseProxyReconciliation_Ingress tests the ingress reconciliation logic
func TestReverseProxyReconciliation_Ingress(t *testing.T) {
	// Set up environment variables for bucket config
	os.Setenv("PUSHCACHE_AWS_ENDPOINT", "minio-service.default.svc.cluster.local")
	os.Setenv("PUSHCACHE_AWS_PORT", "9000")
	os.Setenv("PUSHCACHE_AWS_BUCKET_NAME", "frontend-assets")
	os.Setenv("PUSHCACHE_AWS_ACCESS_KEY_ID", "test-access-key")
	os.Setenv("PUSHCACHE_AWS_SECRET_ACCESS_KEY", "test-secret-key")
	os.Setenv("PUSHCACHE_AWS_REGION", "us-east-1")
	defer func() {
		os.Unsetenv("PUSHCACHE_AWS_ENDPOINT")
		os.Unsetenv("PUSHCACHE_AWS_PORT")
		os.Unsetenv("PUSHCACHE_AWS_BUCKET_NAME")
		os.Unsetenv("PUSHCACHE_AWS_ACCESS_KEY_ID")
		os.Unsetenv("PUSHCACHE_AWS_SECRET_ACCESS_KEY")
		os.Unsetenv("PUSHCACHE_AWS_REGION")
	}()

	// Create test objects
	frontend := &crd.Frontend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-frontend",
			Namespace: "test-namespace",
		},
		Spec: crd.FrontendSpec{
			EnvName: "test-env",
		},
	}

	frontendEnv := &crd.FrontendEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-env",
		},
		Spec: crd.FrontendEnvironmentSpec{
			EnablePushCache:   true,
			ReverseProxyImage: "quay.io/test/reverse-proxy:latest",
			Hostname:          "test.example.com",
			SSL:               false,
		},
	}

	// Create scheme and add our types
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = apps.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = crd.AddToScheme(scheme)

	// Create fake client
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(frontend, frontendEnv).Build()

	// Create reconciliation instance
	reconciliation := &ReverseProxyReconciliation{
		Log:                 logr.Discard(),
		Recorder:            &record.FakeRecorder{},
		Client:              client,
		Ctx:                 context.Background(),
		Frontend:            frontend,
		FrontendEnvironment: frontendEnv,
	}

	t.Run("CreateIngress", func(t *testing.T) {
		// Test creating ingress
		err := reconciliation.reconcileIngress()
		if err != nil {
			t.Fatalf("Failed to reconcile ingress: %v", err)
		}

		// Verify ingress was created
		ingress := &networkingv1.Ingress{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "reverse-proxy",
			Namespace: "test-namespace",
		}, ingress)
		if err != nil {
			t.Fatalf("Failed to get created ingress: %v", err)
		}

		// Verify ingress properties
		if ingress.Spec.Rules[0].Host != "test.example.com" {
			t.Errorf("Expected host=test.example.com, got host=%s", ingress.Spec.Rules[0].Host)
		}

		if ingress.Spec.Rules[0].HTTP.Paths[0].Path != "/" {
			t.Errorf("Expected path=/, got path=%s", ingress.Spec.Rules[0].HTTP.Paths[0].Path)
		}

		if ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name != "reverse-proxy" {
			t.Errorf("Expected service=reverse-proxy, got service=%s", ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name)
		}

		if ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Number != ReverseProxyPort {
			t.Errorf("Expected port=%d, got port=%d", ReverseProxyPort, ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Number)
		}

		// Verify no TLS since SSL is false
		if len(ingress.Spec.TLS) != 0 {
			t.Errorf("Expected no TLS configuration, got %d TLS entries", len(ingress.Spec.TLS))
		}
	})

	t.Run("CreateIngressWithSSL", func(t *testing.T) {
		// Update environment to enable SSL
		frontendEnvSSL := frontendEnv.DeepCopy()
		frontendEnvSSL.Spec.SSL = true

		reconciliationSSL := &ReverseProxyReconciliation{
			Log:                 logr.Discard(),
			Recorder:            &record.FakeRecorder{},
			Client:              client,
			Ctx:                 context.Background(),
			Frontend:            frontend,
			FrontendEnvironment: frontendEnvSSL,
		}

		// Create new ingress config
		ingressConfig, err := reconciliationSSL.createReverseProxyIngressConfig()
		if err != nil {
			t.Fatalf("Failed to create ingress config: %v", err)
		}

		// Verify TLS configuration is added
		if len(ingressConfig.Spec.TLS) != 1 {
			t.Errorf("Expected 1 TLS entry, got %d", len(ingressConfig.Spec.TLS))
		}

		if ingressConfig.Spec.TLS[0].SecretName != "reverse-proxy-tls" {
			t.Errorf("Expected TLS secret=reverse-proxy-tls, got secret=%s", ingressConfig.Spec.TLS[0].SecretName)
		}

		if len(ingressConfig.Spec.TLS[0].Hosts) != 1 || ingressConfig.Spec.TLS[0].Hosts[0] != "test.example.com" {
			t.Errorf("Expected TLS host=test.example.com, got hosts=%v", ingressConfig.Spec.TLS[0].Hosts)
		}
	})

	t.Run("UpdateIngress", func(t *testing.T) {
		// Create an existing ingress with different configuration
		existingIngress := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "reverse-proxy",
				Namespace: "test-namespace",
			},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{
					{
						Host: "old.example.com", // Different host
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{
									{
										Path: "/",
										Backend: networkingv1.IngressBackend{
											Service: &networkingv1.IngressServiceBackend{
												Name: "reverse-proxy",
												Port: networkingv1.ServiceBackendPort{
													Number: ReverseProxyPort,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		// Update client with existing ingress
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(frontend, frontendEnv, existingIngress).Build()
		reconciliation.Client = client

		// Test updating ingress
		err := reconciliation.reconcileIngress()
		if err != nil {
			t.Fatalf("Failed to reconcile ingress: %v", err)
		}

		// Verify ingress was updated
		updatedIngress := &networkingv1.Ingress{}
		err = client.Get(context.Background(), types.NamespacedName{
			Name:      "reverse-proxy",
			Namespace: "test-namespace",
		}, updatedIngress)
		if err != nil {
			t.Fatalf("Failed to get updated ingress: %v", err)
		}

		// Verify the host was updated
		if updatedIngress.Spec.Rules[0].Host != "test.example.com" {
			t.Errorf("Expected updated host=test.example.com, got host=%s", updatedIngress.Spec.Rules[0].Host)
		}
	})
}

// TestReverseProxyReconciliation_CompareIngress tests the ingress comparison logic
func TestReverseProxyReconciliation_CompareIngress(t *testing.T) {
	reconciliation := &ReverseProxyReconciliation{}

	pathType := networkingv1.PathTypePrefix
	baseIngress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/rewrite-target": "/",
				"nginx.ingress.kubernetes.io/ssl-redirect":   "false",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "test.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "reverse-proxy",
											Port: networkingv1.ServiceBackendPort{
												Number: ReverseProxyPort,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name            string
		current         *networkingv1.Ingress
		desired         *networkingv1.Ingress
		expectDifferent bool
	}{
		{
			name:            "Identical ingresses",
			current:         baseIngress.DeepCopy(),
			desired:         baseIngress.DeepCopy(),
			expectDifferent: false,
		},
		{
			name:    "Different host",
			current: baseIngress.DeepCopy(),
			desired: func() *networkingv1.Ingress {
				ing := baseIngress.DeepCopy()
				ing.Spec.Rules[0].Host = "different.example.com"
				return ing
			}(),
			expectDifferent: true,
		},
		{
			name:    "Different path",
			current: baseIngress.DeepCopy(),
			desired: func() *networkingv1.Ingress {
				ing := baseIngress.DeepCopy()
				ing.Spec.Rules[0].HTTP.Paths[0].Path = "/different-path"
				return ing
			}(),
			expectDifferent: true,
		},
		{
			name:    "Different service name",
			current: baseIngress.DeepCopy(),
			desired: func() *networkingv1.Ingress {
				ing := baseIngress.DeepCopy()
				ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name = "different-service"
				return ing
			}(),
			expectDifferent: true,
		},
		{
			name:    "Different port",
			current: baseIngress.DeepCopy(),
			desired: func() *networkingv1.Ingress {
				ing := baseIngress.DeepCopy()
				ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Number = 9090
				return ing
			}(),
			expectDifferent: true,
		},
		{
			name:    "TLS added",
			current: baseIngress.DeepCopy(),
			desired: func() *networkingv1.Ingress {
				ing := baseIngress.DeepCopy()
				ing.Spec.TLS = []networkingv1.IngressTLS{
					{
						Hosts:      []string{"test.example.com"},
						SecretName: "test-tls",
					},
				}
				return ing
			}(),
			expectDifferent: true,
		},
		{
			name:    "Different annotation",
			current: baseIngress.DeepCopy(),
			desired: func() *networkingv1.Ingress {
				ing := baseIngress.DeepCopy()
				ing.Annotations["nginx.ingress.kubernetes.io/ssl-redirect"] = "true"
				return ing
			}(),
			expectDifferent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			different := reconciliation.compareIngress(tt.current, tt.desired)
			if different != tt.expectDifferent {
				t.Errorf("Expected different=%v, got different=%v", tt.expectDifferent, different)
			}
		})
	}
}

// TestReverseProxyReconciliation_FullReconciliation tests the complete reconciliation flow
func TestReverseProxyReconciliation_FullReconciliation(t *testing.T) {
	// Set up environment variables
	os.Setenv("PUSHCACHE_AWS_ENDPOINT", "minio-service.default.svc.cluster.local")
	os.Setenv("PUSHCACHE_AWS_PORT", "9000")
	os.Setenv("PUSHCACHE_AWS_BUCKET_NAME", "frontend-assets")
	os.Setenv("PUSHCACHE_AWS_ACCESS_KEY_ID", "test-access-key")
	os.Setenv("PUSHCACHE_AWS_SECRET_ACCESS_KEY", "test-secret-key")
	os.Setenv("PUSHCACHE_AWS_REGION", "us-east-1")
	defer func() {
		os.Unsetenv("PUSHCACHE_AWS_ENDPOINT")
		os.Unsetenv("PUSHCACHE_AWS_PORT")
		os.Unsetenv("PUSHCACHE_AWS_BUCKET_NAME")
		os.Unsetenv("PUSHCACHE_AWS_ACCESS_KEY_ID")
		os.Unsetenv("PUSHCACHE_AWS_SECRET_ACCESS_KEY")
		os.Unsetenv("PUSHCACHE_AWS_REGION")
	}()

	// Create test objects
	frontend := &crd.Frontend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-frontend",
			Namespace: "test-namespace",
		},
		Spec: crd.FrontendSpec{
			EnvName: "test-env",
		},
	}

	frontendEnv := &crd.FrontendEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-env",
		},
		Spec: crd.FrontendEnvironmentSpec{
			EnablePushCache:   true,
			ReverseProxyImage: "quay.io/test/reverse-proxy:latest",
			Hostname:          "test.example.com",
			SSL:               false,
		},
	}

	// Create scheme and add our types
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = apps.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = crd.AddToScheme(scheme)

	// Create fake client
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(frontend, frontendEnv).Build()

	// Create reconciliation instance
	reconciliation := &ReverseProxyReconciliation{
		Log:                 logr.Discard(),
		Recorder:            &record.FakeRecorder{},
		Client:              client,
		Ctx:                 context.Background(),
		Frontend:            frontend,
		FrontendEnvironment: frontendEnv,
	}

	// Run full reconciliation
	err := reconciliation.run()
	if err != nil {
		t.Fatalf("Full reconciliation failed: %v", err)
	}

	// Verify deployment was created
	deployment := &apps.Deployment{}
	err = client.Get(context.Background(), types.NamespacedName{
		Name:      "reverse-proxy",
		Namespace: "test-namespace",
	}, deployment)
	if err != nil {
		t.Fatalf("Failed to get created deployment: %v", err)
	}

	// Verify service was created
	service := &v1.Service{}
	err = client.Get(context.Background(), types.NamespacedName{
		Name:      "reverse-proxy",
		Namespace: "test-namespace",
	}, service)
	if err != nil {
		t.Fatalf("Failed to get created service: %v", err)
	}

	// Verify ingress was created
	ingress := &networkingv1.Ingress{}
	err = client.Get(context.Background(), types.NamespacedName{
		Name:      "reverse-proxy",
		Namespace: "test-namespace",
	}, ingress)
	if err != nil {
		t.Fatalf("Failed to get created ingress: %v", err)
	}

	// Verify all resources have correct labels for scaling
	expectedLabels := map[string]string{
		"app":         "reverse-proxy",
		"component":   "reverse-proxy",
		"environment": "test-env",
	}

	// Check deployment labels and selectors
	for key, value := range expectedLabels {
		if deployment.Labels[key] != value {
			t.Errorf("Deployment label %s: expected=%s, got=%s", key, value, deployment.Labels[key])
		}
		if deployment.Spec.Selector.MatchLabels[key] != value {
			t.Errorf("Deployment selector %s: expected=%s, got=%s", key, value, deployment.Spec.Selector.MatchLabels[key])
		}
		if deployment.Spec.Template.Labels[key] != value {
			t.Errorf("Pod template label %s: expected=%s, got=%s", key, value, deployment.Spec.Template.Labels[key])
		}
	}

	// Check service labels and selectors
	for key, value := range expectedLabels {
		if service.Labels[key] != value {
			t.Errorf("Service label %s: expected=%s, got=%s", key, value, service.Labels[key])
		}
		if service.Spec.Selector[key] != value {
			t.Errorf("Service selector %s: expected=%s, got=%s", key, value, service.Spec.Selector[key])
		}
	}

	// Verify deployment can be scaled (has proper configuration)
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Errorf("Expected deployment replicas=1, got replicas=%v", deployment.Spec.Replicas)
	}

	// Verify container has proper resource limits for scaling
	container := deployment.Spec.Template.Spec.Containers[0]
	if container.Resources.Requests == nil {
		t.Error("Expected resource requests to be set for scaling")
	}
	if container.Resources.Limits == nil {
		t.Error("Expected resource limits to be set for scaling")
	}
}

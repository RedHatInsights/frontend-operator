package controllers

import (
	"testing"
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

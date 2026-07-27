package controllers

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestExtractBucketConfigFromSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)

	namespace := "ephemeral-abcdef"
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-" + namespace + "-minio",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"accessKey": []byte("test-access"),
			"secretKey": []byte("test-secret"),
			"hostname":  []byte("env-ephemeral-abcdef-minio.ephemeral-abcdef.svc"),
			"port":      []byte("9000"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret).
		Build()

	result, err := extractBucketConfigFromSecret(context.Background(), fakeClient, namespace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if *result.AccessKey != "test-access" {
		t.Errorf("AccessKey = %q, want %q", *result.AccessKey, "test-access")
	}
	if *result.SecretKey != "test-secret" {
		t.Errorf("SecretKey = %q, want %q", *result.SecretKey, "test-secret")
	}
	if *result.Endpoint != "env-ephemeral-abcdef-minio.ephemeral-abcdef.svc" {
		t.Errorf("Endpoint = %q, want %q", *result.Endpoint, "env-ephemeral-abcdef-minio.ephemeral-abcdef.svc")
	}
	if *result.Port != "9000" {
		t.Errorf("Port = %q, want %q", *result.Port, "9000")
	}
	if *result.Name != "frontend-pushcache" {
		t.Errorf("Name = %q, want %q", *result.Name, "frontend-pushcache")
	}
	if *result.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", *result.Region, "us-east-1")
	}
	if *result.TLS != false {
		t.Errorf("TLS = %v, want false", *result.TLS)
	}
}

func TestExtractBucketConfigFromSecretMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	_, err := extractBucketConfigFromSecret(context.Background(), fakeClient, "ephemeral-xyz")
	if err == nil {
		t.Fatal("expected error when Secret is missing, got nil")
	}
}

func TestExtractBucketConfigFromSecretMissingKeys(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)

	namespace := "ephemeral-abc"
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-" + namespace + "-minio",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"accessKey": []byte("key"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret).
		Build()

	_, err := extractBucketConfigFromSecret(context.Background(), fakeClient, namespace)
	if err == nil {
		t.Fatal("expected error when Secret is missing required keys, got nil")
	}
}

func TestExtractBucketConfigFromSecretDefaultPort(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)

	namespace := "ephemeral-noport"
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "env-" + namespace + "-minio",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"accessKey": []byte("key"),
			"secretKey": []byte("secret"),
			"hostname":  []byte("minio.svc"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret).
		Build()

	result, err := extractBucketConfigFromSecret(context.Background(), fakeClient, namespace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if *result.Port != "9000" {
		t.Errorf("Port = %q, want default %q", *result.Port, "9000")
	}
}

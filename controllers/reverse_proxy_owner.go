// This patch implements a single reverse proxy deployment across multiple frontends
// by making the reverse proxy owned by the environment instead of individual frontends.

package controllers

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetReverseProxyOwnerRef returns an owner reference to the FrontendEnvironment
// instead of the individual Frontend, which allows multiple Frontends sharing
// the same environment to co-exist without conflicts.
func (r *FrontendReconciliation) GetReverseProxyOwnerRef() metav1.OwnerReference {
	// If we have a FrontendEnvironment, use it as the owner
	if r.FrontendEnvironment != nil {
		// Create a ClusterRole-referencing OwnerReference since FrontendEnvironment is cluster-scoped
		return metav1.OwnerReference{
			APIVersion: r.FrontendEnvironment.APIVersion,
			Kind:       r.FrontendEnvironment.Kind,
			Name:       r.FrontendEnvironment.Name,
			UID:        r.FrontendEnvironment.UID,
			// We don't want to set controller=true since FrontendEnvironment is cluster-scoped
			// and the reverse proxy is namespace-scoped
		}
	}

	// Fall back to the Frontend if no environment is available
	return r.Frontend.MakeOwnerReference()
}

// GetReverseProxyLabels returns consistent labels for the reverse proxy
// that don't depend on the specific Frontend being reconciled
func (r *FrontendReconciliation) GetReverseProxyLabels() map[string]string {
	labels := make(map[string]string)

	// Use environment-specific labels
	if r.FrontendEnvironment != nil {
		labels["environment"] = r.FrontendEnvironment.Name
	}

	// Add common reverse proxy labels
	labels["app"] = "reverse-proxy"
	labels["component"] = "reverse-proxy"

	return labels
}

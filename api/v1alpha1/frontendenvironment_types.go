/*
Copyright 2021 RedHatInsights.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FrontendEnvironmentSpec defines the desired state of FrontendEnvironment
type FrontendEnvironmentSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of FrontendEnvironment. Edit FrontendEnvironment_types.go to remove/update
	SSO string `json:"sso"`

	// Ingress class
	IngressClass string `json:"ingressClass,omitempty"`

	// Hostname
	Hostname string `json:"hostname,omitempty"`
}

// FrontendEnvironmentStatus defines the observed state of FrontendEnvironment
type FrontendEnvironmentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=feenv

// FrontendEnvironment is the Schema for the FrontendEnvironments API
type FrontendEnvironment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FrontendEnvironmentSpec   `json:"spec,omitempty"`
	Status FrontendEnvironmentStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FrontendEnvironmentList contains a list of FrontendEnvironment
type FrontendEnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FrontendEnvironment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FrontendEnvironment{}, &FrontendEnvironmentList{})
}

// GetLabels returns a base set of labels relating to the ClowdApp.
func (i *FrontendEnvironment) GetLabels() map[string]string {
	if i.Labels == nil {
		i.Labels = map[string]string{}
	}

	if _, ok := i.Labels["FrontendEnvironment"]; !ok {
		i.Labels["FrontendEnvironment"] = i.ObjectMeta.Name
	}

	newMap := make(map[string]string, len(i.Labels))

	for k, v := range i.Labels {
		newMap[k] = v
	}

	return newMap
}

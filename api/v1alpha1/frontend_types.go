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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type ApiInfo struct {
	Versions []string `json:"versions"`
}

type FrontendInfo struct {
	Paths []string `json:"paths"`
}

// FrontendSpec defines the desired state of Frontend
type FrontendSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of Frontend. Edit frontend_types.go to remove/update
	Title          string       `json:"title"`
	DeploymentRepo string       `json:"deploymentRepo"`
	API            ApiInfo      `json:"API"`
	Frontend       FrontendInfo `json:"frontend"`
	Image          string       `json:"image"`
}

// FrontendStatus defines the observed state of Frontend
type FrontendStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Frontend is the Schema for the frontends API
type Frontend struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FrontendSpec   `json:"spec,omitempty"`
	Status FrontendStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FrontendList contains a list of Frontend
type FrontendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Frontend `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Frontend{}, &FrontendList{})
}

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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type ApiInfo struct {
	Versions []string `json:"versions" yaml:"versions"`
}

type FrontendInfo struct {
	Paths []string `json:"paths" yaml:"paths"`
}

// FrontendSpec defines the desired state of Frontend
type FrontendSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of Frontend. Edit frontend_types.go to remove/update
	EnvName        string           `json:"envName" yaml:"envName"`
	Title          string           `json:"title" yaml:"title"`
	DeploymentRepo string           `json:"deploymentRepo" yaml:"deploymentRepo"`
	API            ApiInfo          `json:"API" yaml:"API"`
	Frontend       FrontendInfo     `json:"frontend" yaml:"frontend"`
	Image          string           `json:"image,omitempty" yaml:"image,omitempty"`
	Service        string           `json:"service,omitempty" yaml:"service,omitempty"`
	Module         *FedModule       `json:"module,omitempty" yaml:"module,omitempty"`
	NavItems       []*BundleNavItem `json:"navItems,omitempty" yaml:"navItems,omitempty"`
}

var SuccessfulReconciliation clusterv1.ConditionType = "SuccessfulReconciliation"

// FrontendStatus defines the observed state of Frontend
type FrontendStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []clusterv1.Condition `json:"conditions,omitempty"`
}

type FedModule struct {
	ManifestLocation string   `json:"manifestLocation" yaml:"manifestLocation"`
	Modules          []Module `json:"modules,omitempty" yaml:"modules,omitempty"`
	ModuleID         string   `json:"moduleID,omitempty" yaml:"moduleID,omitempty"`
}

type Module struct {
	Id     string  `json:"id" yaml:"id"`
	Module string  `json:"module" yaml:"module"`
	Routes []Route `json:"routes" yaml:"routes"`
}

type Route struct {
	Pathname string `json:"pathname" yaml:"pathname"`
	Dynamic  bool   `json:"dynamic,omitempty" yaml:"dynamic,omitempty"`
	Exact    bool   `json:"exact,omitempty" yaml:"exact,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Frontend is the Schema for the frontends API
type Frontend struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   FrontendSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status FrontendStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FrontendList contains a list of Frontend
type FrontendList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Items           []Frontend `json:"items" yaml:"items"`
}

func (i *Frontend) GetConditions() clusterv1.Conditions {
	return i.Status.Conditions
}

func (i *Frontend) SetConditions(conditions clusterv1.Conditions) {
	i.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&Frontend{}, &FrontendList{})
}

// GetIdent returns an ident <env>.<app> that should be unique across the cluster.
func (i *Frontend) GetIdent() string {
	return fmt.Sprintf("%v.%v", i.Spec.EnvName, i.Name)
}

func (FEInfo *FrontendInfo) HasPath(lookup string) bool {
	for _, a := range FEInfo.Paths {
		if a == lookup {
			return true
		}
	}
	return false
}

// GetLabels returns a base set of labels relating to the ClowdApp.
func (i *Frontend) GetLabels() map[string]string {
	if i.Labels == nil {
		i.Labels = map[string]string{}
	}

	if _, ok := i.Labels["frontend"]; !ok {
		i.Labels["frontend"] = i.ObjectMeta.Name
	}

	newMap := make(map[string]string, len(i.Labels))

	for k, v := range i.Labels {
		newMap[k] = v
	}

	return newMap
}

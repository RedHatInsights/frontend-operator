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

type BundlePermissionArgs []string

type BundlePermission struct {
	Method string                 `json:"method"`
	Args   []BundlePermissionArgs `json:"args"`
}

type BundleNavItem struct {
	Title       string              `json:"title"`
	GroupID     string              `json:"groupId,omitempty"`
	NavItems    []LeafBundleNavItem `json:"navItems,omitempty"`
	AppId       string              `json:"appId,omitempty"`
	Href        string              `json:"href,omitempty"`
	Product     string              `json:"product,omitempty"`
	IsExternal  bool                `json:"isExternal,omitempty"`
	Filterable  bool                `json:"filterable,omitempty"`
	Permissions []BundlePermission  `json:"permissions,omitempty"`
	Routes      []LeafBundleNavItem `json:"routes,omitempty"`
}

type LeafBundleNavItem struct {
	Title       string             `json:"title"`
	GroupID     string             `json:"groupId,omitempty"`
	AppId       string             `json:"appId,omitempty"`
	Href        string             `json:"href,omitempty"`
	Product     string             `json:"product,omitempty"`
	IsExternal  bool               `json:"isExternal,omitempty"`
	Filterable  bool               `json:"filterable,omitempty"`
	Permissions []BundlePermission `json:"permissions,omitempty"`
}

type ComputedBundle struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	NavItems []BundleNavItem `json:"navItems"`
}

type ExtraNavItem struct {
	Name    string        `json:"name"`
	NavItem BundleNavItem `json:"navItem"`
}

// BundleSpec defines the desired state of Bundle
type BundleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of Bundle. Edit Bundle_types.go to remove/update
	ID            string         `json:"id"`
	Title         string         `json:"title,omitempty"`
	AppList       []string       `json:"appList,omitempty"`
	EnvName       string         `json:"envName,omitempty"`
	ExtraNavItems []ExtraNavItem `json:"extraNavItems,omitempty"`
}

// BundleStatus defines the observed state of Bundle
type BundleStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Bundle is the Schema for the Bundles API
type Bundle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec      BundleSpec      `json:"spec,omitempty"`
	CustomNav []BundleNavItem `json:"customNav,omitempty"`
	Status    BundleStatus    `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// BundleList contains a list of Bundle
type BundleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Bundle `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Bundle{}, &BundleList{})
}

// GetLabels returns a base set of labels relating to the ClowdApp.
func (i *Bundle) GetLabels() map[string]string {
	if i.Labels == nil {
		i.Labels = map[string]string{}
	}

	if _, ok := i.Labels["Bundle"]; !ok {
		i.Labels["Bundle"] = i.ObjectMeta.Name
	}

	newMap := make(map[string]string, len(i.Labels))

	for k, v := range i.Labels {
		newMap[k] = v
	}

	return newMap
}

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

type BundlePermissionArg string

type BundlePermission struct {
	Method string                `json:"method" yaml:"method"`
	Args   []BundlePermissionArg `json:"args,omitempty" yaml:"args,omitempty"`
}

// EmbeddedRoutes allow deeply nested navs to have support for routes
type EmbeddedRoute struct {
	Title   string `json:"title,omitempty" yaml:"title,omitempty"`
	AppId   string `json:"appId,omitempty" yaml:"appId,omitempty"`
	Href    string `json:"href,omitempty" yaml:"href,omitempty"`
	Product string `json:"product,omitempty" yaml:"product,omitempty"`
}

type BundleNavItem struct {
	Title       string              `json:"title" yaml:"title"`
	GroupID     string              `json:"groupId,omitempty" yaml:"groupId,omitempty"`
	Icon        string              `json:"icon,omitempty" yaml:"icon,omitempty"`
	NavItems    []LeafBundleNavItem `json:"navItems,omitempty" yaml:"navItems,omitempty"`
	AppId       string              `json:"appId,omitempty" yaml:"appId,omitempty"`
	Href        string              `json:"href,omitempty" yaml:"href,omitempty"`
	Product     string              `json:"product,omitempty" yaml:"product,omitempty"`
	IsExternal  bool                `json:"isExternal,omitempty" yaml:"isExternal,omitempty"`
	Filterable  bool                `json:"filterable,omitempty" yaml:"filterable,omitempty"`
	Permissions []BundlePermission  `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Routes      []EmbeddedRoute     `json:"routes,omitempty" yaml:"routes,omitempty"`
	Expandable  bool                `json:"expandable,omitempty" yaml:"expandable,omitempty"`
	DynamicNav  string              `json:"dynamicNav,omitempty" yaml:"dynamicNav,omitempty"`
}

type LeafBundleNavItem struct {
	Title       string             `json:"title" yaml:"title"`
	GroupID     string             `json:"groupId,omitempty" yaml:"groupId,omitempty"`
	AppId       string             `json:"appId,omitempty" yaml:"appId,omitempty"`
	Href        string             `json:"href,omitempty" yaml:"href,omitempty"`
	Product     string             `json:"product,omitempty" yaml:"product,omitempty"`
	IsExternal  bool               `json:"isExternal,omitempty" yaml:"isExternal,omitempty"`
	Filterable  bool               `json:"filterable,omitempty" yaml:"filterable,omitempty"`
	Expandable  bool               `json:"expandable,omitempty" yaml:"expandable,omitempty"`
	Notifier    string             `json:"notifier,omitempty" yaml:"notifier,omitempty"`
	Routes      []EmbeddedRoute    `json:"routes,omitempty" yaml:"routes,omitempty"`
	Permissions []BundlePermission `json:"permissions,omitempty" yaml:"permissions,omitempty"`
}

type ComputedBundle struct {
	ID       string          `json:"id" yaml:"id"`
	Title    string          `json:"title" yaml:"title"`
	NavItems []BundleNavItem `json:"navItems" yaml:"navItems"`
}

type ExtraNavItem struct {
	Name    string        `json:"name" yaml:"name"`
	NavItem BundleNavItem `json:"navItem" yaml:"navItem"`
}

// BundleSpec defines the desired state of Bundle
type BundleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of Bundle. Edit Bundle_types.go to remove/update
	ID            string          `json:"id" yaml:"id"`
	Title         string          `json:"title,omitempty" yaml:"title,omitempty"`
	AppList       []string        `json:"appList,omitempty" yaml:"appList,omitempty"`
	EnvName       string          `json:"envName,omitempty" yaml:"envName,omitempty"`
	ExtraNavItems []ExtraNavItem  `json:"extraNavItems,omitempty" yaml:"extraNavItems,omitempty"`
	CustomNav     []BundleNavItem `json:"customNav,omitempty" yaml:"customNav,omitempty"`
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
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   BundleSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status BundleStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

//+kubebuilder:object:root=true

// BundleList contains a list of Bundle
type BundleList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Items           []Bundle `json:"items" yaml:"items"`
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

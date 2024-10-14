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
	"context"
	"fmt"

	errors "github.com/RedHatInsights/clowder/controllers/cloud.redhat.com/errors"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type APIInfo struct {
	Versions []string `json:"versions" yaml:"versions"`
}

type FrontendInfo struct {
	Paths []string `json:"paths" yaml:"paths"`
}

type ServiceMonitorConfig struct {
	Disabled bool `json:"disabled,omitempty"`
}

type SearchEntry struct {
	ID          string   `json:"id" yaml:"id"`
	Href        string   `json:"href" yaml:"href"`
	Title       string   `json:"title" yaml:"title"`
	Description string   `json:"description" yaml:"description"`
	AltTitle    []string `json:"alt_title" yaml:"alt_title"`
	IsExternal  bool     `json:"isExternal" yaml:"isExternal"`
}

// FrontendSpec defines the desired state of Frontend
type FrontendSpec struct {
	Disabled       bool                 `json:"disabled,omitempty" yaml:"disabled,omitempty"`
	EnvName        string               `json:"envName" yaml:"envName"`
	Title          string               `json:"title" yaml:"title"`
	DeploymentRepo string               `json:"deploymentRepo" yaml:"deploymentRepo"`
	API            APIInfo              `json:"API" yaml:"API"`
	Frontend       FrontendInfo         `json:"frontend" yaml:"frontend"`
	Image          string               `json:"image,omitempty" yaml:"image,omitempty"`
	Service        string               `json:"service,omitempty" yaml:"service,omitempty"`
	ServiceMonitor ServiceMonitorConfig `json:"serviceMonitor,omitempty" yaml:"serviceMontior,omitempty"`
	Module         *FedModule           `json:"module,omitempty" yaml:"module,omitempty"`
	NavItems       []*BundleNavItem     `json:"navItems,omitempty" yaml:"navItems,omitempty"`
	AssetsPrefix   string               `json:"assetsPrefix,omitempty" yaml:"assetsPrefix,omitempty"`
	// Akamai cache bust opt-out
	AkamaiCacheBustDisable bool `json:"akamaiCacheBustDisable,omitempty" yaml:"akamaiCacheBustDisable,omitempty"`
	// Files to cache bust
	AkamaiCacheBustPaths []string `json:"akamaiCacheBustPaths,omitempty" yaml:"akamaiCacheBustPaths,omitempty"`
	// The search index partials for the resource
	SearchEntries []*SearchEntry `json:"searchEntries,omitempty" yaml:"searchEntries,omitempty"`
}

var ReconciliationSuccessful = "ReconciliationSuccessful"
var ReconciliationFailed = "ReconciliationFailed"
var FrontendsReady = "FrontendsReady"

// FrontendStatus defines the observed state of Frontend
type FrontendStatus struct {
	Deployments FrontendDeployments `json:"deployments,omitempty"`
	Ready       bool                `json:"ready"`
	Conditions  []metav1.Condition  `json:"conditions,omitempty"`
}

type FrontendDeployments struct {
	ManagedDeployments int32 `json:"managedDeployments"`
	ReadyDeployments   int32 `json:"readyDeployments"`
}

type FedModule struct {
	ManifestLocation string              `json:"manifestLocation" yaml:"manifestLocation"`
	Modules          []Module            `json:"modules,omitempty" yaml:"modules,omitempty"`
	ModuleID         string              `json:"moduleID,omitempty" yaml:"moduleID,omitempty"`
	Config           *apiextensions.JSON `json:"config,omitempty" yaml:"config,omitempty"`
	FullProfile      *bool               `json:"fullProfile,omitempty" yaml:"fullProfile,omitempty"`
}

type Module struct {
	ID                   string   `json:"id" yaml:"id"`
	Module               string   `json:"module" yaml:"module"`
	Routes               []Route  `json:"routes" yaml:"routes"`
	Dependencies         []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	OptionalDependencies []string `json:"optionalDependencies,omitempty" yaml:"optionalDependencies,omitempty"`
}

type Route struct {
	Pathname string              `json:"pathname" yaml:"pathname"`
	Dynamic  bool                `json:"dynamic,omitempty" yaml:"dynamic,omitempty"`
	Exact    bool                `json:"exact,omitempty" yaml:"exact,omitempty"`
	Props    *apiextensions.JSON `json:"props,omitempty" yaml:"props,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=fe
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.deployments.readyDeployments"
// +kubebuilder:printcolumn:name="Managed",type="integer",JSONPath=".status.deployments.managedDeployments"
// +kubebuilder:printcolumn:name="EnvName",type="string",JSONPath=".spec.envName"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

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

func (i *Frontend) GetConditions() []metav1.Condition {
	return i.Status.Conditions
}

func (i *Frontend) SetConditions(conditions []metav1.Condition) {
	i.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&Frontend{}, &FrontendList{})
}

// MakeOwnerReference defines the owner reference pointing to the Frontend resource.
func (i *Frontend) MakeOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: i.APIVersion,
		Kind:       i.Kind,
		Name:       i.ObjectMeta.Name,
		UID:        i.ObjectMeta.UID,
		Controller: TruePtr(),
	}
}

// TruePtr returns a pointer to True
func TruePtr() *bool {
	t := true
	return &t
}

// FalsePtr returns a pointer to False
func FalsePtr() *bool {
	t := false
	return &t
}

// GetIdent returns an ident <env>.<app> that should be unique across the cluster.
func (i *Frontend) GetIdent() string {
	return fmt.Sprintf("%v.%v", i.Spec.EnvName, i.Name)
}

func (feinfo *FrontendInfo) HasPath(lookup string) bool {
	for _, a := range feinfo.Paths {
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

func (i *Frontend) GetNamespacesInEnv(ctx context.Context, pClient client.Client) ([]string, error) {

	var env = &FrontendEnvironment{}
	var err error

	if err = i.GetOurEnv(ctx, pClient, env); err != nil {
		return nil, errors.Wrap("get our env: ", err)
	}

	var feList *FrontendList

	if feList, err = env.GetFrontendsInEnv(ctx, pClient); err != nil {
		return nil, errors.Wrap("get apps in env: ", err)
	}

	tmpNamespace := map[string]bool{}

	for _, app := range feList.Items {
		tmpNamespace[app.Namespace] = true
	}

	namespaceList := []string{}

	for namespace := range tmpNamespace {
		namespaceList = append(namespaceList, namespace)
	}

	return namespaceList, nil
}

func (i *Frontend) GetOurEnv(ctx context.Context, pClient client.Client, env *FrontendEnvironment) error {
	return pClient.Get(ctx, types.NamespacedName{Name: i.Spec.EnvName}, env)
}

// GetDeploymentStatus returns the Status.Deployments member
func (i *Frontend) GetDeploymentStatus() *FrontendDeployments {
	return &i.Status.Deployments
}

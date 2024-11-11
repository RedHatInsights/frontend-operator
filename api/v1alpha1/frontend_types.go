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
	AltTitle    []string `json:"alt_title,omitempty" yaml:"alt_title,omitempty"`
	IsExternal  bool     `json:"isExternal,omitempty" yaml:"isExternal,omitempty"`
}

type ServiceTile struct {
	Section     string `json:"section" yaml:"section"`
	Group       string `json:"group" yaml:"group"`
	ID          string `json:"id" yaml:"id"`
	Href        string `json:"href" yaml:"href"`
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`
	Icon        string `json:"icon" yaml:"icon"`
	IsExternal  bool   `json:"isExternal,omitempty" yaml:"isExternal,omitempty"`
}

type WidgetHeaderLink struct {
	Title string `json:"title" yaml:"title"`
	Href  string `json:"href" yaml:"href"`
}

type WidgetConfig struct {
	Icon        string           `json:"icon" yaml:"icon"`
	Title       string           `json:"title" yaml:"title"`
	Permissions []Permission     `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	HeaderLink  WidgetHeaderLink `json:"headerLink,omitempty" yaml:"headerLink,omitempty"`
}

type WidgetDefaultVariant struct {
	Width     int `json:"w" yaml:"w"`
	Height    int `json:"h" yaml:"h"`
	MaxHeight int `json:"maxH" yaml:"maxH"`
	MinHeight int `json:"minH" yaml:"minH"`
}

type WidgetDefaults struct {
	Small  WidgetDefaultVariant `json:"sm" yaml:"sm"`
	Medium WidgetDefaultVariant `json:"md" yaml:"md"`
	Large  WidgetDefaultVariant `json:"lg" yaml:"lg"`
	XLarge WidgetDefaultVariant `json:"xl" yaml:"xl"`
}

type WidgetEntry struct {
	Scope    string         `json:"scope" yaml:"scope"`
	Module   string         `json:"module" yaml:"module"`
	Config   WidgetConfig   `json:"config" yaml:"config"`
	Defaults WidgetDefaults `json:"defaults" yaml:"defaults"`
}

type NavigationSegment struct {
	SectionID string `json:"sectionId" yaml:"sectionId"`
	// Id of the bundle to which the segment should be injected
	BundleID string `json:"bundleId" yaml:"bundleId"`
	// A position of the segment within the bundle
	// 0 is the first position
	// The position "steps" should be at least 100 to make sure there is enough space in case some segments should be injected between existing ones
	Position uint             `json:"position" yaml:"position"`
	NavItems *[]ChromeNavItem `json:"navItems" yaml:"navItems"`
}

// FrontendSpec defines the desired state of Frontend
type FrontendSpec struct {
	Disabled       bool                 `json:"disabled,omitempty" yaml:"disabled,omitempty"`
	EnvName        string               `json:"envName" yaml:"envName"`
	Title          string               `json:"title" yaml:"title"`
	DeploymentRepo string               `json:"deploymentRepo" yaml:"deploymentRepo"`
	API            *APIInfo             `json:"API,omitempty" yaml:"API,omitempty"`
	Frontend       FrontendInfo         `json:"frontend" yaml:"frontend"`
	Image          string               `json:"image,omitempty" yaml:"image,omitempty"`
	Service        string               `json:"service,omitempty" yaml:"service,omitempty"`
	ServiceMonitor ServiceMonitorConfig `json:"serviceMonitor,omitempty" yaml:"serviceMontior,omitempty"`
	Module         *FedModule           `json:"module,omitempty" yaml:"module,omitempty"`
	NavItems       []*BundleNavItem     `json:"navItems,omitempty" yaml:"navItems,omitempty"`
	// navigation segments for the frontend
	NavigationSegments []*NavigationSegment `json:"navigationSegments,omitempty" yaml:"navigationSegments,omitempty"`
	AssetsPrefix       string               `json:"assetsPrefix,omitempty" yaml:"assetsPrefix,omitempty"`
	// Akamai cache bust opt-out
	AkamaiCacheBustDisable bool `json:"akamaiCacheBustDisable,omitempty" yaml:"akamaiCacheBustDisable,omitempty"`
	// Files to cache bust
	AkamaiCacheBustPaths []string `json:"akamaiCacheBustPaths,omitempty" yaml:"akamaiCacheBustPaths,omitempty"`
	// The search index partials for the resource
	SearchEntries []*SearchEntry `json:"searchEntries,omitempty" yaml:"searchEntries,omitempty"`
	// Data for the all services dropdown
	ServiceTiles []*ServiceTile `json:"serviceTiles,omitempty" yaml:"serviceTiles,omitempty"`
	// Data for the available widgets for the resource
	WidgetRegistry []*WidgetEntry `json:"widgetRegistry,omitempty" yaml:"widgetRegistry,omitempty"`
	Replicas       *int32         `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	// Injects configuration from application when enabled
	FeoConfigEnabled bool `json:"feoConfigEnabled,omitempty" yaml:"feoConfigEnabled,omitempty"`
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
	ManifestLocation     string              `json:"manifestLocation" yaml:"manifestLocation"`
	Modules              []Module            `json:"modules,omitempty" yaml:"modules,omitempty"`
	ModuleID             string              `json:"moduleID,omitempty" yaml:"moduleID,omitempty"`
	Config               *apiextensions.JSON `json:"config,omitempty" yaml:"config,omitempty"` // Type does not match what is currently in chrome-service spec
	ModuleConfig         *ModuleConfig       `json:"moduleConfig,omitempty" yaml:"moduleConfig,omitempty"`
	FullProfile          *bool               `json:"fullProfile,omitempty" yaml:"fullProfile,omitempty"`
	DefaultDocumentTitle string              `json:"defaultDocumentTitle,omitempty" yaml:"defaultDocumentTitle,omitempty"`
	IsFedramp            *bool               `json:"isFedramp,omitempty" yaml:"isFedramp,omitempty"`
	Analytics            *Analytics          `json:"analytics,omitempty" yaml:"analytics,omitempty"`
}

type Module struct {
	ID                   string   `json:"id" yaml:"id"`
	Module               string   `json:"module" yaml:"module"`
	Routes               []Route  `json:"routes" yaml:"routes"`
	Dependencies         []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`                 // not in the current chrome-service spec
	OptionalDependencies []string `json:"optionalDependencies,omitempty" yaml:"optionalDependencies,omitempty"` // not in the current chrome-service spec
}

type ModuleConfig struct {
	SupportCaseData SupportCaseData `json:"supportCaseData,omitempty" yaml:"supportCaseData,omitempty"`
	SSOScopes       []string        `json:"ssoScopes,omitempty" yaml:"ssoScopes,omitempty"`
}

type Route struct {
	Pathname        string              `json:"pathname" yaml:"pathname"`
	Dynamic         bool                `json:"dynamic,omitempty" yaml:"dynamic,omitempty"`
	Exact           bool                `json:"exact,omitempty" yaml:"exact,omitempty"`
	Props           *apiextensions.JSON `json:"props,omitempty" yaml:"props,omitempty"`
	FullProfile     bool                `json:"fullProfile,omitempty" yaml:"fullProfile,omitempty"`
	IsFedramp       bool                `json:"isFedramp,omitempty" yaml:"isFedramp,omitempty"`
	SupportCaseData *SupportCaseData    `json:"supportCaseData,omitempty" yaml:"supportCaseData,omitempty"`
	Permissions     []Permission        `json:"permissions,omitempty" yaml:"permissions,omitempty"`
}

type Analytics struct {
	APIKey string `json:"APIKey" yaml:"APIKey"`
}

type SupportCaseData struct {
	Version string `json:"version" yaml:"version"`
	Product string `json:"product" yaml:"product"`
}

type Permission struct {
	Method string              `json:"method" yaml:"method"`
	Apps   []string            `json:"apps,omitempty" yaml:"apps,omitempty"`
	Args   *apiextensions.JSON `json:"args,omitempty" yaml:"args,omitempty"` // TODO validate array item type (string?)
}

type ChromeNavItem struct {
	IsHidden   bool   `json:"isHidden,omitempty" yaml:"isHidden,omitempty"`
	Expandable bool   `json:"expandable,omitempty" yaml:"expandable,omitempty"`
	Href       string `json:"href,omitempty" yaml:"href,omitempty"`
	AppID      string `json:"appId,omitempty" yaml:"appId,omitempty"`
	IsExternal bool   `json:"isExternal,omitempty" yaml:"isExternal,omitempty"`
	Title      string `json:"title" yaml:"title"`
	GroupID    string `json:"groupId,omitempty" yaml:"groupId,omitempty"`
	ID         string `json:"id,omitempty" yaml:"id,omitempty"`
	Product    string `json:"product,omitempty" yaml:"product,omitempty"`
	Notifier   string `json:"notifier,omitempty" yaml:"notifier,omitempty"`
	Icon       string `json:"icon,omitempty" yaml:"icon,omitempty"`
	IsBeta     bool   `json:"isBeta,omitempty" yaml:"isBeta,omitempty"`
	// kubebuilder struggles validating recursive fields, it has to be helped a bit
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	NavItems []ChromeNavItem `json:"navItems,omitempty" yaml:"navItems,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Routes      []ChromeNavItem `json:"routes,omitempty" yaml:"routes,omitempty"`
	Permissions []Permission    `json:"permissions,omitempty" yaml:"permissions,omitempty"`
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

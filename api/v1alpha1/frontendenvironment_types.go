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

	errors "github.com/RedHatInsights/clowder/controllers/cloud.redhat.com/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FrontendBundles defines the bundles specific to an environment that will be used to
// construct navigation
type FrontendBundles struct {
	ID          string `json:"id" yaml:"id"`
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// The frontend bundles but with the nav items filled with chrome nav items
type FrontendBundlesGenerated struct {
	ID          string          `json:"id" yaml:"id"`
	Title       string          `json:"title" yaml:"title"`
	Description string          `json:"description,omitempty" yaml:"description,omitempty"`
	NavItems    []ChromeNavItem `json:"navItems" yaml:"navItems"`
}

type FrontendServiceCategoryGroup struct {
	ID    string `json:"id" yaml:"id"`
	Title string `json:"title" yaml:"title"`
}

// FrontendServiceCategory defines the category to which service can inject ServiceTiles
// Chroming UI will use this to render the service dropdown component
type FrontendServiceCategory struct {
	ID    string `json:"id" yaml:"id"`
	Title string `json:"title" yaml:"title"`
	// +kubebuilder:validation:MinItems:=1
	Groups []FrontendServiceCategoryGroup `json:"groups" yaml:"groups"`
}

type FrontendServiceCategoryGroupGenerated struct {
	ID    string         `json:"id" yaml:"id"`
	Title string         `json:"title" yaml:"title"`
	Tiles *[]ServiceTile `json:"tiles" yaml:"tiles"`
}

// The categories but with the groups filled with service tiles
type FrontendServiceCategoryGenerated struct {
	ID     string                                  `json:"id" yaml:"id"`
	Title  string                                  `json:"title" yaml:"title"`
	Groups []FrontendServiceCategoryGroupGenerated `json:"groups" yaml:"groups"`
}

// FrontendEnvironmentSpec defines the desired state of FrontendEnvironment
type FrontendEnvironmentSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of FrontendEnvironment. Edit FrontendEnvironment_types.go to remove/update
	SSO string `json:"sso"`

	// Ingress class
	IngressClass string `json:"ingressClass,omitempty"`
	// Ingress annotations
	// These annotations will be applied to the ingress objects created by the frontend
	IngressAnnotations map[string]string `json:"ingressAnnotations,omitempty"`

	// Hostname
	Hostname string `json:"hostname,omitempty"`

	// Whitelist CIDRs
	Whitelist []string `json:"whitelist,omitempty"`

	// MonitorMode determines where a ServiceMonitor object will be placed
	// local will add it to the frontend's namespace
	// app-interface will add it to "openshift-customer-monitoring"
	Monitoring *MonitoringConfig `json:"monitoring,omitempty"`

	// SSL mode requests SSL from the services in openshift and k8s and then applies them to the
	// pod, the route is also set to reencrypt in the case of OpenShift
	SSL bool `json:"ssl,omitempty"`

	// GenerateNavJSON determines if the nav json configmap
	// parts should be generated for the bundles. We want to do
	// do this in epehemeral environments but not in production
	GenerateNavJSON bool `json:"generateNavJSON,omitempty"`
	// Enable Akamai Cache Bust
	EnableAkamaiCacheBust bool `json:"enableAkamaiCacheBust,omitempty"`
	// Set Akamai Cache Bust Image
	AkamaiCacheBustImage string `json:"akamaiCacheBustImage,omitempty"`
	// Deprecated: Users should move to AkamaiCacheBustURLs
	// Preserving for backwards compatibility
	AkamaiCacheBustURL string `json:"akamaiCacheBustURL,omitempty"`
	// Set Akamai Cache Bust URL that the files will hang off of
	AkamaiCacheBustURLs []string `json:"akamaiCacheBustURLs,omitempty"`
	// The name of the secret we will use to get the akamai credentials
	AkamaiSecretName string `json:"akamaiSecretName,omitempty"`
	// List of namespaces that should receive a copy of the frontend configuration as a config map
	// By configurations we mean the fed-modules.json, navigation files, etc.
	TargetNamespaces []string `json:"targetNamespaces,omitempty" yaml:"targetNamespaces,omitempty"`
	// For the ChromeUI to render additional global components
	ServiceCategories *[]FrontendServiceCategory `json:"serviceCategories,omitempty" yaml:"serviceCategories,omitempty"`
	// Custom HTTP Headers
	// These populate an ENV var that is then added into the caddy config as a header block
	HTTPHeaders map[string]string `json:"httpHeaders,omitempty"`
	// OverwriteCaddyConfig determines if the operator should overwrite
	// frontend container Caddyfiles with a common core Caddyfile
	OverwriteCaddyConfig bool `json:"overwriteCaddyConfig,omitempty"`
	// Enable Push Cache Container
	EnablePushCache bool `json:"enablePushCache,omitempty"`
	// S3 Push Cache Bucket
	PushCacheBucket string `json:"pushCacheBucket,omitempty"`

	DefaultReplicas *int32 `json:"defaultReplicas,omitempty" yaml:"defaultReplicas,omitempty"`
	// For the ChromeUI to render navigation bundles
	Bundles *[]FrontendBundles `json:"bundles,omitempty" yaml:"bundles,omitempty"`

	Requests v1.ResourceList `json:"requests,omitempty" yaml:"requests,omitempty"`
	Limits   v1.ResourceList `json:"limits,omitempty" yaml:"limits,omitempty"`
}

type MonitoringConfig struct {
	// +kubebuilder:validation:Enum={"local", "app-interface"}
	Mode     string `json:"mode"`
	Disabled bool   `json:"disabled"`
}

// FrontendEnvironmentStatus defines the observed state of FrontendEnvironment
type FrontendEnvironmentStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=feenv
// +kubebuilder:printcolumn:name="Namespace",type="string",JSONPath=".status.targetNamespace"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

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

	if _, ok := i.Labels["frontendenv"]; !ok {
		i.Labels["frontendenv"] = i.ObjectMeta.Name
	}

	newMap := make(map[string]string, len(i.Labels))

	for k, v := range i.Labels {
		newMap[k] = v
	}

	return newMap
}

func (i *FrontendEnvironment) GetFrontendsInEnv(ctx context.Context, pClient client.Client) (*FrontendList, error) {

	feList := &FrontendList{}

	err := pClient.List(ctx, feList, client.MatchingFields{"spec.envName": i.Name})

	if err != nil {
		return feList, errors.Wrap("could not list apps", err)
	}

	return feList, nil
}

func (i *FrontendEnvironment) GenerateTargetNamespace() string {
	return i.Name
}

// MakeOwnerReference defines the owner reference pointing to the Frontend resource.
func (i *FrontendEnvironment) MakeOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: i.APIVersion,
		Kind:       i.Kind,
		Name:       i.ObjectMeta.Name,
		UID:        i.ObjectMeta.UID,
		Controller: TruePtr(),
	}
}

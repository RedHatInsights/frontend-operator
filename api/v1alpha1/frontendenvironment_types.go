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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	//Whitelist CIDRs
	Whitelist []string `json:"whitelist,omitempty"`

	//MonitorMode determines where a ServiceMonitor object will be placed
	// local will add it to the frontend's namespace
	// app-interface will add it to "openshift-customer-monitoring"
	Monitoring *MonitoringConfig `json:"monitoring,omitempty"`
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

/*

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RouteMigrateSpec defines the desired state of RouteMigrate
type RouteMigrateSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Migrate is an example field of RouteMigrate. Edit RouteMigrate_types.go to remove/update
	DestinationNamespace string             `json:"destinationNamespace,omitempty"`
	Routes               RouteMigrateRoutes `json:"routes,omitempty"`
}

// RouteMigrateStatus defines the observed state of RouteMigrate
type RouteMigrateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []RouteMigrateConditions `json:"conditions,omitempty"`
}

// RouteMigrateConditions defines the observed conditions of the migrations
type RouteMigrateConditions struct {
	LastTransitionTime string `json:"lastTransitionTime"`
	Status             string `json:"status"`
	Type               string `json:"type"`
	Condition          string `json:"condition"`
}

type RouteMigrateRoutes struct {
	ActiveRoutes  string `json:"activeRoutes"`
	StandbyRoutes string `json:"standbyRoutes"`
}

// +kubebuilder:object:root=true

// RouteMigrate is the Schema for the routemigrates API
type RouteMigrate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteMigrateSpec   `json:"spec,omitempty"`
	Status RouteMigrateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RouteMigrateList contains a list of RouteMigrate
type RouteMigrateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RouteMigrate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RouteMigrate{}, &RouteMigrateList{})
}

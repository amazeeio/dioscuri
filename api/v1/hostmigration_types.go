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

// HostMigrationSpec defines the desired state of HostMigration
type HostMigrationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Migrate is an example field of RouteMigrate. Edit RouteMigrate_types.go to remove/update
	DestinationNamespace string             `json:"destinationNamespace,omitempty"`
	Hosts                HostMigrationHosts `json:"hosts,omitempty"`
}

// HostMigrationStatus defines the observed state of RouteMigrate
type HostMigrationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []HostMigrationConditions `json:"conditions,omitempty"`
}

// HostMigrationConditions defines the observed conditions of the migrations
type HostMigrationConditions struct {
	LastTransitionTime string `json:"lastTransitionTime"`
	Status             string `json:"status"`
	Type               string `json:"type"`
	Condition          string `json:"condition"`
}

// HostMigrationHosts .
type HostMigrationHosts struct {
	ActiveHosts  string `json:"activeHosts"`
	StandbyHosts string `json:"standbyHosts"`
}

// +kubebuilder:object:root=true

// HostMigration is the Schema for the hostmigrations API
type HostMigration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HostMigrationSpec   `json:"spec,omitempty"`
	Status HostMigrationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HostMigrationList contains a list of HostMigration
type HostMigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HostMigration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HostMigration{}, &HostMigrationList{})
}

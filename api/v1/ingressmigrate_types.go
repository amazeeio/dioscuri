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

// IngressMigrateSpec defines the desired state of IngressMigrate
type IngressMigrateSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Migrate is an example field of Migrate. Edit Migrate_types.go to remove/update
	DestinationNamespace string         `json:"destinationNamespace,omitempty"`
	Ingress              MigrateIngress `json:"ingress,omitempty"`
}

// IngressMigrateStatus defines the observed state of IngressMigrate
type IngressMigrateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []IngressMigrateConditions `json:"conditions,omitempty"`
}

// IngressMigrateConditions defines the observed conditions of the migrations
type IngressMigrateConditions struct {
	LastTransitionTime string `json:"lastTransitionTime"`
	Status             string `json:"status"`
	Type               string `json:"type"`
	Condition          string `json:"condition"`
}

// MigrateIngress .
type MigrateIngress struct {
	ActiveIngress  string `json:"activeIngress"`
	StandbyIngress string `json:"standbyIngress"`
}

// +kubebuilder:object:root=true

// IngressMigrate is the Schema for the ingressmigrates API
type IngressMigrate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IngressMigrateSpec   `json:"spec,omitempty"`
	Status IngressMigrateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IngressMigrateList contains a list of IngressMigrate
type IngressMigrateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IngressMigrate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IngressMigrate{}, &IngressMigrateList{})
}

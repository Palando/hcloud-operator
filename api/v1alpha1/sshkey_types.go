/*
Copyright 2022 Sascha Gaspar.

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SshKeySpec defines the desired state of SshKey
type SshKeySpec struct {
	Location  Location `json:"location,omitempty"`
	Id        string   `json:"id"`
	PublicKey string   `json:"publicKey,omitempty"`
}

// SshKeyStatus defines the observed state of SshKey
type SshKeyStatus struct {
	VmStatus    VmStatus `json:"vmStatus,omitempty"`
	Location    Location `json:"location,omitempty"`
	Id          string   `json:"id"`
	Allocated   bool     `json:"allocated"`
	Tainted     bool     `json:"tainted"`
	PublicKey   string   `json:"publicKey,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SshKey is the Schema for the sshkeys API
type SshKey struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SshKeySpec   `json:"spec,omitempty"`
	Status SshKeyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SshKeyList contains a list of SshKey
type SshKeyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SshKey `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SshKey{}, &SshKeyList{})
}

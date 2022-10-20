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

// VirtualMachineSpec defines the desired state of VirtualMachine
type VirtualMachineSpec struct {
	Location                 Location `json:"location,omitempty"`
	Id                       string   `json:"id"`
	VirtualMachineTemplateId string   `json:"vmTemplateId"`
	Image                    string   `json:"image"`
	SshUserName              string   `json:"sshUserName"`
	SecretNames              []string `json:"secretNames"`
}

// VirtualMachineStatus defines the observed state of VirtualMachine
type VirtualMachineStatus struct {
	VmStatus     VmStatus `json:"vmStatus,omitempty"`
	Location     Location `json:"location,omitempty"`
	Id           string   `json:"id"`
	Allocated    bool     `json:"allocated"`
	Tainted      bool     `json:"tainted"`
	PublicIP     string   `json:"publicIP,omitempty"`
	PublicIPv6   string   `json:"publicIPv6,omitempty"`
	PrivateIP    string   `json:"privateIP,omitempty"`
	PrivateIPv6  string   `json:"privateIPv6,omitempty"`
	Hostname     string   `json:"hostName,omitempty"`
	RootPassword string   `json:"rootPassword"`
}

type VmStatus string

const (
	None                 VmStatus = ""
	ReadyForProvisioning VmStatus = "readyforprovisioning"
	Provisioning         VmStatus = "provisioning"
	Running              VmStatus = "running"
	Terminating          VmStatus = "terminating"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// VirtualMachine is the Schema for the virtualmachines API
type VirtualMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VirtualMachineSpec   `json:"spec,omitempty"`
	Status VirtualMachineStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VirtualMachineList contains a list of VirtualMachine
type VirtualMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VirtualMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VirtualMachine{}, &VirtualMachineList{})
}

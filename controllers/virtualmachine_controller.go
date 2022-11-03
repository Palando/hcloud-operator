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

package controllers

import (
	"context"
	"fmt"
	"time"

	hc "github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/go-logr/logr"

	"github.com/palando/hcloud-operator/hcloud"

	hcloudv1alpha1 "github.com/palando/hcloud-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logger "sigs.k8s.io/controller-runtime/pkg/log"
)

// VirtualMachineReconciler reconciles a VirtualMachine object
type VirtualMachineReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=hcloud.sva.codes,resources=virtualmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=hcloud.sva.codes,resources=virtualmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=hcloud.sva.codes,resources=virtualmachines/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *VirtualMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	virtualMachineFinalizer := "VirtualMachine.hcloud.sva.codes"

	log := logger.FromContext(ctx)

	vmCr, err := r.GetVirtualMachineCR(ctx, req, log)
	if errors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	token, err := r.GetTokenFromSecret(ctx, req.Namespace, log)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	hclient, err := hcloud.NewHetznerCloudClient(token, vmCr.Spec.Location)
	if err != nil {
		return ctrl.Result{}, err
	}

	vmInfo, err := hcloud.GetVirtualMachineInfo(ctx, *hclient, vmCr.Spec.Id, log)
	if err != nil {
		return ctrl.Result{}, err
	}

	if vmCr.ObjectMeta.DeletionTimestamp.IsZero() {
		switch vmCr.Status.VmStatus {
		case hcloudv1alpha1.None:
			if vmInfo == nil {
				result, err := r.CreateVirtualMachineAndUpdateState(ctx, hclient, vmCr, log)
				if err != nil {
					log.Error(err, "Error creating virtual machine")
					return result, err
				}
			} else {
				r.updateCrStateFromServerInfo(ctx, vmCr, vmInfo, log)
			}
			r.Status().Update(ctx, vmCr)
			if err != nil {
				log.Error(err, "Error updating status")
				return ctrl.Result{}, err
			}
			if !controllerutil.ContainsFinalizer(vmCr, virtualMachineFinalizer) {
				controllerutil.AddFinalizer(vmCr, virtualMachineFinalizer)
			}
			err = r.Update(ctx, vmCr)
			if err != nil {
				log.Error(err, "Error updating virtual machine CR")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		case hcloudv1alpha1.Provisioning:
			if vmInfo == nil {
				return ctrl.Result{}, nil
			}
			r.updateCrStateFromServerInfo(ctx, vmCr, vmInfo, log)
			r.Status().Update(ctx, vmCr)
			if err != nil {
				log.Error(err, "Error updating status")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(vmCr, virtualMachineFinalizer) {
			vmCr.Status.VmStatus = hcloudv1alpha1.None
			r.Status().Update(ctx, vmCr)
			if err != nil {
				log.Error(err, "Error updating status")
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(vmCr, virtualMachineFinalizer)
			err = r.Update(ctx, vmCr)
			if err != nil {
				log.Error(err, "Error updating virtual machine CR")
				return ctrl.Result{}, err
			}
			if vmInfo == nil {
				return ctrl.Result{}, nil
			}
			err := r.DeleteVirtualMachine(ctx, hclient, vmInfo, log)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{Requeue: true}, nil
}

func (r *VirtualMachineReconciler) CreateVirtualMachineAndUpdateState(ctx context.Context, hclient *hc.Client, vmCr *hcloudv1alpha1.VirtualMachine, log logr.Logger) (ctrl.Result, error) {
	keys, err := hcloud.GetSshKeys(ctx, *hclient, vmCr.Spec.SecretNames, log)
	if err != nil {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, err
	}
	createResult, serverOpts, err := hcloud.CreateVm(ctx, *hclient, vmCr, keys, log)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}
	r.updateCrStateFromCreationResult(ctx, vmCr, createResult, serverOpts, log)

	return ctrl.Result{}, nil
}

func (r *VirtualMachineReconciler) updateCrStateFromServerInfo(ctx context.Context, vmCr *hcloudv1alpha1.VirtualMachine, serverInfo *hc.Server, log logr.Logger) {
	vmCr.Status.Tainted = true
	vmCr.Status.VmStatus = hcloudv1alpha1.Provisioning
	vmCr.Status.Id = serverInfo.Name

	ipv4 := serverInfo.PublicNet.IPv4.IP.To4()
	if ipv4 == nil {
		vmCr.Status.PrivateIP = ""
	} else {
		vmCr.Status.PrivateIP = ipv4.String()
	}

	ipv6 := serverInfo.PublicNet.IPv6.IP.To16()
	if ipv6 == nil {
		vmCr.Status.PrivateIPv6 = ""
	} else {
		vmCr.Status.PrivateIPv6 = ipv6.String() + "1"
	}

	switch serverInfo.Status {
	case hc.ServerStatusInitializing:
		fallthrough
	case hc.ServerStatusStarting:
		fallthrough
	case hc.ServerStatusMigrating:
		fallthrough
	case hc.ServerStatusRebuilding:
		fallthrough
	case hc.ServerStatusOff:
		vmCr.Status.VmStatus = hcloudv1alpha1.Provisioning
		vmCr.Status.Allocated = false
		vmCr.Status.Tainted = false
	case hc.ServerStatusRunning:
		vmCr.Status.VmStatus = hcloudv1alpha1.Running
		vmCr.Status.Allocated = true
		vmCr.Status.Tainted = false
	case hc.ServerStatusDeleting:
		fallthrough
	case hc.ServerStatusStopping:
		vmCr.Status.VmStatus = hcloudv1alpha1.Terminating
		vmCr.Status.Allocated = false
		vmCr.Status.Tainted = false
	case hc.ServerStatusUnknown:
		fallthrough
	default:
		vmCr.Status.VmStatus = hcloudv1alpha1.Error
		vmCr.Status.Allocated = false
		vmCr.Status.Tainted = true
	}
}

func (r *VirtualMachineReconciler) updateCrStateFromCreationResult(ctx context.Context, vmCr *hcloudv1alpha1.VirtualMachine, createResult *hc.ServerCreateResult, serverOpts *hc.ServerCreateOpts, log logr.Logger) {
	vmCr.Status.RootPassword = createResult.RootPassword

	r.updateCrStateFromServerInfo(ctx, vmCr, createResult.Server, log)
	vmCr.Status.Location = hcloudv1alpha1.Location(serverOpts.Location.Name)
}

func (r *VirtualMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hcloudv1alpha1.VirtualMachine{}).
		Complete(r)
}

func (r *VirtualMachineReconciler) GetTokenFromSecret(ctx context.Context, namespace string, log logr.Logger) (token string, err error) {
	secret := &corev1.Secret{}
	namespacedSecret := types.NamespacedName{Namespace: namespace, Name: "hcloud-token"}
	err = r.Get(ctx, namespacedSecret, secret)
	if err != nil {
		log.Error(err, "Secret hcloud-token does not exist in namespace "+namespace)
		return "", err
	}

	tokenData, ok := secret.Data["hcloud-token"]
	if !ok {
		errorMessage := "no key hcloud-token exists in secret in namespace " + namespace
		err := fmt.Errorf(errorMessage)
		log.Error(err, errorMessage)
		return "", err
	}
	return string(tokenData), nil
}

func (r *VirtualMachineReconciler) GetVirtualMachineCR(ctx context.Context, req ctrl.Request, log logr.Logger) (*hcloudv1alpha1.VirtualMachine, error) {
	vm := &hcloudv1alpha1.VirtualMachine{}
	err := r.Get(ctx, req.NamespacedName, vm)
	if errors.IsNotFound(err) {
		return nil, err
	}
	if err != nil {
		log.Error(err, "Unable to fetch VirtualMachine instance.")
		return nil, fmt.Errorf("could not fetch ReplicaSet: %+v", err)
	}
	return vm, nil
}

func (r *VirtualMachineReconciler) DeleteVirtualMachine(ctx context.Context, hclient *hc.Client, server *hc.Server, log logr.Logger) error {
	err := hcloud.DeleteVm(ctx, hclient, server, log)
	if err != nil {
		log.Error(err, "error deleting virtual machine "+server.Name)
		return err
	}
	return nil
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

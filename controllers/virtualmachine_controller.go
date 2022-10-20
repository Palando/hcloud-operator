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

	log.Info("Reconcile start, VirtualMachine.hcloud.sva.codes req.Namespace: " + req.Namespace + ", Name: " + req.Name)

	vmCr, err := r.GetVirtualMachineCR(ctx, req, log)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("vm.Spec.Id: " + vmCr.Spec.Id)
	log.Info("vm.Status.Id: " + vmCr.Status.Id)

	if vmCr.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("Deletion timestamp is zero")
	} else {
		log.Info("Deletion timestamp is not zero")
	}

	vmId := vmCr.Spec.Id
	if vmId == "" {
		vmId = vmCr.Status.Id
	}

	log.Info("VM ID: " + vmId)

	token, err := r.GetTokenFromSecret(ctx, req.Namespace, log)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	hclient, err := hcloud.NewHetznerCloudClient(token, vmCr.Spec.Location)
	if err != nil {
		return ctrl.Result{}, err
	}

	vmInfo, err := hcloud.GetVirtualMachineInfo(ctx, *hclient, vmId, log)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.updateCrStateFromServerInfo(ctx, vmCr, vmInfo, log)

	if vmCr.Status.VmStatus == "" && vmInfo.Name != vmCr.Spec.Id && vmCr.ObjectMeta.DeletionTimestamp.IsZero() {
		result, err := r.CreateVirtualMachine(ctx, hclient, vmCr, log)
		if err != nil {
			return result, err
		}
		controllerutil.AddFinalizer(&vmCr, virtualMachineFinalizer)
	} else if containsString(vmCr.ObjectMeta.Finalizers, virtualMachineFinalizer) || !vmCr.ObjectMeta.DeletionTimestamp.IsZero() {
		err := r.DeleteVirtualMachine(ctx, hclient, vmInfo, log)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Info("Reconsile end.")

	return ctrl.Result{}, nil
}

func (r *VirtualMachineReconciler) CreateVirtualMachine(ctx context.Context, hclient *hc.Client, vmCr hcloudv1alpha1.VirtualMachine, log logr.Logger) (ctrl.Result, error) {
	keys, err := hcloud.GetSshKeys(ctx, *hclient, vmCr.Spec.SecretNames, log)
	if err != nil {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, err
	}
	createResult, serverOpts, err := hcloud.CreateVm(ctx, *hclient, vmCr, keys, log)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}
	err = r.updateCrState(ctx, vmCr, createResult, serverOpts, log)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *VirtualMachineReconciler) updateCrStateFromServerInfo(ctx context.Context, vmCr hcloudv1alpha1.VirtualMachine, serverInfo *hc.Server, log logr.Logger) error {
	vmCr.Status.Id = serverInfo.Name
	// vmCr.Status.RootPassword = serverInfo.PublicNet.IPv4.DNSPtr
	// Todo:
}

func (r *VirtualMachineReconciler) updateCrState(ctx context.Context, vmCr hcloudv1alpha1.VirtualMachine, createResult *hc.ServerCreateResult, serverOpts *hc.ServerCreateOpts, log logr.Logger) error {
	vmCr.Status.RootPassword = createResult.RootPassword
	vmCr.Status.Location = hcloudv1alpha1.Location(serverOpts.Location.Name)
	vmCr.Status.Tainted = true
	vmCr.Status.VmStatus = hcloudv1alpha1.Provisioning
	vmCr.Status.Id = createResult.Server.Name

	ipv4 := createResult.Server.PublicNet.IPv4.IP.To4()
	if ipv4 == nil {
		vmCr.Status.PrivateIP = ""
	} else {
		vmCr.Status.PrivateIP = ipv4.String()
	}

	ipv6 := createResult.Server.PublicNet.IPv6.IP.To16()
	if ipv6 == nil {
		vmCr.Status.PrivateIPv6 = ""
	} else {
		vmCr.Status.PrivateIPv6 = ipv6.String() + "1"
	}

	log.Info("Update CR state. Root password is " + vmCr.Status.RootPassword)

	err := r.Status().Update(ctx, &vmCr)
	// err := r.Update(ctx, &vmCr)
	if err != nil {
		log.Error(err, "error updating virtual machine CR")
		return err
	}
	return nil
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

func (r *VirtualMachineReconciler) GetVirtualMachineCR(ctx context.Context, req ctrl.Request, log logr.Logger) (hcloudv1alpha1.VirtualMachine, error) {
	vm := hcloudv1alpha1.VirtualMachine{}
	if err := r.Get(ctx, req.NamespacedName, &vm); err != nil {
		if errors.IsNotFound(err) {
			return vm, nil
		}
		log.Error(err, "Unable to fetch VirtualMachine instance.")
		return vm, client.IgnoreNotFound(err)
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

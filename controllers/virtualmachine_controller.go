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

	"github.com/go-logr/logr"

	"github.com/palando/hcloud-operator/hcloud"

	hcloudv1alpha1 "github.com/palando/hcloud-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	log := logger.FromContext(ctx)

	log.Info("Reconcile start, req.Namespace: " + req.Namespace + ", Name: " + req.Name)

	vmCr, err := r.GetVirtualMachineCR(ctx, req, log)
	if err != nil {
		return ctrl.Result{}, err
	}

	token, err := r.GetTokenFromSecret(ctx, vmCr, log)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	hclient, err := hcloud.NewHetznerCloudClient(token, vmCr.Spec.Region)
	if err != nil {
		return ctrl.Result{}, err
	}

	vmInfo, err := hcloud.GetVirtualMachineInfo(ctx, *hclient, vmCr.Spec.Id, log)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("VM Name: " + vmCr.Name + ", VM CR State: " + string(vmCr.Status.VmStatus) + ", VM State: " + string(vmInfo.Status))

	if vmCr.ObjectMeta.DeletionTimestamp.IsZero() {
		switch vmCr.Status.VmStatus {
		case hcloudv1alpha1.None:
			// hcloud.CreateVm(*hclient)
		case hcloudv1alpha1.ReadyForProvisioning:

		case hcloudv1alpha1.Provisioning:

		case hcloudv1alpha1.Running:

		case hcloudv1alpha1.Terminating:

		}
	}
	// else {

	// }

	log.Info("Reconsile end.")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hcloudv1alpha1.VirtualMachine{}).
		Complete(r)
}

func (r *VirtualMachineReconciler) GetTokenFromSecret(ctx context.Context, instance hcloudv1alpha1.VirtualMachine, log logr.Logger) (token string, err error) {
	secret := &corev1.Secret{}
	namespacedSecret := types.NamespacedName{Namespace: instance.Namespace, Name: "hcloud-token"}
	err = r.Get(ctx, namespacedSecret, secret)
	if err != nil {
		log.Error(err, "Secret hcloud-token does not exist in namespace "+instance.Namespace)
		return "", err
	}

	tokenData, ok := secret.Data["hcloud-token"]
	if !ok {
		errorMessage := "no key hcloud-token exists in secret in namespace " + instance.Namespace
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

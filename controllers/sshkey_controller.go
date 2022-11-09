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

// SshKeyReconciler reconciles a sshkey object
type SshKeyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=hcloud.sva.codes,resources=virtualmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=hcloud.sva.codes,resources=virtualmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=hcloud.sva.codes,resources=virtualmachines/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *SshKeyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	virtualMachineFinalizer := "sshkey.hcloud.sva.codes"

	log := logger.FromContext(ctx)

	cr, err := r.GetSshKeyCR(ctx, req, log)
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

	hclient, err := hcloud.NewHetznerCloudClient(token, cr.Spec.Location)
	if err != nil {
		return ctrl.Result{}, err
	}

	vmInfo, err := hcloud.GetSshKeyInfo(ctx, *hclient, cr.Spec.Id, log)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cr.ObjectMeta.DeletionTimestamp.IsZero() {
		switch cr.Status.VmStatus {
		case hcloudv1alpha1.None:
			if vmInfo == nil {
				result, err := r.CreateSshKeyAndUpdateState(ctx, hclient, cr, log)
				if err != nil {
					log.Error(err, "Error creating virtual machine (90)")
					return result, err
				}
				err = r.updateStatus(ctx, req, cr)
				if err != nil {
					return ctrl.Result{Requeue: true}, err
				}
				if !controllerutil.ContainsFinalizer(cr, virtualMachineFinalizer) {
					controllerutil.AddFinalizer(cr, virtualMachineFinalizer)
				}
				err = r.Update(ctx, cr)
				if err != nil {
					log.Error(err, "Error updating virtual machine CR (108)")
					return ctrl.Result{}, err
				}
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{Requeue: false}, nil
		case hcloudv1alpha1.Provisioning:
			if vmInfo == nil {
				return ctrl.Result{}, nil
			}
			if r.updateCrStateFromServerInfo(cr, vmInfo) {
				err = r.updateStatus(ctx, req, cr)
				if err != nil {
					log.Error(err, "Error updating status (122)")
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{Requeue: true}, nil
		case hcloudv1alpha1.Running:
			return ctrl.Result{Requeue: false}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(cr, virtualMachineFinalizer) {
			controllerutil.RemoveFinalizer(cr, virtualMachineFinalizer)
			err = r.Update(ctx, cr)
			if err != nil {
				log.Error(err, "Error updating virtual machine CR")
				return ctrl.Result{}, err
			}
			if vmInfo == nil {
				log.Info("Error: VM is already deleted")
				return ctrl.Result{}, nil
			}
			err := r.DeleteSshKey(ctx, hclient, vmInfo, log)
			if err != nil {
				log.Error(err, "Error deleting virtual machine (151)")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{Requeue: false}, nil
	}

	return ctrl.Result{Requeue: true}, nil
}

func (r *SshKeyReconciler) updateStatus(ctx context.Context, req ctrl.Request, cr *hcloudv1alpha1.SshKey) error {
	err := r.Status().Update(ctx, cr)
	if err != nil {
		err = r.Get(ctx, req.NamespacedName, cr)
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		err = r.Status().Update(ctx, cr)
	}
	return err
}

func (r *SshKeyReconciler) CreateSshKeyAndUpdateState(ctx context.Context, hclient *hc.Client, cr *hcloudv1alpha1.SshKey, log logr.Logger) (ctrl.Result, error) {
	keys, err := hcloud.GetSshKeys(ctx, *hclient, cr.Spec.SecretNames, log)
	if err != nil {
		log.Error(err, "Error getting SSH keys")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, err
	}
	createResult, serverOpts, err := hcloud.CreateVm(ctx, *hclient, cr, keys, log)
	if err != nil {
		log.Error(err, "Error creating VM")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}
	r.updateCrStateFromCreationResult(cr, createResult, serverOpts)

	return ctrl.Result{}, nil
}

func (r *SshKeyReconciler) updateCrStateFromServerInfo(cr *hcloudv1alpha1.SshKey, serverInfo *hc.Server) bool {
	cr.Status.Id = serverInfo.Name

	ipv4 := serverInfo.PublicNet.IPv4.IP.To4()
	if ipv4 == nil {
		cr.Status.PrivateIP = ""
	} else {
		cr.Status.PrivateIP = ipv4.String()
	}

	ipv6 := serverInfo.PublicNet.IPv6.IP.To16()
	if ipv6 == nil {
		cr.Status.PrivateIPv6 = ""
	} else {
		cr.Status.PrivateIPv6 = ipv6.String() + "1"
	}

	oldVmState := cr.Status.VmStatus

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
		cr.Status.VmStatus = hcloudv1alpha1.Provisioning
		cr.Status.Allocated = false
		cr.Status.Tainted = false
	case hc.ServerStatusRunning:
		cr.Status.VmStatus = hcloudv1alpha1.Running
		cr.Status.Allocated = true
		cr.Status.Tainted = false
	case hc.ServerStatusDeleting:
		fallthrough
	case hc.ServerStatusStopping:
		cr.Status.VmStatus = hcloudv1alpha1.Terminating
		cr.Status.Allocated = false
		cr.Status.Tainted = false
	case hc.ServerStatusUnknown:
		fallthrough
	default:
		cr.Status.VmStatus = hcloudv1alpha1.Error
		cr.Status.Allocated = false
		cr.Status.Tainted = true
	}

	return cr.Status.VmStatus != oldVmState
}

func (r *SshKeyReconciler) updateCrStateFromCreationResult(cr *hcloudv1alpha1.SshKey, createResult *hc.ServerCreateResult, serverOpts *hc.ServerCreateOpts) {
	cr.Status.RootPassword = createResult.RootPassword
	cr.Status.VmStatus = hcloudv1alpha1.Provisioning

	r.updateCrStateFromServerInfo(cr, createResult.Server)
	cr.Status.Location = hcloudv1alpha1.Location(serverOpts.Location.Name)
}

func (r *SshKeyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hcloudv1alpha1.SshKey{}).
		Complete(r)
}

func (r *SshKeyReconciler) GetTokenFromSecret(ctx context.Context, namespace string, log logr.Logger) (token string, err error) {
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

func (r *SshKeyReconciler) GetSshKeyCR(ctx context.Context, req ctrl.Request, log logr.Logger) (*hcloudv1alpha1.SshKey, error) {
	vm := &hcloudv1alpha1.SshKey{}
	err := r.Get(ctx, req.NamespacedName, vm)
	if errors.IsNotFound(err) {
		return nil, err
	}
	if err != nil {
		log.Error(err, "Unable to fetch sshkey instance.")
		return nil, fmt.Errorf("could not fetch ReplicaSet: %+v", err)
	}
	return vm, nil
}

func (r *SshKeyReconciler) DeleteSshKey(ctx context.Context, hclient *hc.Client, server *hc.Server, log logr.Logger) error {
	err := hcloud.DeleteVm(ctx, hclient, server, log)
	if err != nil {
		log.Error(err, "error deleting virtual machine "+server.Name)
		return err
	}
	return nil
}

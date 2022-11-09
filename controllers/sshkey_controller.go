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

//+kubebuilder:rbac:groups=hcloud.sva.codes,resources=sshkeys,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=hcloud.sva.codes,resources=sshkeys/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=hcloud.sva.codes,resources=sshkeys/finalizers,verbs=update

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

	sshKeyInfo, err := hcloud.GetSshKeyInfo(ctx, *hclient, cr.Spec.Id, log)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cr.ObjectMeta.DeletionTimestamp.IsZero() {
		switch cr.Status.VmStatus {
		case hcloudv1alpha1.None:
			if sshKeyInfo == nil {
				result, err := r.CreateSshKeyAndUpdateState(hclient, cr, log)
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
			if sshKeyInfo == nil {
				return ctrl.Result{}, nil
			}
			if r.updateCrStateFromSshKeyInfo(cr, sshKeyInfo) {
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
			if sshKeyInfo == nil {
				log.Info("Error: SSH key is already deleted")
				return ctrl.Result{}, nil
			}
			err := r.DeleteSshKey(ctx, hclient, sshKeyInfo, log)
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

func (r *SshKeyReconciler) CreateSshKeyAndUpdateState(hclient *hc.Client, cr *hcloudv1alpha1.SshKey, log logr.Logger) (ctrl.Result, error) {
	sshKeyInfo, err := hcloud.CreateSshKey(*hclient, cr.Spec.Id, cr.Spec.PublicKey, log)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}
	r.updateCrStateFromSshKeyInfo(cr, sshKeyInfo)

	return ctrl.Result{}, nil
}

func (r *SshKeyReconciler) updateCrStateFromSshKeyInfo(cr *hcloudv1alpha1.SshKey, sshKeyInfo *hc.SSHKey) bool {
	cr.Status.Id = sshKeyInfo.Name
	cr.Status.PublicKey = sshKeyInfo.PublicKey
	cr.Status.Fingerprint = sshKeyInfo.Fingerprint
	return true
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
	sshKey := &hcloudv1alpha1.SshKey{}
	err := r.Get(ctx, req.NamespacedName, sshKey)
	if errors.IsNotFound(err) {
		return nil, err
	}
	if err != nil {
		log.Error(err, "Unable to fetch sshkey instance.")
		return nil, fmt.Errorf("could not fetch ReplicaSet: %+v", err)
	}
	return sshKey, nil
}

func (r *SshKeyReconciler) DeleteSshKey(ctx context.Context, hclient *hc.Client, sshKeyInfo *hc.SSHKey, log logr.Logger) error {
	_, err := hclient.SSHKey.Delete(ctx, sshKeyInfo)
	if err != nil {
		log.Error(err, "error deleting SSH key "+sshKeyInfo.Name)
		return err
	}
	return nil
}

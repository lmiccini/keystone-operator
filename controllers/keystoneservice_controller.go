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

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	keystonev1 "github.com/openstack-k8s-operators/keystone-operator/api/v1beta1"
	condition "github.com/openstack-k8s-operators/lib-common/modules/common/condition"
	helper "github.com/openstack-k8s-operators/lib-common/modules/common/helper"
	secret "github.com/openstack-k8s-operators/lib-common/modules/common/secret"

	openstack "github.com/openstack-k8s-operators/lib-common/modules/openstack"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GetClient -
func (r *KeystoneServiceReconciler) GetClient() client.Client {
	return r.Client
}

// GetKClient -
func (r *KeystoneServiceReconciler) GetKClient() kubernetes.Interface {
	return r.Kclient
}

// GetScheme -
func (r *KeystoneServiceReconciler) GetScheme() *runtime.Scheme {
	return r.Scheme
}

// KeystoneServiceReconciler reconciles a KeystoneService object
type KeystoneServiceReconciler struct {
	client.Client
	Kclient kubernetes.Interface
	Scheme  *runtime.Scheme
}

// GetLogger returns a logger object with a logging prefix of "controller.name" and additional controller context fields
func (r *KeystoneServiceReconciler) GetLogger(ctx context.Context) logr.Logger {
	return log.FromContext(ctx).WithName("Controllers").WithName("KeystoneService")
}

// +kubebuilder:rbac:groups=keystone.openstack.org,resources=keystoneservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keystone.openstack.org,resources=keystoneservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keystone.openstack.org,resources=keystoneservices/finalizers,verbs=update;patch
// +kubebuilder:rbac:groups=keystone.openstack.org,resources=keystoneapis,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=keystone.openstack.org,resources=keystoneapis/finalizers,verbs=update;patch

// Reconcile keystone service requests
func (r *KeystoneServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, _err error) {

	log := r.GetLogger(ctx)

	// Fetch the KeystoneService instance
	instance := &keystonev1.KeystoneService{}
	err := r.Client.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	helper, err := helper.NewHelper(
		instance,
		r.Client,
		r.Kclient,
		r.Scheme,
		//TODO remove later, log used here as to not break the helper struct signiture.
		log,
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always patch the instance status when exiting this function so we can persist any changes.
	defer func() {
		// Don't update the status, if Reconciler Panics
		if r := recover(); r != nil {
			log.Info(fmt.Sprintf("Panic during reconcile %v\n", r))
			panic(r)
		}
		// update the Ready condition based on the sub conditions
		if instance.Status.Conditions.AllSubConditionIsTrue() {
			instance.Status.Conditions.MarkTrue(
				condition.ReadyCondition, condition.ReadyMessage)
		} else {
			// something is not ready so reset the Ready condition
			instance.Status.Conditions.MarkUnknown(
				condition.ReadyCondition, condition.InitReason, condition.ReadyInitMessage)
			// and recalculate it based on the state of the rest of the conditions
			instance.Status.Conditions.Set(
				instance.Status.Conditions.Mirror(condition.ReadyCondition))
		}
		err := helper.PatchInstance(ctx, instance)
		if err != nil {
			_err = err
			return
		}
	}()

	//
	// initialize status
	//
	if instance.Status.Conditions == nil {
		instance.Status.Conditions = condition.Conditions{}
		cl := condition.CreateList(
			condition.UnknownCondition(keystonev1.KeystoneAPIReadyCondition, condition.InitReason, keystonev1.KeystoneAPIReadyInitMessage),
			condition.UnknownCondition(keystonev1.AdminServiceClientReadyCondition, condition.InitReason, keystonev1.AdminServiceClientReadyInitMessage),
			condition.UnknownCondition(keystonev1.KeystoneServiceOSServiceReadyCondition, condition.InitReason, keystonev1.KeystoneServiceOSServiceReadyInitMessage),
			condition.UnknownCondition(keystonev1.KeystoneServiceOSUserReadyCondition, condition.InitReason, keystonev1.KeystoneServiceOSUserReadyInitMessage))
		instance.Status.Conditions.Init(&cl)

		// Register overall status immediately to have an early feedback e.g. in the cli
		return ctrl.Result{}, nil
	}

	// If we're not deleting this and the service object doesn't have our finalizer, add it.
	if instance.DeletionTimestamp.IsZero() && controllerutil.AddFinalizer(instance, helper.GetFinalizer()) {
		return ctrl.Result{}, err
	}

	//
	// Validate that keystoneAPI is up
	//
	keystoneAPI, err := keystonev1.GetKeystoneAPI(ctx, helper, instance.Namespace, map[string]string{})
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			// If this KeystoneService CR is being deleted and it has not registered any actual
			// service on the OpenStack side, just redirect execution to the "reconcileDelete()"
			// logic to avoid potentially hanging on waiting for a KeystoneAPI to appear (which
			// is not needed anyhow, since there is nothing to clean-up on the OpenStack side)
			if !instance.DeletionTimestamp.IsZero() && instance.Status.ServiceID == "" {
				return r.reconcileDelete(ctx, instance, helper, nil, nil)
			}

			instance.Status.Conditions.Set(condition.FalseCondition(
				keystonev1.KeystoneAPIReadyCondition,
				condition.ErrorReason,
				condition.SeverityWarning,
				keystonev1.KeystoneAPIReadyNotFoundMessage,
			))
			log.Info("KeystoneAPI not found!")
			return ctrl.Result{RequeueAfter: time.Second * 5}, nil
		}
		instance.Status.Conditions.Set(condition.FalseCondition(
			keystonev1.KeystoneAPIReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			keystonev1.KeystoneAPIReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}

	// If both the service and the KeystoneAPI is deleted then we can skip
	// the cleanup of the service in the DB as the DB is going away as well.
	// Moreover if KeystoneAPI is being deleted then we cannot talk to the
	// keystone REST API any more. This happens for example during namespace
	// deletion.
	if !instance.DeletionTimestamp.IsZero() && !keystoneAPI.DeletionTimestamp.IsZero() {
		return r.reconcileDeleteFinalizersOnly(ctx, instance, helper, keystoneAPI)
	}

	// If this KeystoneService CR is being deleted and it has not registered any actual
	// service on the OpenStack side, just redirect execution to the "reconcileDelete()"
	// logic to avoid potentially hanging on waiting for the KeystoneAPI to be ready
	// (which is not needed anyhow, since there is nothing to clean-up on the OpenStack
	// side)
	if !instance.DeletionTimestamp.IsZero() && instance.Status.ServiceID == "" {
		return r.reconcileDelete(ctx, instance, helper, nil, keystoneAPI)
	}

	if !keystoneAPI.IsReady() {
		instance.Status.Conditions.Set(condition.FalseCondition(
			keystonev1.KeystoneAPIReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			keystonev1.KeystoneAPIReadyWaitingMessage))
		log.Info("KeystoneAPI not yet ready")
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	instance.Status.Conditions.MarkTrue(keystonev1.KeystoneAPIReadyCondition, keystonev1.KeystoneAPIReadyMessage)

	//
	// get admin authentication OpenStack
	//
	os, ctrlResult, err := keystonev1.GetAdminServiceClient(
		ctx,
		helper,
		keystoneAPI,
	)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			keystonev1.AdminServiceClientReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			keystonev1.AdminServiceClientReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}
	if (ctrlResult != ctrl.Result{}) {
		instance.Status.Conditions.Set(condition.FalseCondition(
			keystonev1.AdminServiceClientReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			keystonev1.AdminServiceClientReadyWaitingMessage))
		return ctrlResult, nil
	}
	instance.Status.Conditions.MarkTrue(keystonev1.AdminServiceClientReadyCondition, keystonev1.AdminServiceClientReadyMessage)

	// Handle normal service delete
	if !instance.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, instance, helper, os, keystoneAPI)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, instance, helper, os, keystoneAPI)

}

// SetupWithManager x
func (r *KeystoneServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keystonev1.KeystoneService{}).
		Complete(r)
}

func (r *KeystoneServiceReconciler) reconcileDelete(
	ctx context.Context,
	instance *keystonev1.KeystoneService,
	helper *helper.Helper,
	os *openstack.OpenStack,
	keystoneAPI *keystonev1.KeystoneAPI,
) (ctrl.Result, error) {
	log := r.GetLogger(ctx)
	log.Info("Reconciling Service delete")

	// only cleanup the service if there is the ServiceID reference in the
	// object status and if we have an OpenStack backend to use
	if instance.Status.ServiceID != "" && os != nil {
		// Delete User
		err := os.DeleteUser(
			log,
			instance.Spec.ServiceUser,
			"default")
		if err != nil {
			return ctrl.Result{}, err
		}

		// Delete Service
		err = os.DeleteService(
			log,
			instance.Status.ServiceID)
		if err != nil {
			log.Info(err.Error())
			return ctrl.Result{}, err
		}

		// Clear the service ID so that any potential requeues after this reconcile
		// will know that there is no need to worry about cleaning up the OpenStack
		// side of things anymore (deferred PatchInstance call will persist this to
		// etcd)
		instance.Status.ServiceID = ""
	} else {
		log.Info("Not deleting service as there is no stores service ID", "KeystoneService", instance.Spec.ServiceName)
	}

	// There are certain deletion scenarios where we might not have the keystoneAPI
	if keystoneAPI != nil {
		// Remove the finalizer for this service from the KeystoneAPI
		if controllerutil.RemoveFinalizer(keystoneAPI, fmt.Sprintf("%s-%s", helper.GetFinalizer(), instance.Name)) {
			err := r.Update(ctx, keystoneAPI)

			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// Service is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(instance, helper.GetFinalizer())
	log.Info("Reconciled Service delete successfully")

	return ctrl.Result{}, nil
}

func (r *KeystoneServiceReconciler) reconcileDeleteFinalizersOnly(
	ctx context.Context,
	instance *keystonev1.KeystoneService,
	helper *helper.Helper,
	keystoneAPI *keystonev1.KeystoneAPI,
) (ctrl.Result, error) {
	Log := r.GetLogger(ctx)
	Log.Info("Reconciling Service delete while KeystoneAPI is being deleted")

	if controllerutil.RemoveFinalizer(keystoneAPI, fmt.Sprintf("%s-%s", helper.GetFinalizer(), instance.Name)) {
		err := r.Update(ctx, keystoneAPI)

		if err != nil {
			return ctrl.Result{}, err
		}
	}

	controllerutil.RemoveFinalizer(instance, helper.GetFinalizer())
	Log.Info("Reconciled Service delete successfully")

	return ctrl.Result{}, nil
}

func (r *KeystoneServiceReconciler) reconcileNormal(
	ctx context.Context,
	instance *keystonev1.KeystoneService,
	helper *helper.Helper,
	os *openstack.OpenStack,
	keystoneAPI *keystonev1.KeystoneAPI,
) (ctrl.Result, error) {
	log := r.GetLogger(ctx)
	log.Info("Reconciling Service")

	//
	// Add a finalizer to the KeystoneAPI for this service instance, as we do not want the
	// KeystoneAPI to disappear before this service in the case where this service is deleted
	// (so that we can properly remove the service and user from the Keystone database on the
	// OpenStack side)
	//
	if controllerutil.AddFinalizer(keystoneAPI, fmt.Sprintf("%s-%s", helper.GetFinalizer(), instance.Name)) {
		err := r.Update(ctx, keystoneAPI)

		if err != nil {
			return ctrl.Result{}, err
		}
	}

	//
	// Create new service if ServiceID is not already set
	//
	err := r.reconcileService(ctx, instance, os)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			keystonev1.KeystoneServiceOSServiceReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			keystonev1.KeystoneServiceOSServiceReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}
	instance.Status.Conditions.MarkTrue(
		keystonev1.KeystoneServiceOSServiceReadyCondition,
		keystonev1.KeystoneServiceOSServiceReadyMessage,
		instance.Spec.ServiceName,
		instance.Status.ServiceID,
	)

	//
	// create/update service user
	//
	ctrlResult, err := r.reconcileUser(
		ctx,
		helper,
		instance,
		os)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			keystonev1.KeystoneServiceOSUserReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			keystonev1.KeystoneServiceOSUserReadyErrorMessage,
			err.Error()))
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		instance.Status.Conditions.Set(condition.FalseCondition(
			keystonev1.KeystoneServiceOSUserReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			keystonev1.KeystoneServiceOSUserReadyWaitingMessage))
		return ctrlResult, nil
	}
	instance.Status.Conditions.MarkTrue(
		keystonev1.KeystoneServiceOSUserReadyCondition,
		keystonev1.KeystoneServiceOSUserReadyMessage,
		instance.Spec.ServiceUser,
	)

	log.Info("Reconciled Service successfully")
	return ctrl.Result{}, nil
}

func (r *KeystoneServiceReconciler) reconcileService(
	ctx context.Context,
	instance *keystonev1.KeystoneService,
	os *openstack.OpenStack,
) error {
	log := r.GetLogger(ctx)
	log.Info("Reconciling Service ", "KeystoneService", instance.Spec.ServiceName)

	// verify if there is already a service in keystone for the type and name
	service, err := os.GetService(
		log,
		instance.Spec.ServiceType,
		instance.Spec.ServiceName,
	)
	// If the service is not found, don't count that as an error here,
	// it gets created bellow
	if err != nil && !strings.Contains(err.Error(), openstack.ServiceNotFound) {
		return err
	}

	if service == nil {
		// create the service
		instance.Status.ServiceID, err = os.CreateService(
			log,
			openstack.Service{
				Name:        instance.Spec.ServiceName,
				Type:        instance.Spec.ServiceType,
				Description: instance.Spec.ServiceDescription,
				Enabled:     instance.Spec.Enabled,
			})
		if err != nil {
			return err
		}
	} else {
		// During adoption there are services in the keystone DB but the
		// KeystoneService CR is fresh so we have to propagate the service ID
		// from the DB to the KeystoneService CR.
		instance.Status.ServiceID = service.ID

		if service.Enabled != instance.Spec.Enabled ||
			service.Extra["description"] != instance.Spec.ServiceDescription {
			// update the service ONLY if Enabled or Description changed.
			err := os.UpdateService(
				log,
				openstack.Service{
					Name:        instance.Spec.ServiceName,
					Type:        instance.Spec.ServiceType,
					Description: instance.Spec.ServiceDescription,
					Enabled:     instance.Spec.Enabled,
				},
				service.ID)
			if err != nil {
				return err
			}
		}
	}

	log.Info("Reconciled Service successfully")
	return nil
}

func (r *KeystoneServiceReconciler) reconcileUser(
	ctx context.Context,
	h *helper.Helper,
	instance *keystonev1.KeystoneService,
	os *openstack.OpenStack,
) (reconcile.Result, error) {
	log := r.GetLogger(ctx)
	log.Info("Reconciling User", "User", instance.Spec.ServiceUser)
	roleNames := []string{"admin", "service"}

	// get the password of the service user from the secret
	password, ctrlResult, err := secret.GetDataFromSecret(
		ctx,
		h,
		instance.Spec.Secret,
		10*time.Second,
		instance.Spec.PasswordSelector)
	if err != nil {
		return ctrl.Result{}, err
	}
	if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}

	//
	// create service project if it does not exist
	//
	serviceProjectID, err := os.CreateProject(
		log,
		openstack.Project{
			Name:        "service",
			Description: "service",
			DomainID:    "default",
		})
	if err != nil {
		return ctrl.Result{}, err
	}

	//
	// create user if it does not exist
	//
	userID, err := os.CreateUser(
		log,
		openstack.User{
			Name:      instance.Spec.ServiceUser,
			Password:  password,
			ProjectID: serviceProjectID,
			DomainID:  "default",
		})
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, roleName := range roleNames {
		//
		// create role if it does not exist
		//
		_, err = os.CreateRole(
			log,
			roleName)
		if err != nil {
			return ctrl.Result{}, err
		}

		//
		// add the role to the user
		//
		err = os.AssignUserRole(
			log,
			roleName,
			userID,
			serviceProjectID)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Info("Reconciled User successfully")
	return ctrl.Result{}, nil
}

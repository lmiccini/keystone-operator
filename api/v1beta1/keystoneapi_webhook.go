/*
Copyright 2022.

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

//
// Generated by:
//
// operator-sdk create webhook --group keystone --version v1beta1 --kind KeystoneAPI --programmatic-validation --defaulting
//

package v1beta1

import (
	"fmt"

	"github.com/openstack-k8s-operators/lib-common/modules/common/service"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// KeystoneAPIDefaults -
type KeystoneAPIDefaults struct {
	ContainerImageURL string
}

var keystoneAPIDefaults KeystoneAPIDefaults

// log is for logging in this package.
var keystoneapilog = logf.Log.WithName("keystoneapi-resource")

// SetupKeystoneAPIDefaults - initialize KeystoneAPI spec defaults for use with either internal or external webhooks
func SetupKeystoneAPIDefaults(defaults KeystoneAPIDefaults) {
	keystoneAPIDefaults = defaults
	keystoneapilog.Info("KeystoneAPI defaults initialized", "defaults", defaults)
}

// SetupWebhookWithManager sets up the webhook with the Manager
func (r *KeystoneAPI) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-keystone-openstack-org-v1beta1-keystoneapi,mutating=true,failurePolicy=fail,sideEffects=None,groups=keystone.openstack.org,resources=keystoneapis,verbs=create;update,versions=v1beta1,name=mkeystoneapi.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &KeystoneAPI{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *KeystoneAPI) Default() {
	keystoneapilog.Info("default", "name", r.Name)

	if r.Spec.ContainerImage == "" {
		r.Spec.ContainerImage = keystoneAPIDefaults.ContainerImageURL
	}
	r.Spec.Default()
}

// Default - set defaults for this KeystoneAPI spec
func (spec *KeystoneAPISpec) Default() {
	// no defaults to set yet
	spec.KeystoneAPISpecCore.Default()
}

// Default - set defaults for this KeystoneAPI core spec
// NOTE: only this version is used by OpenStackOperators webhook
func (spec *KeystoneAPISpecCore) Default() {
	// no defaults to set yet
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-keystone-openstack-org-v1beta1-keystoneapi,mutating=false,failurePolicy=fail,sideEffects=None,groups=keystone.openstack.org,resources=keystoneapis,verbs=create;update,versions=v1beta1,name=vkeystoneapi.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &KeystoneAPI{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *KeystoneAPI) ValidateCreate() (admission.Warnings, error) {
	keystoneapilog.Info("validate create", "name", r.Name)

	allErrs := field.ErrorList{}
	basePath := field.NewPath("spec")

	if err := r.Spec.ValidateCreate(basePath); err != nil {
		allErrs = append(allErrs, err...)
	}

	if len(allErrs) != 0 {
		return nil, apierrors.NewInvalid(GroupVersion.WithKind("KeystoneAPI").GroupKind(), r.Name, allErrs)
	}

	return nil, nil
}

// ValidateCreate - Exported function wrapping non-exported validate functions,
// this function can be called externally to validate an KeystoneAPI spec.
func (spec *KeystoneAPISpec) ValidateCreate(basePath *field.Path) field.ErrorList {
	return spec.KeystoneAPISpecCore.ValidateCreate(basePath)
}

func (spec *KeystoneAPISpecCore) ValidateCreate(basePath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// validate the service override key is valid
	allErrs = append(allErrs, service.ValidateRoutedOverrides(basePath.Child("override").Child("service"), spec.Override.Service)...)

	return allErrs
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *KeystoneAPI) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	keystoneapilog.Info("validate update", "name", r.Name)

	oldKeystoneAPI, ok := old.(*KeystoneAPI)
	if !ok || oldKeystoneAPI == nil {
		return nil, apierrors.NewInternalError(fmt.Errorf("unable to convert existing object"))
	}

	allErrs := field.ErrorList{}
	basePath := field.NewPath("spec")

	if err := r.Spec.ValidateUpdate(oldKeystoneAPI.Spec, basePath); err != nil {
		allErrs = append(allErrs, err...)
	}

	if len(allErrs) != 0 {
		return nil, apierrors.NewInvalid(GroupVersion.WithKind("KeystoneAPI").GroupKind(), r.Name, allErrs)
	}

	return nil, nil
}

// ValidateUpdate - Exported function wrapping non-exported validate functions,
// this function can be called externally to validate an ironic spec.
func (spec *KeystoneAPISpec) ValidateUpdate(old KeystoneAPISpec, basePath *field.Path) field.ErrorList {
	return spec.KeystoneAPISpecCore.ValidateUpdate(old.KeystoneAPISpecCore, basePath)
}

func (spec *KeystoneAPISpecCore) ValidateUpdate(_ KeystoneAPISpecCore, basePath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// validate the service override key is valid
	allErrs = append(allErrs, service.ValidateRoutedOverrides(basePath.Child("override").Child("service"), spec.Override.Service)...)

	return allErrs
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *KeystoneAPI) ValidateDelete() (admission.Warnings, error) {
	keystoneapilog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

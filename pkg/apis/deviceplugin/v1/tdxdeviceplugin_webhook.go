// Copyright 2023 Intel Corporation. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1

import (
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/intel/intel-device-plugins-for-kubernetes/pkg/controllers"
)

const (
	tdxPluginKind = "TdxDevicePlugin"
)

var (
	// tdxdevicepluginlog is for logging in this package.
	tdxdevicepluginlog = logf.Log.WithName("tdxdeviceplugin-resource")

	tdxMinVersion = controllers.ImageMinVersion
)

// SetupWebhookWithManager sets up a webhook for TdxDevicePlugin custom resources.
func (r *TdxDevicePlugin) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-deviceplugin-intel-com-v1-tdxdeviceplugin,mutating=true,failurePolicy=fail,groups=deviceplugin.intel.com,resources=tdxdeviceplugins,verbs=create;update,versions=v1,name=mtdxdeviceplugin.kb.io,sideEffects=None,admissionReviewVersions=v1,reinvocationPolicy=IfNeeded

var _ webhook.Defaulter = &TdxDevicePlugin{}

// Default implements webhook.Defaulter so a webhook will be registered for the type.
func (r *TdxDevicePlugin) Default() {
	tdxdevicepluginlog.Info("default", "name", r.Name)

	if len(r.Spec.Image) == 0 {
		r.Spec.Image = "intel/intel-tdx-plugin:" + tdxMinVersion.String()
	}
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-deviceplugin-intel-com-v1-tdxdeviceplugin,mutating=false,failurePolicy=fail,groups=deviceplugin.intel.com,resources=tdxdeviceplugins,versions=v1,name=vtdxdeviceplugin.kb.io,sideEffects=None,admissionReviewVersions=v1

var _ webhook.Validator = &TdxDevicePlugin{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *TdxDevicePlugin) ValidateCreate() (admission.Warnings, error) {
	tdxdevicepluginlog.Info("validate create", "name", r.Name)

	if controllers.GetDevicePluginCount(tdxPluginKind) > 0 {
		return nil, errors.Errorf("an instance of %q already exists in the cluster", tdxPluginKind)
	}

	return nil, r.validatePlugin()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *TdxDevicePlugin) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	tdxdevicepluginlog.Info("validate update", "name", r.Name)

	return nil, r.validatePlugin()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *TdxDevicePlugin) ValidateDelete() (admission.Warnings, error) {
	tdxdevicepluginlog.Info("validate delete", "name", r.Name)

	return nil, nil
}

func (r *TdxDevicePlugin) validatePlugin() error {
	if err := validatePluginImage(r.Spec.Image, "intel-tdx-plugin", tdxMinVersion); err != nil {
		return err
	}

	if r.Spec.InitImage == "" {
		return nil
	}

	return validatePluginImage(r.Spec.InitImage, "intel-tdx-initcontainer", tdxMinVersion)
}

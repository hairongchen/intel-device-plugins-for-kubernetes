// Copyright 2020-2022 Intel Corporation. All Rights Reserved.
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

// Package tdx contains TDX specific reconciliation logic.
package tdx

import (
	"context"
	"reflect"
	"strconv"
	"strings"

	"github.com/intel/intel-device-plugins-for-kubernetes/deployments"
	devicepluginv1 "github.com/intel/intel-device-plugins-for-kubernetes/pkg/apis/deviceplugin/v1"
	"github.com/intel/intel-device-plugins-for-kubernetes/pkg/controllers"
	"github.com/pkg/errors"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ownerKey = ".metadata.controller.tdx"

var defaultNodeSelector = deployments.TDXPluginDaemonSet().Spec.Template.Spec.NodeSelector

// +kubebuilder:rbac:groups=deviceplugin.intel.com,resources=tdxdeviceplugins,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=deviceplugin.intel.com,resources=tdxdeviceplugins/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=deviceplugin.intel.com,resources=tdxdeviceplugins/finalizers,verbs=update

// SetupReconciler creates a new reconciler for TdxDevicePlugin objects.
func SetupReconciler(mgr ctrl.Manager, namespace string, withWebhook bool) error {
	c := &controller{scheme: mgr.GetScheme(), ns: namespace}
	if err := controllers.SetupWithManager(mgr, c, devicepluginv1.GroupVersion.String(), "TdxDevicePlugin", ownerKey); err != nil {
		return err
	}

	// if withWebhook {
	// 	return (&devicepluginv1.TdxDevicePlugin{}).SetupWebhookWithManager(mgr)
	// }

	return nil
}

type controller struct {
	controllers.DefaultServiceAccountFactory
	scheme *runtime.Scheme
	ns     string
}

func (c *controller) Upgrade(ctx context.Context, obj client.Object) bool {
	dp := obj.(*devicepluginv1.TdxDevicePlugin)
	return controllers.UpgradeImages(&dp.Spec.Image, &dp.Spec.InitImage)
}

func (c *controller) CreateEmptyObject() client.Object {
	return &devicepluginv1.TdxDevicePlugin{}
}

func (c *controller) GetTotalObjectCount(ctx context.Context, clnt client.Client) (int, error) {
	var list devicepluginv1.TdxDevicePluginList
	if err := clnt.List(ctx, &list); err != nil {
		return 0, err
	}

	return len(list.Items), nil
}

func addVolumeIfMissing(spec *v1.PodSpec, name, path string, hpType v1.HostPathType) {
	for _, vol := range spec.Volumes {
		if vol.Name == name {
			return
		}
	}

	spec.Volumes = append(spec.Volumes, v1.Volume{
		Name: name,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: path,
				Type: &hpType,
			},
		},
	})
}

func setInitContainer(spec *v1.PodSpec, imageName string) {
	yes := true
	spec.InitContainers = []v1.Container{
		{
			Image:           imageName,
			ImagePullPolicy: "IfNotPresent",
			Name:            "intel-tdx-initcontainer",
			SecurityContext: &v1.SecurityContext{
				SELinuxOptions: &v1.SELinuxOptions{
					Type: "container_device_plugin_init_t",
				},
				ReadOnlyRootFilesystem: &yes,
			},
			VolumeMounts: []v1.VolumeMount{
				{
					MountPath: "/etc/kubernetes/node-feature-discovery/source.d/",
					Name:      "nfd-features",
				},
			},
		}}
	addVolumeIfMissing(spec, "nfd-features", "/etc/kubernetes/node-feature-discovery/source.d/", v1.HostPathDirectoryOrCreate)
}

func (c *controller) NewDaemonSet(rawObj client.Object) *apps.DaemonSet {
	devicePlugin := rawObj.(*devicepluginv1.TdxDevicePlugin)

	daemonSet := deployments.TDXPluginDaemonSet()
	if len(devicePlugin.Spec.NodeSelector) > 0 {
		daemonSet.Spec.Template.Spec.NodeSelector = devicePlugin.Spec.NodeSelector
	}

	daemonSet.ObjectMeta.Namespace = c.ns

	daemonSet.Spec.Template.Spec.Containers[0].Args = getPodArgs(devicePlugin)
	daemonSet.Spec.Template.Spec.Containers[0].Image = devicePlugin.Spec.Image

	// add the optional init container
	if devicePlugin.Spec.InitImage != "" {
		setInitContainer(&daemonSet.Spec.Template.Spec, devicePlugin.Spec.InitImage)
	}

	return daemonSet
}

func removeVolume(volumes []v1.Volume, name string) []v1.Volume {
	newVolumes := []v1.Volume{}

	for _, volume := range volumes {
		if volume.Name != name {
			newVolumes = append(newVolumes, volume)
		}
	}

	return newVolumes
}

func (c *controller) UpdateDaemonSet(rawObj client.Object, ds *apps.DaemonSet) (updated bool) {
	dp := rawObj.(*devicepluginv1.TdxDevicePlugin)

	if ds.Spec.Template.Spec.Containers[0].Image != dp.Spec.Image {
		ds.Spec.Template.Spec.Containers[0].Image = dp.Spec.Image
		updated = true
	}

	if dp.Spec.InitImage == "" {
		if ds.Spec.Template.Spec.InitContainers != nil {
			ds.Spec.Template.Spec.InitContainers = nil
			ds.Spec.Template.Spec.Volumes = removeVolume(ds.Spec.Template.Spec.Volumes, "nfd-features")
			updated = true
		}
	} else {
		setInitContainer(&ds.Spec.Template.Spec, dp.Spec.InitImage)
		updated = true
	}

	if len(dp.Spec.NodeSelector) > 0 {
		if !reflect.DeepEqual(ds.Spec.Template.Spec.NodeSelector, dp.Spec.NodeSelector) {
			ds.Spec.Template.Spec.NodeSelector = dp.Spec.NodeSelector
			updated = true
		}
	} else if !reflect.DeepEqual(ds.Spec.Template.Spec.NodeSelector, defaultNodeSelector) {
		ds.Spec.Template.Spec.NodeSelector = defaultNodeSelector
		updated = true
	}

	newargs := getPodArgs(dp)
	if strings.Join(ds.Spec.Template.Spec.Containers[0].Args, " ") != strings.Join(newargs, " ") {
		ds.Spec.Template.Spec.Containers[0].Args = newargs
		updated = true
	}

	return updated
}

func (c *controller) UpdateStatus(rawObj client.Object, ds *apps.DaemonSet, nodeNames []string) (updated bool, err error) {
	dp := rawObj.(*devicepluginv1.TdxDevicePlugin)

	dsRef, err := reference.GetReference(c.scheme, ds)
	if err != nil {
		return false, errors.Wrap(err, "unable to make reference to controlled daemon set")
	}

	if dp.Status.ControlledDaemonSet.UID != dsRef.UID {
		dp.Status.ControlledDaemonSet = *dsRef
		updated = true
	}

	if dp.Status.DesiredNumberScheduled != ds.Status.DesiredNumberScheduled {
		dp.Status.DesiredNumberScheduled = ds.Status.DesiredNumberScheduled
		updated = true
	}

	if dp.Status.NumberReady != ds.Status.NumberReady {
		dp.Status.NumberReady = ds.Status.NumberReady
		updated = true
	}

	if strings.Join(dp.Status.NodeNames, ",") != strings.Join(nodeNames, ",") {
		dp.Status.NodeNames = nodeNames
		updated = true
	}

	return updated, nil
}

func getPodArgs(sdp *devicepluginv1.TdxDevicePlugin) []string {
	args := make([]string, 0, 4)
	args = append(args, "-v", strconv.Itoa(sdp.Spec.LogLevel))

	if sdp.Spec.TdxLimit > 0 {
		args = append(args, "-tdx-limit", strconv.Itoa(sdp.Spec.TdxLimit))
	} else {
		args = append(args, "-tdx-limit", "1")
	}

	return args
}
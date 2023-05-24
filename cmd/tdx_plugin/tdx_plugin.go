package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"runtime"
	"strconv"

	dpapi "github.com/intel/intel-device-plugins-for-kubernetes/pkg/deviceplugin"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	// Device plugin settings.
	namespace                   = "tdx.intel.com"
	deviceType                  = "tdx-guest"
	devicePath                  = "/dev"
	podsPerCoreEnvVariable      = "PODS_PER_CORE"
	defaultPodCount        uint = 110
)

type devicePlugin struct {
	scanDone chan bool
	devfsDir string
	nTDX     uint
}

func newDevicePlugin(devfsDir string, nTDX uint) *devicePlugin {
	return &devicePlugin{
		devfsDir: devfsDir,
		nTDX:     nTDX,
		scanDone: make(chan bool, 1),
	}
}

func (dp *devicePlugin) Scan(notifier dpapi.Notifier) error {
	devTree, err := dp.scan()
	if err != nil {
		return err
	}

	notifier.Notify(devTree)

	// Wait forever to prevent manager run loop from exiting.
	<-dp.scanDone

	return nil
}

func (dp *devicePlugin) scan() (dpapi.DeviceTree, error) {
	devTree := dpapi.NewDeviceTree()

	// Assume /dev/tdx-guest exists
	tdxDevicePath := path.Join(dp.devfsDir, "tdx-guest")

	if _, err := os.Stat(tdxDevicePath); err != nil {
		klog.Error("No TDX device file available: ", err)
		return devTree, nil
	}

	for i := uint(0); i < dp.nTDX; i++ {
		devID := fmt.Sprintf("%s-%d", "tdx-guest", i)
		nodes := []pluginapi.DeviceSpec{{HostPath: tdxDevicePath, ContainerPath: tdxDevicePath, Permissions: "rw"}}
		devTree.AddDevice(deviceType, devID, dpapi.NewDeviceInfoWithTopologyHints(pluginapi.Healthy, nodes, nil, nil, nil, nil))
	}

	return devTree, nil
}

func getDefaultPodCount(nCPUs uint) uint {
	// By default we provide as many enclave resources as there can be pods
	// running on the node. The problem is that this value is configurable
	// either via "--pods-per-core" or "--max-pods" kubelet options. We get the
	// limit by multiplying the number of cores in the system with env variable
	// "PODS_PER_CORE".
	envPodsPerCore := os.Getenv(podsPerCoreEnvVariable)
	if envPodsPerCore != "" {
		tmp, err := strconv.ParseUint(envPodsPerCore, 10, 32)
		if err != nil {
			klog.Errorf("Error: failed to parse %s value as uint, using default value.", podsPerCoreEnvVariable)
		} else {
			return uint(tmp) * nCPUs
		}
	}

	return defaultPodCount
}

func main() {
	var tdxLimit uint

	podCount := getDefaultPodCount(uint(runtime.NumCPU()))

	flag.UintVar(&tdxLimit, "tdx-limit", podCount, "Number of \"tdx\" resources")
	flag.Parse()

	klog.V(4).Infof("TDX device plugin started with %d \"%s/tdx\" resources.", tdxLimit, namespace)

	plugin := newDevicePlugin(devicePath, tdxLimit)
	manager := dpapi.NewManager(namespace, plugin)
	manager.Run()
}

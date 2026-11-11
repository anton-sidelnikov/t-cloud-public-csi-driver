package backend

import (
	"errors"
	"fmt"
	"slices"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

// Validate checks that the advertised CSI capabilities of a backend match the
// interfaces its driver and service implement.
func Validate(driver Driver, service Service) error {
	if driver == nil || service == nil {
		return errors.New("backend driver and service are required")
	}

	_, isBlock := driver.(BlockDriver)
	_, isShare := driver.(FileShareDriver)
	if isBlock == isShare {
		return fmt.Errorf("backend %s must implement exactly one of BlockDriver or FileShareDriver", driver.Name())
	}

	controllerCaps := driver.ControllerCapabilities()
	nodeCaps := driver.NodeCapabilities()

	if !slices.Contains(nodeCaps, csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME) {
		return fmt.Errorf("backend %s must support node STAGE_UNSTAGE_VOLUME", driver.Name())
	}

	publish := slices.Contains(controllerCaps, csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME)
	if isBlock && !publish {
		return fmt.Errorf("block backend %s must support controller PUBLISH_UNPUBLISH_VOLUME", driver.Name())
	}
	if isShare && publish {
		return fmt.Errorf("file share backend %s must not support controller PUBLISH_UNPUBLISH_VOLUME", driver.Name())
	}
	if _, ok := service.(Attacher); publish && !ok {
		return fmt.Errorf("backend %s advertises PUBLISH_UNPUBLISH_VOLUME but its service does not implement Attacher", driver.Name())
	}

	if isShare && slices.Contains(nodeCaps, csi.NodeServiceCapability_RPC_EXPAND_VOLUME) {
		return fmt.Errorf("file share backend %s must not support node EXPAND_VOLUME", driver.Name())
	}

	snapshots := slices.Contains(controllerCaps, csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT) ||
		slices.Contains(controllerCaps, csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS)
	if _, ok := service.(Snapshotter); snapshots && !ok {
		return fmt.Errorf("backend %s advertises snapshot capabilities but its service does not implement Snapshotter", driver.Name())
	}

	return nil
}

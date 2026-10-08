package step

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-xcode/v2/destination"
	"github.com/bitrise-io/go-xcode/v2/simulator"
	"github.com/bitrise-io/go-xcode/v2/xcodeversion"
)

const (
	iosBootTimeout = 5 * time.Minute
	// The simulator Bitrise macOS stacks create for every iOS runtime, also the default of Xcode Test.
	defaultSimulatorDestination = "platform=iOS Simulator,name=Bitrise iOS default,OS=latest"
	iosRuntimePrefix            = "com.apple.CoreSimulator.SimRuntime.iOS-"
	simulatorShutdownState      = "Shutdown"
)

type iosDevices struct {
	logger           log.Logger
	deviceFinder     destination.DeviceFinder
	simulatorManager simulator.Manager
	destinationHint  string
}

func newIOSDevices(logger log.Logger, commandFactory command.Factory, destinationHint string) iosDevices {
	xcodeVersion, err := xcodeversion.NewXcodeVersionProvider(commandFactory).GetVersion()
	if err != nil {
		logger.Warnf("Failed to read the Xcode version: %s", err)
	}
	return iosDevices{
		logger:           logger,
		deviceFinder:     destination.NewDeviceFinder(logger, commandFactory, xcodeVersion),
		simulatorManager: simulator.NewManager(logger, commandFactory),
		destinationHint:  destinationHint,
	}
}

func (i iosDevices) acquire() (Device, error) {
	if i.destinationHint != "" {
		i.logger.Printf("Using the simulator from %s: %s", xcodeDestinationEnv, i.destinationHint)
		return i.useDestination(i.destinationHint)
	}

	list, err := i.deviceFinder.ListDevices()
	if err != nil {
		return Device{}, err
	}
	udid, err := pickRunningDevice(runningSimulators(list))
	if err != nil {
		return Device{}, err
	}
	if udid != "" {
		i.logger.Printf("Using the running simulator: %s", udid)
		if err := i.simulatorManager.WaitForBootFinished(udid, iosBootTimeout); err != nil {
			return Device{}, err
		}
		return Device{ID: udid}, nil
	}

	i.logger.Printf("No running simulator, booting %s", defaultSimulatorDestination)
	return i.useDestination(defaultSimulatorDestination)
}

func (i iosDevices) useDestination(dest string) (Device, error) {
	sim, err := destination.NewSimulator(dest)
	if err != nil {
		return Device{}, fmt.Errorf("invalid destination (%s): %w", dest, err)
	}
	device, err := i.deviceFinder.FindDevice(*sim)
	if err != nil {
		return Device{}, fmt.Errorf("find simulator (%s): %w", dest, err)
	}
	i.logger.Printf("Simulator: %s, %s, %s (%s)", device.Name, device.OS, device.UDID, device.State)

	if device.State != simulatorShutdownState {
		if err := i.simulatorManager.WaitForBootFinished(device.UDID, iosBootTimeout); err != nil {
			return Device{}, err
		}
		return Device{ID: device.UDID}, nil
	}

	start := time.Now()
	release := func() {
		i.logger.Println()
		i.logger.Infof("Shutting down the simulator the step started")
		if err := i.simulatorManager.Shutdown(device.UDID); err != nil {
			i.logger.Warnf("%s", err)
		}
	}
	if err := i.simulatorManager.Boot(device); err != nil {
		return Device{}, err
	}
	if err := i.simulatorManager.WaitForBootFinished(device.UDID, iosBootTimeout); err != nil {
		release()
		return Device{}, err
	}
	i.logger.Donef("Simulator booted in %s", time.Since(start).Round(time.Second))
	return Device{
		ID:        device.UDID,
		Release:   release,
		HintEnv:   xcodeDestinationEnv,
		HintValue: fmt.Sprintf("platform=%s,name=%s,OS=%s", device.Platform, device.Name, device.OS),
	}, nil
}

// runningSimulators counts Booting simulators as running too, so the step never boots a second one next to them.
func runningSimulators(list *destination.DeviceList) []string {
	var udids []string
	for _, runtimeID := range slices.Sorted(maps.Keys(list.Devices)) {
		if !strings.HasPrefix(runtimeID, iosRuntimePrefix) {
			continue
		}
		for _, device := range list.Devices[runtimeID] {
			if device.State != simulatorShutdownState {
				udids = append(udids, device.UDID)
			}
		}
	}
	return udids
}

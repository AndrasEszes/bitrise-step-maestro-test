package step

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/bitrise-io/go-android/v2/adbmanager"
	"github.com/bitrise-io/go-android/v2/sdk"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/pathutil"
	"github.com/hashicorp/go-version"
)

const (
	androidBootTimeout     = 10 * time.Minute
	emulatorCheckInterval  = 5 * time.Second
	emulatorStopTimeout    = time.Minute
	avdName                = "bitrise_maestro"
	avdDeviceProfile       = "pixel"
	systemImagePrefix      = "system-images"
	ps16kSystemImageSuffix = "_ps16k"
)

// The first tag with an image for the host ABI wins, then the highest API level within it.
var preferredSystemImageTags = []string{"google_apis", "google_apis_ps16k", "aosp_atd"}

var animationSettings = []string{"window_animation_scale", "transition_animation_scale", "animator_duration_scale"}

type androidDevices struct {
	logger         log.Logger
	commandFactory command.Factory
	androidHome    string
	serialHint     string
	systemImage    string
}

func newAndroidDevices(logger log.Logger, commandFactory command.Factory, androidHome, serialHint, systemImage string) androidDevices {
	return androidDevices{
		logger:         logger,
		commandFactory: commandFactory,
		androidHome:    androidHome,
		serialHint:     serialHint,
		systemImage:    systemImage,
	}
}

func (a androidDevices) acquire() (Device, error) {
	sdkModel, err := sdk.New(a.androidHome, pathutil.NewPathChecker())
	if err != nil {
		return Device{}, fmt.Errorf("init Android SDK: %w", err)
	}
	adb, err := adbmanager.New(sdkModel, a.commandFactory, a.logger)
	if err != nil {
		return Device{}, err
	}

	serial := a.serialHint
	if serial != "" {
		a.logger.Printf("Using the emulator from %s: %s", emulatorSerialEnv, serial)
	} else {
		running, err := a.runningDevices()
		if err != nil {
			return Device{}, err
		}
		if serial, err = pickRunningDevice(running); err != nil {
			return Device{}, err
		}
		if serial != "" {
			a.logger.Printf("Using the running device: %s", serial)
		}
	}
	if serial != "" {
		if err := adb.WaitForDevice(serial, androidBootTimeout); err != nil {
			return Device{}, err
		}
		return Device{ID: serial, Release: func() {}}, nil
	}

	a.logger.Printf("No running device, booting an emulator")
	return a.boot(sdkModel, adb)
}

func (a androidDevices) boot(sdkModel *sdk.Model, adb *adbmanager.Model) (Device, error) {
	cmdlineToolsPath, err := sdkModel.CmdlineToolsPath()
	if err != nil {
		return Device{}, err
	}

	image, err := a.resolveSystemImage(filepath.Join(cmdlineToolsPath, "sdkmanager"))
	if err != nil {
		return Device{}, err
	}
	if err := a.createAVD(filepath.Join(cmdlineToolsPath, "avdmanager"), image); err != nil {
		return Device{}, err
	}

	start := time.Now()
	emulator, err := a.startEmulator()
	if err != nil {
		return Device{}, err
	}
	release := func() { emulator.stop(a.logger, a.commandFactory) }

	if err := adb.WaitForDevice(emulator.serial, androidBootTimeout-time.Since(start)); err != nil {
		release()
		return Device{}, err
	}
	a.logger.Donef("Emulator %s booted in %s", emulator.serial, time.Since(start).Round(time.Second))

	if err := a.disableAnimations(emulator.serial); err != nil {
		release()
		return Device{}, err
	}
	return Device{ID: emulator.serial, Release: release}, nil
}

func (a androidDevices) resolveSystemImage(sdkManagerPath string) (string, error) {
	installed, err := installedSystemImages(a.androidHome)
	if err != nil {
		return "", err
	}

	if a.systemImage == "" {
		image, err := selectSystemImage(installed, hostABI(runtime.GOARCH))
		if err != nil {
			return "", err
		}
		a.logger.Printf("System image: %s (newest preinstalled)", image)
		return image, nil
	}

	a.logger.Printf("System image: %s", a.systemImage)
	if slices.Contains(installed, a.systemImage) {
		return a.systemImage, nil
	}

	cmd := a.commandFactory.Create(sdkManagerPath, []string{"--verbose", a.systemImage}, &command.Opts{
		Stdin: strings.NewReader(strings.Repeat("y\n", 20)),
	})
	a.logger.Printf("The system image is not preinstalled, installing it")
	a.logger.TDonef("$ %s", cmd.PrintableCommandArgs())
	start := time.Now()
	if out, err := cmd.RunAndReturnTrimmedCombinedOutput(); err != nil {
		return "", fmt.Errorf("install system image %s: %w\n%s", a.systemImage, err, out)
	}
	a.logger.Printf("Installed in %s", time.Since(start).Round(time.Second))
	return a.systemImage, nil
}

func (a androidDevices) createAVD(avdManagerPath, image string) error {
	parts := strings.Split(image, ";")
	tag, abi := parts[2], parts[3]
	args := []string{"--verbose", "create", "avd", "--force", "--name", avdName, "--device", avdDeviceProfile, "--package", image, "--abi", abi}
	// The valid avdmanager tag of a 16 KB page size image varies by API level, so avdmanager picks it.
	if !strings.HasSuffix(tag, ps16kSystemImageSuffix) {
		args = append(args, "--tag", tag)
	}

	cmd := a.commandFactory.Create(avdManagerPath, args, &command.Opts{
		// Answers "no" to creating a custom hardware profile.
		Stdin: strings.NewReader(strings.Repeat("no\n", 20)),
	})
	a.logger.TDonef("$ %s", cmd.PrintableCommandArgs())
	if out, err := cmd.RunAndReturnTrimmedCombinedOutput(); err != nil {
		return fmt.Errorf("create emulator: %w\n%s", err, out)
	}
	return nil
}

type runningEmulator struct {
	serial  string
	cmd     command.Command
	exited  chan struct{}
	logPath string
}

func (a androidDevices) startEmulator() (runningEmulator, error) {
	logFile, err := os.CreateTemp("", "emulator-*.log")
	if err != nil {
		return runningEmulator{}, err
	}

	args := []string{
		"@" + avdName,
		"-no-window", "-no-boot-anim", "-no-audio",
		"-no-snapshot", "-wipe-data",
		"-netdelay", "none",
		"-gpu", "auto",
		"-camera-back", "none", "-camera-front", "none",
	}
	cmd := a.commandFactory.Create(filepath.Join(a.androidHome, "emulator", "emulator"), args, &command.Opts{Stdout: logFile, Stderr: logFile})
	a.logger.TDonef("$ %s", cmd.PrintableCommandArgs())
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return runningEmulator{}, fmt.Errorf("start emulator: %w", err)
	}

	emulator := runningEmulator{cmd: cmd, exited: make(chan struct{}), logPath: logFile.Name()}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		close(emulator.exited)
	}()

	// The step only boots when no device runs, so the first device adb reports is this emulator.
	deadline := time.Now().Add(androidBootTimeout)
	for emulator.serial == "" {
		select {
		case <-emulator.exited:
			return runningEmulator{}, fmt.Errorf("emulator exited before it came online, see %s", emulator.logPath)
		case <-time.After(emulatorCheckInterval):
		}

		running, err := a.runningDevices()
		if err != nil {
			emulator.stop(a.logger, a.commandFactory)
			return runningEmulator{}, err
		}
		if len(running) > 0 {
			emulator.serial = running[0]
		} else if time.Now().After(deadline) {
			emulator.stop(a.logger, a.commandFactory)
			return runningEmulator{}, fmt.Errorf("emulator did not come online in %s, see %s", androidBootTimeout, emulator.logPath)
		}
	}
	return emulator, nil
}

func (e runningEmulator) stop(logger log.Logger, commandFactory command.Factory) {
	logger.Println()
	logger.Infof("Shutting down the emulator the step started")
	if e.serial != "" {
		cmd := commandFactory.Create("adb", []string{"-s", e.serial, "emu", "kill"}, nil)
		logger.TDonef("$ %s", cmd.PrintableCommandArgs())
		if out, err := cmd.RunAndReturnTrimmedCombinedOutput(); err != nil {
			logger.Warnf("adb emu kill: %s\n%s", err, out)
		}
		select {
		case <-e.exited:
			return
		case <-time.After(emulatorStopTimeout):
			logger.Warnf("The emulator did not exit in %s, killing it", emulatorStopTimeout)
		}
	}
	if err := e.cmd.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		logger.Warnf("Kill emulator: %s", err)
	}
}

func (a androidDevices) disableAnimations(serial string) error {
	a.logger.Printf("Disabling animations")
	for _, setting := range animationSettings {
		cmd := a.commandFactory.Create("adb", []string{"-s", serial, "shell", "settings", "put", "global", setting, "0"}, nil)
		if out, err := cmd.RunAndReturnTrimmedCombinedOutput(); err != nil {
			return fmt.Errorf("disable animations (%s): %w\n%s", setting, err, out)
		}
	}
	return nil
}

func (a androidDevices) runningDevices() ([]string, error) {
	cmd := a.commandFactory.Create("adb", []string{"devices"}, nil)
	out, err := cmd.RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w\n%s", err, out)
	}
	return parseADBDevices(out), nil
}

// parseADBDevices returns every serial adb lists, offline ones included: a device that is still booting
// shows up as offline, and the step must not boot a second one next to it.
func parseADBDevices(out string) []string {
	var serials []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.HasPrefix(line, "*") {
			continue
		}
		serials = append(serials, fields[0])
	}
	return serials
}

func installedSystemImages(androidHome string) ([]string, error) {
	dirs, err := filepath.Glob(filepath.Join(androidHome, systemImagePrefix, "*", "*", "*"))
	if err != nil {
		return nil, err
	}

	var images []string
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		rel, err := filepath.Rel(androidHome, dir)
		if err != nil {
			return nil, err
		}
		images = append(images, strings.Join(strings.Split(rel, string(filepath.Separator)), ";"))
	}
	return images, nil
}

func selectSystemImage(installed []string, abi string) (string, error) {
	for _, tag := range preferredSystemImageTags {
		var best string
		var bestAPILevel *version.Version
		for _, image := range installed {
			parts := strings.Split(image, ";")
			if len(parts) != 4 || parts[2] != tag || parts[3] != abi {
				continue
			}
			apiLevel, err := version.NewVersion(strings.TrimPrefix(parts[1], "android-"))
			if err != nil {
				continue
			}
			if bestAPILevel == nil || apiLevel.GreaterThan(bestAPILevel) {
				best, bestAPILevel = image, apiLevel
			}
		}
		if best != "" {
			return best, nil
		}
	}
	return "", fmt.Errorf("no preinstalled %s system image (%s) in the Android SDK: set android_system_image, or start an emulator before this Step", abi, strings.Join(preferredSystemImageTags, ", "))
}

func hostABI(goarch string) string {
	if goarch == "arm64" {
		return "arm64-v8a"
	}
	return "x86_64"
}

func validateSystemImage(image string) error {
	if image == "" {
		return nil
	}
	parts := strings.Split(image, ";")
	if len(parts) != 4 || parts[0] != systemImagePrefix {
		return fmt.Errorf("android_system_image must look like system-images;android-<API level>;<tag>;<ABI>, got: %s", image)
	}
	return nil
}

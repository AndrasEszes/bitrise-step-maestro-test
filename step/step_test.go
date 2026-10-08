package step

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_DisablesMaestroAnalyticsAndUpdateCheck(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "env.txt")
	fakeMaestro := filepath.Join(t.TempDir(), "maestro")
	writeFile(t, fakeMaestro, "#!/bin/sh\nenv > \""+envFile+"\"\n")
	require.NoError(t, os.Chmod(fakeMaestro, 0755))

	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	_, err := s.Run(Config{FlowPaths: []string{".maestro"}}, Installation{BinaryPath: fakeMaestro})
	require.NoError(t, err)

	maestroEnv, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.Contains(t, string(maestroEnv), "MAESTRO_CLI_NO_ANALYTICS=true\n")
	assert.Contains(t, string(maestroEnv), "MAESTRO_DISABLE_UPDATE_CHECK=true\n")
	assert.Contains(t, string(maestroEnv), "PATH=", "the rest of the environment is passed through")
}

func TestConfigFromInput_FlowPaths(t *testing.T) {
	config, err := configFromInput(Input{FlowPath: "maestro/entry\nmaestro/unlock\n", TestName: "Maestro"})
	require.NoError(t, err)
	assert.Equal(t, []string{"maestro/entry", "maestro/unlock"}, config.FlowPaths)

	_, err = configFromInput(Input{FlowPath: " \n ", TestName: "Maestro"})
	require.ErrorContains(t, err, "flow_path")
}

func TestConfigFromInput_ManageDevice(t *testing.T) {
	config, err := configFromInput(Input{FlowPath: ".maestro", TestName: "Maestro", AdditionalArgs: "-e A=b"})
	require.NoError(t, err)
	assert.True(t, config.ManageDevice)

	config, err = configFromInput(Input{FlowPath: ".maestro", TestName: "Maestro", AdditionalArgs: "--device emulator-5554"})
	require.NoError(t, err)
	assert.False(t, config.ManageDevice, "a device picked in additional_args is left to Maestro")

	_, err = configFromInput(Input{FlowPath: ".maestro", TestName: "Maestro", AndroidSystemImage: "android-36"})
	require.ErrorContains(t, err, "android_system_image")
}

func TestRun_RunsOnTheAcquiredDeviceAndReleasesIt(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	fakeMaestro := filepath.Join(t.TempDir(), "maestro")
	writeFile(t, fakeMaestro, "#!/bin/sh\necho \"$@\" > \""+argsFile+"\"\nexit 1\n")
	require.NoError(t, os.Chmod(fakeMaestro, 0755))

	devices := &fakeDevices{device: Device{ID: "emulator-5554"}}
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	s.devices = devices

	_, err := s.Run(Config{FlowPaths: []string{".maestro"}, ManageDevice: true}, Installation{BinaryPath: fakeMaestro})
	require.ErrorContains(t, err, "maestro test failed")

	maestroArgs, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Contains(t, string(maestroArgs), "--device emulator-5554")
	assert.True(t, devices.released, "the device is released even when the flows fail")
}

func TestRun_DeviceNotManaged(t *testing.T) {
	fakeMaestro := filepath.Join(t.TempDir(), "maestro")
	writeFile(t, fakeMaestro, "#!/bin/sh\n")
	require.NoError(t, os.Chmod(fakeMaestro, 0755))

	devices := &fakeDevices{}
	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())
	s.devices = devices

	_, err := s.Run(Config{FlowPaths: []string{".maestro"}}, Installation{BinaryPath: fakeMaestro})
	require.NoError(t, err)
	assert.False(t, devices.acquired)
}

type fakeDevices struct {
	device   Device
	acquired bool
	released bool
}

func (f *fakeDevices) Acquire(Config) (Device, error) {
	f.acquired = true
	device := f.device
	device.Release = func() { f.released = true }
	return device, nil
}

package step

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunningDevices_UsesTheSDKAdb(t *testing.T) {
	androidHome := t.TempDir()
	writeExecutable(t, filepath.Join(androidHome, "platform-tools", "adb"), "printf 'List of devices attached\\nemulator-5554\\tdevice\\n'\n")
	fakeOnPath(t, "adb", "printf 'List of devices attached\\n'\n")

	running, err := testAndroidDevices(androidHome).runningDevices()
	require.NoError(t, err)
	assert.Equal(t, []string{"emulator-5554"}, running)
}

func TestInstallAppCommand_UsesTheSDKAdb(t *testing.T) {
	androidHome := t.TempDir()
	sdkAdb := filepath.Join(androidHome, "platform-tools", "adb")
	writeExecutable(t, sdkAdb, "")

	name, _ := installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, "emulator-5554", androidHome)
	assert.Equal(t, sdkAdb, name)

	name, _ = installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid}, "emulator-5554", t.TempDir())
	assert.Equal(t, "adb", name, "adb on PATH when the SDK has none")
}

func TestAcquire_RunningEmulatorWithoutSDK(t *testing.T) {
	fakeOnPath(t, "adb", "printf 'List of devices attached\\nemulator-5554\\tdevice\\n'\n")

	device, err := testAndroidDevices("").acquire()
	require.NoError(t, err)
	assert.Equal(t, "emulator-5554", device.ID)
	assert.Nil(t, device.Release, "a device the step found is never shut down")
}

func TestAcquire_BootingWithoutSDK(t *testing.T) {
	fakeOnPath(t, "adb", "printf 'List of devices attached\\n'\n")

	_, err := testAndroidDevices("").acquire()
	require.ErrorContains(t, err, "ANDROID_HOME")
}

func testAndroidDevices(androidHome string) androidDevices {
	return newAndroidDevices(log.NewLogger(), command.NewFactory(env.NewRepository()), androidHome, "", "")
}

func writeExecutable(t *testing.T, pth, script string) {
	writeFile(t, pth, "#!/bin/sh\n"+script)
	require.NoError(t, os.Chmod(pth, 0755))
}

// fakeOnPath puts a fake binary first on PATH for the rest of the test.
func fakeOnPath(t *testing.T, name, script string) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, name), script)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

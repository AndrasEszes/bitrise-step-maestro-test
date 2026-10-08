package step

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitUntilBooted(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	devices := iosDevicesWithFakeXcrun(t, "echo \"$@\" > \""+argsFile+"\"\n")

	require.NoError(t, devices.waitUntilBooted("UDID-1", time.Minute))

	args, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Equal(t, "simctl bootstatus UDID-1\n", string(args), "nothing is launched on a simulator the step found running")
}

func TestWaitUntilBooted_Fails(t *testing.T) {
	devices := iosDevicesWithFakeXcrun(t, "exit 1\n")

	require.ErrorContains(t, devices.waitUntilBooted("UDID-1", time.Minute), "wait for simulator UDID-1")
}

func TestWaitUntilBooted_TimesOut(t *testing.T) {
	devices := iosDevicesWithFakeXcrun(t, "sleep 10\n")

	start := time.Now()
	require.ErrorContains(t, devices.waitUntilBooted("UDID-1", 100*time.Millisecond), "did not finish booting")
	assert.Less(t, time.Since(start), 5*time.Second)
}

func iosDevicesWithFakeXcrun(t *testing.T, script string) iosDevices {
	binDir := t.TempDir()
	writeFile(t, filepath.Join(binDir, "xcrun"), "#!/bin/sh\n"+script)
	require.NoError(t, os.Chmod(filepath.Join(binDir, "xcrun"), 0755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return iosDevices{logger: log.NewLogger(), commandFactory: command.NewFactory(env.NewRepository())}
}

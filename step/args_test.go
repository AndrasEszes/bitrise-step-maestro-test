package step

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseApp(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    App
		wantErr bool
	}{
		{name: "empty", path: "", want: App{}},
		{name: "apk", path: "build/app-debug.apk", want: App{Path: "build/app-debug.apk", Platform: PlatformAndroid}},
		{name: "app with trailing slash", path: "Build/Wikipedia.app/", want: App{Path: "Build/Wikipedia.app/", Platform: PlatformIOS}},
		{name: "ipa is rejected", path: "Wikipedia.ipa", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseApp(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitList(t *testing.T) {
	assert.Equal(t, []string{"smoke", "android", "login"}, splitList(" smoke, android\nlogin,,"))
	assert.Nil(t, splitList(""))
}

func TestTestArgs(t *testing.T) {
	config := Config{
		FlowPath:       ".maestro",
		IncludeTags:    []string{"smoke", "android"},
		ExcludeTags:    []string{"flaky"},
		AdditionalArgs: []string{"-e", "USERNAME=bitrise"},
	}
	result := Result{JUnitPath: "/tmp/report.xml", TestOutputDir: "/tmp/out"}

	assert.Equal(t, []string{
		"test", "--format", "junit", "--output", "/tmp/report.xml", "--test-output-dir", "/tmp/out",
		"--include-tags", "smoke,android",
		"--exclude-tags", "flaky",
		"-e", "USERNAME=bitrise",
		".maestro",
	}, testArgs(config, result))
}

func TestTestArgs_Minimal(t *testing.T) {
	result := Result{JUnitPath: "/tmp/report.xml", TestOutputDir: "/tmp/out"}
	assert.Equal(t, []string{
		"test", "--format", "junit", "--output", "/tmp/report.xml", "--test-output-dir", "/tmp/out", "flows/login.yaml",
	}, testArgs(Config{FlowPath: "flows/login.yaml"}, result))
}

func TestInstallAppCommand(t *testing.T) {
	name, args := installAppCommand(App{Path: "app.apk", Platform: PlatformAndroid})
	assert.Equal(t, "adb", name)
	assert.Equal(t, []string{"install", "-r", "app.apk"}, args)

	name, args = installAppCommand(App{Path: "My.app", Platform: PlatformIOS})
	assert.Equal(t, "xcrun", name)
	assert.Equal(t, []string{"simctl", "install", "booted", "My.app"}, args)
}

func TestHasAndroidDevice(t *testing.T) {
	assert.True(t, hasAndroidDevice("List of devices attached\nemulator-5554\tdevice\n"))
	assert.False(t, hasAndroidDevice("List of devices attached\nemulator-5556\toffline\n"))
	assert.False(t, hasAndroidDevice("List of devices attached\n"))
}

func TestHasBootedSimulator(t *testing.T) {
	assert.True(t, hasBootedSimulator("== Devices ==\n-- iOS 26.4 --\n    iPhone 17 (0D6E2C4B-6F3A-4E8B-9C1D-2A3B4C5D6E7F) (Booted) \n"))
	assert.False(t, hasBootedSimulator("== Devices ==\n"))
}

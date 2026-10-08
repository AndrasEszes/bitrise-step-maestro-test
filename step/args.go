package step

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kballard/go-shellquote"
)

type Platform string

const (
	PlatformUnknown Platform = ""
	PlatformAndroid Platform = "android"
	PlatformIOS     Platform = "ios"
)

type App struct {
	Path     string
	Platform Platform
}

func parseApp(pth string) (App, error) {
	pth = strings.TrimSpace(pth)
	if pth == "" {
		return App{}, nil
	}

	switch strings.ToLower(filepath.Ext(strings.TrimSuffix(pth, "/"))) {
	case ".apk":
		return App{Path: pth, Platform: PlatformAndroid}, nil
	case ".app":
		return App{Path: pth, Platform: PlatformIOS}, nil
	default:
		return App{}, fmt.Errorf("app_path must point to an .apk (Android) or a simulator .app (iOS), got: %s", pth)
	}
}

// resolveTarget decides the platform first, then the app for it: when app_path yields both an .apk and an .app, the one
// for that platform is installed. A platform passed to maestro test wins, because Maestro runs the flows only on that
// platform. Then an app of a single platform decides, then the host: Bitrise runs Android emulators on Linux and iOS
// simulators on macOS.
func resolveTarget(appInput string, additionalArgs []string, goos string) (App, Platform, error) {
	var apps []App
	for _, pth := range splitLines(appInput) {
		app, err := parseApp(pth)
		if err != nil {
			return App{}, PlatformUnknown, err
		}
		apps = append(apps, app)
	}

	platform, _ := platformArg(additionalArgs)
	if platform == PlatformUnknown && len(apps) == 1 {
		platform = apps[0].Platform
	}
	if platform == PlatformUnknown {
		platform = hostPlatform(goos)
	}

	for _, app := range apps {
		if app.Platform == platform {
			return app, platform, nil
		}
	}
	if len(apps) > 0 {
		return apps[0], platform, nil
	}
	return App{}, platform, nil
}

// platformArg returns the platform passed to maestro test, and whether one was passed at all: a platform the step has
// no device for, such as web, comes back as PlatformUnknown.
func platformArg(args []string) (Platform, bool) {
	for i, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		if name != "-p" && name != "--platform" {
			continue
		}
		if !hasValue && i+1 < len(args) {
			value = args[i+1]
		}
		switch strings.ToLower(value) {
		case "android":
			return PlatformAndroid, true
		case "ios":
			return PlatformIOS, true
		default:
			return PlatformUnknown, true
		}
	}
	return PlatformUnknown, false
}

// managesDevice tells whether the step prepares the device: not when the device is picked in the arguments, or when
// Maestro is asked to run on a platform the step has no device for.
func managesDevice(additionalArgs []string) bool {
	if hasDeviceArg(additionalArgs) {
		return false
	}
	platform, passed := platformArg(additionalArgs)
	return !passed || platform != PlatformUnknown
}

func hostPlatform(goos string) Platform {
	if goos == "darwin" {
		return PlatformIOS
	}
	return PlatformAndroid
}

func splitList(input string) []string {
	var items []string
	for _, item := range strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == '\n' }) {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func splitLines(input string) []string {
	var lines []string
	for _, line := range strings.Split(input, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitArgs(input string) ([]string, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}
	return shellquote.Split(input)
}

func testArgs(config Config, result Result, deviceID string) []string {
	args := []string{"test", "--format", "junit", "--output", result.JUnitPath, "--test-output-dir", result.TestOutputDir}
	if deviceID != "" {
		args = append(args, "--device", deviceID)
	}
	if len(config.IncludeTags) > 0 {
		args = append(args, "--include-tags", strings.Join(config.IncludeTags, ","))
	}
	if len(config.ExcludeTags) > 0 {
		args = append(args, "--exclude-tags", strings.Join(config.ExcludeTags, ","))
	}
	args = append(args, config.AdditionalArgs...)
	return append(args, config.FlowPaths...)
}

func installAppCommand(app App, deviceID, androidHome string) (string, []string) {
	if app.Platform == PlatformAndroid {
		var args []string
		if deviceID != "" {
			args = append(args, "-s", deviceID)
		}
		return adbPath(androidHome), append(args, "install", "-r", app.Path)
	}

	target := "booted"
	if deviceID != "" {
		target = deviceID
	}
	return "xcrun", []string{"simctl", "install", target, app.Path}
}

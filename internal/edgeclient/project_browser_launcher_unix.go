//go:build !windows

package edgeclient

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

const projectBrowserChromium = "/usr/lib/chromium/chromium"

var browserChromeFlagName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func RunProjectBrowserLauncher(profile string, chromeArgs []string) error {
	args, err := projectBrowserBubblewrapArgs(profile, chromeArgs)
	if err != nil {
		return err
	}
	return syscall.Exec("/usr/bin/bwrap", append([]string{"bwrap"}, args...), []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"})
}

func projectBrowserBubblewrapArgs(profile string, chromeArgs []string) ([]string, error) {
	cleanProfile := filepath.Clean(profile)
	if profile == "" || !filepath.IsAbs(profile) || cleanProfile != profile {
		return nil, errors.New("project browser profile is invalid")
	}
	info, err := os.Lstat(profile)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return nil, errors.New("project browser profile is unsafe")
	}
	validated, err := validateBrowserChromeArgs(profile, chromeArgs)
	if err != nil {
		return nil, err
	}
	args := []string{"--die-with-parent", "--new-session", "--unshare-all", "--share-net", "--clearenv"}
	for _, path := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/ssl/certs", "/etc/ca-certificates", "/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/etc/passwd", "/etc/group", "/etc/services", "/etc/protocols"} {
		if _, err := os.Stat(path); err == nil {
			args = append(args, "--ro-bind", path, path)
		}
	}
	args = append(args,
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--bind", profile, "/browser-profile", "--chdir", "/browser-profile",
		"--setenv", "HOME", "/browser-profile", "--setenv", "XDG_CONFIG_HOME", "/browser-profile/.config",
		"--setenv", "XDG_CACHE_HOME", "/browser-profile/.cache", "--setenv", "USER", "mcpedge",
		"--setenv", "LANG", "C.UTF-8", "--setenv", "LC_ALL", "C.UTF-8", "--setenv", "TMPDIR", "/tmp",
		"--", projectBrowserChromium,
	)
	return append(args, validated...), nil
}

func validateBrowserChromeArgs(profile string, input []string) ([]string, error) {
	allowed := map[string]bool{
		"no-first-run": true, "no-default-browser-check": true, "headless": true, "disable-background-networking": true,
		"enable-features": true, "disable-background-timer-throttling": true, "disable-backgrounding-occluded-windows": true,
		"disable-breakpad": true, "disable-client-side-phishing-detection": true, "disable-default-apps": true, "disable-dev-shm-usage": true,
		"disable-extensions": true, "disable-features": true, "disable-hang-monitor": true, "disable-ipc-flooding-protection": true,
		"disable-popup-blocking": true, "disable-prompt-on-repost": true, "disable-renderer-backgrounding": true, "disable-sync": true,
		"force-color-profile": true, "metrics-recording-only": true, "safebrowsing-disable-auto-update": true, "enable-automation": true,
		"password-store": true, "use-mock-keychain": true, "remote-debugging-port": true, "user-data-dir": true,
		"window-size": true, "no-sandbox": true, "disable-gpu": true, "hide-scrollbars": true, "mute-audio": true,
		"ignore-certificate-errors": true,
	}
	output := make([]string, 0, len(input))
	seenProfile, seenDebug := false, false
	for _, argument := range input {
		if argument == "about:blank" {
			output = append(output, argument)
			continue
		}
		if !strings.HasPrefix(argument, "--") || strings.ContainsRune(argument, 0) {
			return nil, errors.New("project browser Chrome arguments are invalid")
		}
		nameValue := strings.TrimPrefix(argument, "--")
		name, value, has := strings.Cut(nameValue, "=")
		if !browserChromeFlagName.MatchString(name) || !allowed[name] || len(value) > 4096 {
			return nil, errors.New("project browser Chrome flag is not allowed")
		}
		switch name {
		case "user-data-dir":
			if !has || value != profile || seenProfile {
				return nil, errors.New("project browser profile flag is invalid")
			}
			seenProfile = true
			argument = "--user-data-dir=/browser-profile"
		case "remote-debugging-port":
			if !has || value != "0" || seenDebug {
				return nil, errors.New("project browser debugging flag is invalid")
			}
			seenDebug = true
		case "window-size":
			parts := strings.Split(value, ",")
			if !has || len(parts) != 2 {
				return nil, errors.New("project browser viewport flag is invalid")
			}
			w, e1 := strconv.Atoi(parts[0])
			h, e2 := strconv.Atoi(parts[1])
			if e1 != nil || e2 != nil || w < 320 || w > 1920 || h < 240 || h > 1080 {
				return nil, errors.New("project browser viewport flag is invalid")
			}
		}
		output = append(output, argument)
	}
	if !seenProfile || !seenDebug {
		return nil, errors.New("project browser required Chrome flags are missing")
	}
	return output, nil
}

package edgeclient

import (
	"errors"
	"slices"
	"strings"
)

var devGitRemoteConfigArguments = []string{
	"config", "--local", "--no-includes", "--get-regexp", `^remote\.origin\.(url|pushurl)$`,
}

func devGitFastForwardArguments(args []string) bool {
	return len(args) == 3 && args[0] == "merge" && args[1] == "--ff-only" && devGitCommitPattern.MatchString(args[2])
}

func devGitReadsLocalConfig(args []string) bool {
	return slices.Equal(args, devGitRemoteConfigArguments)
}

func devGitNetworkCommand(args []string, owner string) (string, bool, error) {
	if len(args) == 0 {
		return "", false, errors.New("development Git request is empty")
	}
	var remoteURL string
	switch args[0] {
	case "clone":
		if len(args) < 4 {
			return "", false, errors.New("development Git clone request is invalid")
		}
		separator := -1
		for index, arg := range args {
			if arg == "--" {
				separator = index
				break
			}
		}
		if separator < 1 || separator+3 != len(args) {
			return "", false, errors.New("development Git clone request is invalid")
		}
		remoteURL = args[separator+1]
	case "fetch":
		if len(args) != 4 || args[1] != "--no-tags" {
			return "", false, errors.New("development Git fetch request is invalid")
		}
		remoteURL = args[2]
	case "push":
		if len(args) != 4 || args[1] != "--porcelain" {
			return "", false, errors.New("development Git push request is invalid")
		}
		remoteURL = args[2]
	case "ls-remote":
		if len(args) != 4 || args[1] != "--heads" {
			return "", false, errors.New("development Git remote inspection request is invalid")
		}
		remoteURL = args[2]
	case "pull", "submodule":
		return "", false, errors.New("development Git network command is not supported")
	default:
		if strings.HasPrefix(args[0], "-") {
			return "", false, errors.New("development Git command options must follow the command")
		}
		return "", false, nil
	}
	prefix := "https://github.com/" + owner + "/"
	if !githubOwnerPattern.MatchString(owner) || !strings.HasPrefix(remoteURL, prefix) || !strings.HasSuffix(remoteURL, ".git") ||
		!devGitSimplePattern.MatchString(strings.TrimSuffix(strings.TrimPrefix(remoteURL, prefix), ".git")) {
		return "", false, errors.New("development Git network target is not owner-bound HTTPS")
	}
	return remoteURL, true, nil
}

func devGitProtectedArguments(args []string, remoteURL string) []string {
	return devGitProtectedArgumentsForOS(args, remoteURL, "/dev/null")
}

func devGitProtectedArgumentsForOS(args []string, remoteURL, nullPath string) []string {
	protected := []string{
		"-c", "credential.helper=",
		"-c", "core.hooksPath=" + nullPath,
		"-c", "core.fsmonitor=false",
		"-c", "protocol.file.allow=never",
		"-c", "protocol.ext.allow=never",
	}
	if remoteURL != "" {
		protected = append(protected,
			"-c", "credential."+remoteURL+".helper=",
			"-c", "credential.useHttpPath=true",
			"-c", "http.proxy=",
			"-c", "http.extraHeader=",
			"-c", "http.cookieFile=",
			"-c", "http.saveCookies=false",
			"-c", "http.sslVerify=true",
			"-c", "http.followRedirects=false",
			"-c", "http."+remoteURL+".proxy=",
			"-c", "http."+remoteURL+".extraHeader=",
			"-c", "http."+remoteURL+".cookieFile=",
			"-c", "http."+remoteURL+".saveCookies=false",
			"-c", "http."+remoteURL+".sslVerify=true",
			"-c", "http."+remoteURL+".followRedirects=false",
			"-c", "url."+remoteURL+".insteadOf="+remoteURL,
			"-c", "url."+remoteURL+".pushInsteadOf="+remoteURL,
		)
	}
	return append(protected, args...)
}

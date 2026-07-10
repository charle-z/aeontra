package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const privilegedPlanTTL = 2 * time.Minute

type PrivilegedConfig struct {
	Enabled         bool
	AllowedServices []string
	Timeout         time.Duration
	services        map[string]bool
}

type privilegedProfile struct {
	Argv       []string
	Dir        string
	Network    string
	Filesystem string
	Effect     string
	Risk       string
	Runner     string
}

func normalizePrivilegedConfig(cfg PrivilegedConfig) PrivilegedConfig {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Minute
	}
	cfg.services = map[string]bool{}
	for _, service := range cfg.AllowedServices {
		service = strings.TrimSpace(service)
		if validServiceName(service) {
			cfg.services[service] = true
		}
	}
	return cfg
}

func (s *Service) PrivilegedTaskPreview(repo, profile string, params map[string]string) (string, error) {
	sp := s.log.Start("privileged_task_preview")
	profile = strings.TrimSpace(profile)
	if !s.privileged.Enabled {
		err := fmt.Errorf("privileged task profiles are disabled by administrator configuration")
		sp.Finish(audit.Deny, profile, nil, err)
		return "", err
	}
	definition, err := s.buildPrivilegedProfile(repo, profile, params)
	if err != nil {
		sp.Finish(audit.Deny, profile, nil, err)
		return "", err
	}
	argvJSON, _ := json.Marshal(definition.Argv)
	plan, err := s.plans.CreateTTL("privileged-task", map[string]string{
		"profile": profile, "argv": string(argvJSON), "dir": definition.Dir,
		"network": definition.Network, "filesystem": definition.Filesystem,
		"effect": definition.Effect, "risk": definition.Risk, "runner": definition.Runner,
	}, privilegedPlanTTL)
	if err != nil {
		sp.Finish(audit.Error, profile, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, profile+" "+plan.ID, []string{definition.Dir}, nil)
	return fmt.Sprintf("Command:\n%s\n\nWorking directory:\n%s\n\nNetwork:\n%s\n\nFilesystem access:\n%s\n\nExpected effect:\n%s\n\nRisk:\n%s\n\nPlan ID:\n%s\n\nExpiry:\n%s\n",
		strings.Join(definition.Argv, " "), definition.Dir, definition.Network, definition.Filesystem,
		definition.Effect, definition.Risk, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *Service) PrivilegedTaskExecute(planID string, approve bool) (string, error) {
	sp := s.log.Start("privileged_task_execute")
	if !s.privileged.Enabled {
		err := fmt.Errorf("privileged task profiles are disabled by administrator configuration")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: privileged_task_execute would run the reviewed server-generated profile. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "privileged-task")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	var argv []string
	if err := json.Unmarshal([]byte(plan.Args["argv"]), &argv); err != nil || len(argv) == 0 {
		err := fmt.Errorf("invalid server-generated privileged plan")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	dir, err := s.workdir(plan.Args["dir"])
	if err != nil || dir != plan.Args["dir"] {
		if err == nil {
			err = fmt.Errorf("privileged working directory changed after preview")
		}
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if strings.HasPrefix(plan.Args["profile"], "docker-") {
		err := fmt.Errorf("secure failure: Docker socket access is not exposed through the public MCP; run this profile only in a separately contained administrator runner")
		sp.Finish(audit.Deny, planID, []string{dir}, err)
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.privileged.Timeout)
	defer cancel()
	var out string
	if plan.Args["runner"] == "sandbox-preferred" && s.sandbox.Status(ctx).Available {
		result, runErr := s.sandbox.Run(ctx, SandboxRunRequest{Dir: dir, Argv: argv, NetworkProfile: "none", Timeout: s.privileged.Timeout})
		out = strings.TrimRight(result.Stdout+"\n"+result.Stderr, "\n")
		err = runErr
	} else {
		out, err = s.run(ctx, dir, argv[0], argv[1:])
	}
	if err != nil {
		sp.Finish(audit.Error, planID, []string{dir}, err)
		return s.redact(out), fmt.Errorf("privileged profile %s failed: %w", plan.Args["profile"], err)
	}
	sp.Finish(audit.Allow, planID, []string{dir}, nil)
	return s.redact(out), nil
}

func (s *Service) buildPrivilegedProfile(repo, profile string, params map[string]string) (privilegedProfile, error) {
	params = clonePlanArgs(params)
	requireDir := profile != "inspect-approved-service-status" && profile != "restart-approved-service"
	dir := s.root
	var err error
	if requireDir {
		dir, err = s.workdir(repo)
		if err != nil {
			return privilegedProfile{}, err
		}
	} else if strings.TrimSpace(repo) != "" {
		return privilegedProfile{}, fmt.Errorf("repo is not accepted by service profiles")
	}
	p := privilegedProfile{Dir: dir, Filesystem: dir, Network: "disabled (profile does not request network; sandbox enforcement is used when available)", Runner: "host-fixed"}
	switch profile {
	case "git-fetch":
		if err := onlyProfileParams(params, "remote"); err != nil {
			return p, err
		}
		remote := defaultGitName(params["remote"], "origin")
		if !safeGitName(remote) || strings.Contains(remote, "/") {
			return p, fmt.Errorf("invalid git remote")
		}
		p.Argv = []string{"git", "fetch", remote}
		p.Network = "enabled (required only to contact the configured Git remote)"
		p.Effect = "Update remote-tracking refs from one named remote."
		p.Risk = "Network access and local Git metadata changes; no refspec or extra arguments."
	case "git-fast-forward":
		if err := onlyProfileParams(params, "remote", "branch"); err != nil {
			return p, err
		}
		remote := defaultGitName(params["remote"], "origin")
		branch := defaultGitName(params["branch"], "main")
		if !safeGitName(remote) || strings.Contains(remote, "/") || !safeGitName(branch) {
			return p, fmt.Errorf("invalid remote or branch")
		}
		p.Argv = []string{"git", "merge", "--ff-only", remote + "/" + branch}
		p.Effect = "Fast-forward the current branch to one remote-tracking branch."
		p.Risk = "Changes the checked-out branch; ff-only rejects divergence."
	case "go-test", "go-vet", "go-build", "gofmt-check":
		if err := onlyProfileParams(params); err != nil {
			return p, err
		}
		commands := map[string][]string{
			"go-test": {"go", "test", "./...", "-count=1"}, "go-vet": {"go", "vet", "./..."},
			"go-build": {"go", "build", "./..."}, "gofmt-check": {"gofmt", "-l", "."},
		}
		p.Argv = commands[profile]
		p.Runner = "sandbox-preferred"
		p.Effect = "Run the fixed " + profile + " verification profile."
		p.Risk = "Build or test code may consume CPU, memory, and write tool caches; timeout applies."
	case "docker-build-project", "docker-compose-config":
		if err := onlyProfileParams(params); err != nil {
			return p, err
		}
		if profile == "docker-build-project" {
			p.Argv = []string{"docker", "build", "--network", "none", "-t", "mcp-devbox-profile-build", "."}
			p.Effect = "Build the project Dockerfile with build networking disabled."
		} else {
			p.Argv = []string{"docker", "compose", "config"}
			p.Effect = "Validate and render the project Docker Compose configuration."
		}
		p.Risk = "Preview only in the public MCP architecture; execution fails securely without a separate administrator runner and never exposes the Docker socket."
	case "inspect-approved-service-status", "restart-approved-service":
		if err := onlyProfileParams(params, "service"); err != nil {
			return p, err
		}
		service := strings.TrimSpace(params["service"])
		if !validServiceName(service) || !s.privileged.services[service] {
			return p, fmt.Errorf("service %q is not in the administrator allowlist", service)
		}
		action := "status"
		p.Effect = "Inspect one administrator-approved service."
		p.Risk = "Read-only host service inspection."
		if profile == "restart-approved-service" {
			action = "restart"
			p.Effect = "Restart one administrator-approved service."
			p.Risk = "Brief service interruption and host state change."
		}
		p.Argv = []string{"systemctl", action, "--", service}
		p.Filesystem = "none beyond system service manager access"
	default:
		return p, fmt.Errorf("unknown privileged profile %q", profile)
	}
	return p, nil
}

func onlyProfileParams(params map[string]string, allowed ...string) error {
	allow := map[string]bool{}
	for _, key := range allowed {
		allow[key] = true
	}
	var unknown []string
	for key := range params {
		if !allow[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("parameters not accepted by this profile: %s", strings.Join(unknown, ", "))
	}
	return nil
}

func validServiceName(service string) bool {
	return service != "" && !strings.HasPrefix(service, "-") && !strings.Contains(service, "..") &&
		!strings.ContainsAny(service, "/\\;|&`$<>\r\n\t ") && gitSafeDirRe.MatchString(service)
}

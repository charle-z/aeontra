// Package workflowpolicy validates GitHub Actions workflow files against the
// repository's least-privilege CI rules before GitHub executes them.
package workflowpolicy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

var (
	ErrInvalidWorkflow     = errors.New("workflow policy: invalid workflow")
	ErrForbiddenTrigger    = errors.New("workflow policy: forbidden trigger")
	ErrForbiddenPermission = errors.New("workflow policy: forbidden permission")
	ErrMissingTimeout      = errors.New("workflow policy: missing job timeout")
	ErrInvalidTimeout      = errors.New("workflow policy: invalid job timeout")
	ErrForbiddenPRContent  = errors.New("workflow policy: forbidden pull-request content")
	ErrMutableVersion      = errors.New("workflow policy: mutable action or tool version")
)

const maxJobTimeoutMinutes = 90

var allowedPermissions = map[string]map[string]bool{
	"actions":         {"read": true},
	"checks":          {"read": true},
	"contents":        {"read": true},
	"packages":        {"read": true},
	"pull-requests":   {"read": true},
	"security-events": {"read": true, "write": true},
	"statuses":        {"read": true},
}

var forbiddenRunFragments = []string{
	"docker login",
	"kubectl ",
	"platform_deploy",
	"coolify",
	"mcp-devbox-charlez.duckdns.org",
	"zap-full-scan",
	"zap-api-scan",
	"production deploy",
	"deploy production",
}

// Validate checks one workflow document. It does not execute any workflow command.
func Validate(name string, content []byte) error {
	if len(strings.TrimSpace(string(content))) == 0 {
		return fmt.Errorf("%w: %s is empty", ErrInvalidWorkflow, name)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidWorkflow, name, err)
	}
	root, err := documentMapping(&document)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidWorkflow, name, err)
	}

	onNode := mappingValue(root, "on")
	if onNode == nil {
		return fmt.Errorf("%w: %s has no on trigger", ErrInvalidWorkflow, name)
	}
	if eventEnabled(onNode, "pull_request_target") {
		return fmt.Errorf("%w: %s uses pull_request_target", ErrForbiddenTrigger, name)
	}
	pullRequestFacing := eventEnabled(onNode, "pull_request")

	permissions := mappingValue(root, "permissions")
	if permissions == nil {
		return fmt.Errorf("%w: %s must declare root permissions", ErrForbiddenPermission, name)
	}
	if err := validatePermissions(permissions, name+" root"); err != nil {
		return err
	}

	jobs := mappingValue(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) == 0 {
		return fmt.Errorf("%w: %s has no jobs mapping", ErrInvalidWorkflow, name)
	}
	for i := 0; i < len(jobs.Content); i += 2 {
		jobName := jobs.Content[i].Value
		job := jobs.Content[i+1]
		if job.Kind != yaml.MappingNode {
			return fmt.Errorf("%w: %s job %s is not a mapping", ErrInvalidWorkflow, name, jobName)
		}
		if err := validateJob(name, jobName, job, pullRequestFacing); err != nil {
			return err
		}
	}

	if pullRequestFacing && strings.Contains(strings.ToLower(string(content)), "${{ secrets.") {
		return fmt.Errorf("%w: %s references repository secrets from a pull-request workflow", ErrForbiddenPRContent, name)
	}
	return nil
}

func documentMapping(document *yaml.Node) (*yaml.Node, error) {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, errors.New("expected one YAML document")
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("root must be a mapping")
	}
	return root, nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func eventEnabled(node *yaml.Node, event string) bool {
	switch node.Kind {
	case yaml.ScalarNode:
		return node.Value == event
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if child.Value == event {
				return true
			}
		}
	case yaml.MappingNode:
		return mappingValue(node, event) != nil
	}
	return false
}

func validatePermissions(node *yaml.Node, scope string) error {
	if node.Kind == yaml.ScalarNode {
		return fmt.Errorf("%w: %s uses %q", ErrForbiddenPermission, scope, node.Value)
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%w: %s permissions must be a mapping", ErrForbiddenPermission, scope)
	}
	for i := 0; i < len(node.Content); i += 2 {
		permission := strings.ToLower(strings.TrimSpace(node.Content[i].Value))
		value := strings.ToLower(strings.TrimSpace(node.Content[i+1].Value))
		allowedValues, ok := allowedPermissions[permission]
		if !ok || !allowedValues[value] {
			return fmt.Errorf("%w: %s %s=%s", ErrForbiddenPermission, scope, permission, value)
		}
	}
	return nil
}

func validateJob(workflowName, jobName string, job *yaml.Node, pullRequestFacing bool) error {
	timeout := mappingValue(job, "timeout-minutes")
	if timeout == nil {
		return fmt.Errorf("%w: %s job %s", ErrMissingTimeout, workflowName, jobName)
	}
	minutes, err := strconv.Atoi(strings.TrimSpace(timeout.Value))
	if err != nil || minutes < 1 || minutes > maxJobTimeoutMinutes {
		return fmt.Errorf("%w: %s job %s timeout %q", ErrInvalidTimeout, workflowName, jobName, timeout.Value)
	}
	if permissions := mappingValue(job, "permissions"); permissions != nil {
		if err := validatePermissions(permissions, workflowName+" job "+jobName); err != nil {
			return err
		}
	}
	return walkJob(job, pullRequestFacing, workflowName+" job "+jobName)
}

func walkJob(node *yaml.Node, pullRequestFacing bool, scope string) error {
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i].Value
			value := node.Content[i+1]
			switch key {
			case "uses":
				if err := validateActionVersion(value.Value, scope); err != nil {
					return err
				}
			case "run":
				if err := validateRun(value.Value, pullRequestFacing, scope); err != nil {
					return err
				}
			}
			if err := walkJob(value, pullRequestFacing, scope); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range node.Content {
		if err := walkJob(child, pullRequestFacing, scope); err != nil {
			return err
		}
	}
	return nil
}

func validateActionVersion(value, scope string) error {
	separator := strings.LastIndex(value, "@")
	if separator <= 0 || separator == len(value)-1 {
		return fmt.Errorf("%w: %s action %q has no explicit ref", ErrMutableVersion, scope, value)
	}
	ref := strings.ToLower(strings.TrimSpace(value[separator+1:]))
	if ref == "main" || ref == "master" || ref == "latest" {
		return fmt.Errorf("%w: %s action %q", ErrMutableVersion, scope, value)
	}
	return nil
}

func validateRun(value string, pullRequestFacing bool, scope string) error {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "@latest") {
		return fmt.Errorf("%w: %s run command uses @latest", ErrMutableVersion, scope)
	}
	if !pullRequestFacing {
		return nil
	}
	for _, fragment := range forbiddenRunFragments {
		if strings.Contains(lower, fragment) {
			return fmt.Errorf("%w: %s run command contains %q", ErrForbiddenPRContent, scope, fragment)
		}
	}
	return nil
}

//go:build windows

package edgeclient

import (
	"context"
	"errors"
)

type unsupportedProjectCheckoutInspector struct{}

func newProjectCheckoutInspector() ProjectCheckoutInspector {
	return unsupportedProjectCheckoutInspector{}
}

func (unsupportedProjectCheckoutInspector) Inspect(context.Context, string, string, string) (ProjectCheckoutState, error) {
	return ProjectCheckoutUnsafe, errors.New("project checkout inspection requires Linux")
}

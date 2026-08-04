//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	cdpinput "github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

type chromiumProjectBrowserRunner struct{}

func NewProjectBrowserRunner() BrowserRunner { return chromiumProjectBrowserRunner{} }

func (chromiumProjectBrowserRunner) Run(parent context.Context, request BrowserPageRequest) (BrowserPageResult, error) {
	if request.ProfilePath == "" || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 120 {
		return BrowserPageResult{}, errors.New("project browser runner request is invalid")
	}
	executable, err := os.Executable()
	if err != nil {
		return BrowserPageResult{}, errors.New("project browser launcher unavailable")
	}
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.ExecPath(executable), chromedp.UserDataDir(request.ProfilePath), chromedp.WindowSize(request.ViewportWidth, request.ViewportHeight), chromedp.Flag("no-sandbox", true), chromedp.Flag("disable-gpu", true), chromedp.Flag("hide-scrollbars", true), chromedp.Flag("mute-audio", true))
	if request.IgnoreHTTPSErrors {
		opts = append(opts, chromedp.IgnoreCertErrors)
	}
	opts = append(opts, chromedp.ModifyCmdFunc(func(command *exec.Cmd) {
		command.Args = append([]string{command.Args[0], "browser-launcher", "--profile", request.ProfilePath, "--"}, command.Args[1:]...)
	}))
	timeoutCtx, cancelTimeout := context.WithTimeout(parent, time.Duration(request.TimeoutSeconds)*time.Second)
	defer cancelTimeout()
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(timeoutCtx, opts...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorAllow).WithDownloadPath("/browser-profile/downloads").WithEventsEnabled(true).Do(ctx)
	})); err != nil {
		return BrowserPageResult{}, errors.New("project browser startup failed")
	}
	if len(request.Cookies) > 0 {
		if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			browser := chromedp.FromContext(ctx).Browser
			if browser == nil {
				return errors.New("browser executor unavailable")
			}
			return storage.SetCookies(request.Cookies).Do(cdp.WithExecutor(ctx, browser))
		})); err != nil {
			return BrowserPageResult{}, errors.New("project browser cookie restore failed")
		}
	}
	if request.CurrentURL != "" && (len(request.Steps) == 0 || request.Steps[0].Action != "navigate") {
		if err := ValidateBrowserURL(browserCtx, request.NetworkScope, request.InitialOrigin, request.CurrentURL, nil); err != nil {
			return BrowserPageResult{}, err
		}
		if err := chromedp.Run(browserCtx, chromedp.Navigate(request.CurrentURL)); err != nil {
			return BrowserPageResult{}, errors.New("project browser initial navigation failed")
		}
	}
	for _, step := range request.Steps {
		if err := runProjectBrowserStep(browserCtx, request, step); err != nil {
			return BrowserPageResult{}, err
		}
	}
	var result BrowserPageResult
	actions := []chromedp.Action{chromedp.Location(&result.URL), chromedp.Title(&result.Title)}
	if request.Capture == "text" || request.Capture == "both" {
		actions = append(actions, chromedp.Text("body", &result.Text, chromedp.ByQuery))
	}
	if err := chromedp.Run(browserCtx, actions...); err != nil {
		return BrowserPageResult{}, errors.New("project browser result capture failed")
	}
	if request.Capture == "screenshot" || request.Capture == "both" {
		if request.FullPage {
			if err := chromedp.Run(browserCtx, chromedp.FullScreenshot(&result.Screenshot, 60)); err != nil {
				return BrowserPageResult{}, errors.New("project browser screenshot failed")
			}
		} else {
			if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
				body, e := page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatJpeg).WithQuality(60).WithFromSurface(true).Do(ctx)
				if e == nil {
					result.Screenshot = body
				}
				return e
			})); err != nil {
				return BrowserPageResult{}, errors.New("project browser screenshot failed")
			}
		}
	}
	browser := chromedp.FromContext(browserCtx).Browser
	if browser == nil {
		return BrowserPageResult{}, errors.New("project browser cookie capture failed")
	}
	cookies, err := storage.GetCookies().Do(cdp.WithExecutor(browserCtx, browser))
	if err != nil {
		return BrowserPageResult{}, errors.New("project browser cookie capture failed")
	}
	result.Cookies = projectBrowserCookieParams(cookies)
	return result, nil
}

func runProjectBrowserStep(ctx context.Context, request BrowserPageRequest, step BrowserStep) error {
	query := projectBrowserQueryOption(step.SelectorType)
	switch step.Action {
	case "navigate":
		if err := ValidateBrowserURL(ctx, request.NetworkScope, request.InitialOrigin, step.URL, nil); err != nil {
			return err
		}
		if err := chromedp.Run(ctx, chromedp.Navigate(step.URL)); err != nil {
			return errors.New("project browser navigation failed")
		}
	case "click":
		if err := chromedp.Run(ctx, chromedp.WaitVisible(step.Selector, query), chromedp.Click(step.Selector, query)); err != nil {
			return errors.New("project browser click failed")
		}
	case "type":
		actions := []chromedp.Action{chromedp.WaitVisible(step.Selector, query), chromedp.Focus(step.Selector, query)}
		if step.Clear {
			actions = append(actions, chromedp.KeyEvent("a", chromedp.KeyModifiers(cdpinput.ModifierCtrl)), chromedp.KeyEvent(kb.Backspace))
		}
		actions = append(actions, chromedp.SendKeys(step.Selector, step.Text, query))
		if err := chromedp.Run(ctx, actions...); err != nil {
			return fmt.Errorf("project browser typing failed: %w", err)
		}
	case "press":
		key := projectBrowserKey(step.Key)
		var action chromedp.Action
		if step.Selector != "" {
			action = chromedp.SendKeys(step.Selector, key, query)
		} else {
			action = chromedp.KeyEvent(key)
		}
		if err := chromedp.Run(ctx, action); err != nil {
			return errors.New("project browser key press failed")
		}
	case "select":
		if err := chromedp.Run(ctx, chromedp.WaitVisible(step.Selector, query), chromedp.SetValue(step.Selector, step.Value, query)); err != nil {
			return errors.New("project browser select failed")
		}
	case "wait":
		if step.Selector != "" {
			if err := chromedp.Run(ctx, chromedp.WaitVisible(step.Selector, query)); err != nil {
				return errors.New("project browser wait failed")
			}
		} else {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(step.Milliseconds) * time.Millisecond):
			}
		}
	default:
		return errors.New("project browser action is invalid")
	}
	return nil
}

func projectBrowserQueryOption(kind string) chromedp.QueryOption {
	if kind == "text" {
		return chromedp.BySearch
	}
	return chromedp.ByQuery
}
func projectBrowserKey(name string) string {
	switch name {
	case "Enter":
		return kb.Enter
	case "Tab":
		return kb.Tab
	case "Escape":
		return kb.Escape
	case "ArrowUp":
		return kb.ArrowUp
	case "ArrowDown":
		return kb.ArrowDown
	case "ArrowLeft":
		return kb.ArrowLeft
	case "ArrowRight":
		return kb.ArrowRight
	case "Home":
		return kb.Home
	case "End":
		return kb.End
	case "PageUp":
		return kb.PageUp
	case "PageDown":
		return kb.PageDown
	case "Backspace":
		return kb.Backspace
	case "Delete":
		return kb.Delete
	}
	return ""
}
func projectBrowserCookieParams(cookies []*network.Cookie) []*network.CookieParam {
	result := make([]*network.CookieParam, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		parameter := &network.CookieParam{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, SameSite: cookie.SameSite, Priority: cookie.Priority, SourceScheme: cookie.SourceScheme, SourcePort: cookie.SourcePort, PartitionKey: cookie.PartitionKey}
		if !cookie.Session && cookie.Expires > 0 {
			seconds := int64(cookie.Expires)
			nanoseconds := int64((cookie.Expires - float64(seconds)) * 1e9)
			value := cdp.TimeSinceEpoch(time.Unix(seconds, nanoseconds).UTC())
			parameter.Expires = &value
		}
		result = append(result, parameter)
	}
	return result
}

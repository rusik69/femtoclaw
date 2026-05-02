package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const browserOpTimeout = 2 * time.Minute
const maxHTMLChars = 200_000

// BrowserSession holds a headless Chromium instance for one chat turn.
type BrowserSession struct {
	browser     *rod.Browser
	page        *rod.Page
	downloadDir string
}

// NewBrowserSession launches headless Chromium (system binary if found, else rod default).
func NewBrowserSession() (*BrowserSession, error) {
	l := launcher.New().Headless(true).NoSandbox(true)
	if bin := os.Getenv("ROD_BROWSER_BIN"); bin != "" {
		l = l.Bin(bin)
	} else {
		for _, path := range []string{
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/lib/chromium/chromium",
		} {
			if _, err := os.Stat(path); err == nil {
				l = l.Bin(path)
				break
			}
		}
	}
	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("launch browser: %w", err)
	}
	b := rod.New().ControlURL(u)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("connect browser: %w", err)
	}
	return &BrowserSession{browser: b}, nil
}

// Close shuts down the browser.
func (s *BrowserSession) Close() {
	if s == nil {
		return
	}
	if s.downloadDir != "" {
		_ = os.RemoveAll(s.downloadDir)
		s.downloadDir = ""
	}
	if s.browser == nil {
		return
	}
	_ = s.browser.Close()
	s.browser = nil
	s.page = nil
}

func (s *BrowserSession) ensureDownloadDir() (string, error) {
	if s.downloadDir != "" {
		return s.downloadDir, nil
	}
	d, err := os.MkdirTemp("", "femtoclaw-download-*")
	if err != nil {
		return "", err
	}
	s.downloadDir = d
	return d, nil
}

func (s *BrowserSession) ensurePage() (*rod.Page, error) {
	if s.page != nil {
		return s.page, nil
	}
	p, err := s.browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, err
	}
	s.page = p
	s.page.Timeout(browserOpTimeout)
	return s.page, nil
}

func floatFromArg(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// Do runs a browser action. Args: action (required), url, selector, text, script, value, y, cookies.
func (s *BrowserSession) Do(args map[string]interface{}) (string, error) {
	if s == nil || s.browser == nil {
		return "", fmt.Errorf("browser session closed")
	}
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return "", fmt.Errorf("missing action")
	}

	switch action {
	case "close":
		s.Close()
		return "Browser closed.", nil
	}

	page, err := s.ensurePage()
	if err != nil {
		return "", err
	}
	page.Timeout(browserOpTimeout)

	switch action {
	case "navigate":
		url, _ := args["url"].(string)
		if url == "" {
			return "", fmt.Errorf("navigate requires url")
		}
		if err := page.Navigate(url); err != nil {
			return "", err
		}
		if err := page.WaitLoad(); err != nil {
			return "", err
		}
		return fmt.Sprintf("Navigated to %s", url), nil

	case "screenshot":
		buf, err := page.Screenshot(true, nil)
		if err != nil {
			return "", err
		}
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf), nil

	case "click":
		sel, _ := args["selector"].(string)
		if sel == "" {
			return "", fmt.Errorf("click requires selector")
		}
		el, err := page.Element(sel)
		if err != nil {
			return "", err
		}
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return "", err
		}
		return "Clicked " + sel, nil

	case "type":
		sel, _ := args["selector"].(string)
		txt, _ := args["text"].(string)
		if sel == "" {
			return "", fmt.Errorf("type requires selector")
		}
		el, err := page.Element(sel)
		if err != nil {
			return "", err
		}
		if err := el.Input(txt); err != nil {
			return "", err
		}
		return "Typed into " + sel, nil

	case "text":
		sel, _ := args["selector"].(string)
		if sel == "" {
			sel = "body"
		}
		el, err := page.Element(sel)
		if err != nil {
			return "", err
		}
		t, err := el.Text()
		if err != nil {
			return "", err
		}
		return t, nil

	case "html":
		h, err := page.HTML()
		if err != nil {
			return "", err
		}
		orig := len(h)
		if orig > maxHTMLChars {
			h = h[:maxHTMLChars] + fmt.Sprintf("\n... truncated (%d chars total)", orig)
		}
		return h, nil

	case "eval":
		script, _ := args["script"].(string)
		if script == "" {
			return "", fmt.Errorf("eval requires script (JS expression, e.g. document.title)")
		}
		js := script
		if !strings.Contains(script, "=>") && !strings.HasPrefix(strings.TrimSpace(script), "function") {
			js = fmt.Sprintf("() => (%s)", script)
		}
		res, err := page.Eval(js)
		if err != nil {
			return "", err
		}
		j, err := page.ObjectToJSON(res)
		if err != nil {
			return "", err
		}
		return j.String(), nil

	case "scroll":
		sel, _ := args["selector"].(string)
		if sel != "" {
			el, err := page.Element(sel)
			if err != nil {
				return "", err
			}
			if err := el.ScrollIntoView(); err != nil {
				return "", err
			}
			return "Scrolled " + sel + " into view", nil
		}
		y, ok := floatFromArg(args["y"])
		if !ok || y == 0 {
			return "", fmt.Errorf("scroll requires selector (scroll into view) or numeric y (viewport wheel delta, positive=down)")
		}
		if err := page.Mouse.Scroll(0, y, 1); err != nil {
			return "", err
		}
		return fmt.Sprintf("Scrolled viewport by y=%v", y), nil

	case "select":
		sel, _ := args["selector"].(string)
		val, _ := args["value"].(string)
		if sel == "" || val == "" {
			return "", fmt.Errorf("select requires selector and value (option visible text)")
		}
		el, err := page.Element(sel)
		if err != nil {
			return "", err
		}
		if err := el.Select([]string{val}, true, rod.SelectorTypeText); err != nil {
			return "", err
		}
		return "Selected option " + val + " in " + sel, nil

	case "wait":
		sel, _ := args["selector"].(string)
		if sel != "" {
			el, err := page.Element(sel)
			if err != nil {
				return "", err
			}
			if err := el.WaitVisible(); err != nil {
				return "", err
			}
			return "Element visible: " + sel, nil
		}
		if err := page.WaitStable(time.Second); err != nil {
			return "", err
		}
		return "Page stable", nil

	case "hover":
		sel, _ := args["selector"].(string)
		if sel == "" {
			return "", fmt.Errorf("hover requires selector")
		}
		el, err := page.Element(sel)
		if err != nil {
			return "", err
		}
		if err := el.Hover(); err != nil {
			return "", err
		}
		return "Hovered " + sel, nil

	case "back":
		if err := page.NavigateBack(); err != nil {
			return "", err
		}
		_ = page.WaitLoad()
		return "Navigated back", nil

	case "forward":
		if err := page.NavigateForward(); err != nil {
			return "", err
		}
		_ = page.WaitLoad()
		return "Navigated forward", nil

	case "cookies_get":
		cookies, err := page.Cookies(nil)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(cookies)
		if err != nil {
			return "", err
		}
		return string(b), nil

	case "cookies_set":
		raw, _ := args["cookies"].(string)
		if raw == "" {
			return "", fmt.Errorf("cookies_set requires cookies JSON array [{name,value,domain?,path?,url?,secure?,httpOnly?}]")
		}
		var inputs []struct {
			Name     string `json:"name"`
			Value    string `json:"value"`
			Domain   string `json:"domain"`
			Path     string `json:"path"`
			URL      string `json:"url"`
			Secure   bool   `json:"secure"`
			HTTPOnly bool   `json:"httpOnly"`
		}
		if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
			return "", fmt.Errorf("parse cookies JSON: %w", err)
		}
		params := make([]*proto.NetworkCookieParam, 0, len(inputs))
		for _, in := range inputs {
			if in.Name == "" {
				continue
			}
			params = append(params, &proto.NetworkCookieParam{
				Name:     in.Name,
				Value:    in.Value,
				Domain:   in.Domain,
				Path:     in.Path,
				URL:      in.URL,
				Secure:   in.Secure,
				HTTPOnly: in.HTTPOnly,
			})
		}
		if err := page.SetCookies(params); err != nil {
			return "", err
		}
		return fmt.Sprintf("Set %d cookie(s)", len(params)), nil

	case "tab_new":
		url, _ := args["url"].(string)
		p, err := s.browser.Page(proto.TargetCreateTarget{URL: url})
		if err != nil {
			return "", err
		}
		s.page = p
		s.page.Timeout(browserOpTimeout)
		_ = s.page.WaitLoad()
		inf, _ := s.page.Info()
		u := ""
		if inf != nil {
			u = inf.URL
		}
		return fmt.Sprintf("New tab active, url=%s", u), nil

	case "tab_list":
		pages, err := s.browser.Pages()
		if err != nil {
			return "", err
		}
		var lines []string
		for i, p := range pages {
			inf, err := p.Info()
			u := "(unknown)"
			if err == nil && inf != nil {
				u = inf.URL
			}
			mark := " "
			if s.page != nil && p.TargetID == s.page.TargetID {
				mark = "*"
			}
			lines = append(lines, fmt.Sprintf("%s%d: %s", mark, i, u))
		}
		return strings.Join(lines, "\n"), nil

	case "tab_switch":
		pattern, _ := args["url"].(string)
		if pattern == "" {
			return "", fmt.Errorf("tab_switch requires url (substring to match tab URL)")
		}
		pages, err := s.browser.Pages()
		if err != nil {
			return "", err
		}
		for _, p := range pages {
			inf, err := p.Info()
			if err != nil || inf == nil {
				continue
			}
			if strings.Contains(inf.URL, pattern) {
				if _, err := p.Activate(); err != nil {
					return "", err
				}
				s.page = p
				s.page.Timeout(browserOpTimeout)
				return fmt.Sprintf("Active tab: %s", inf.URL), nil
			}
		}
		return "", fmt.Errorf("no tab URL contains %q", pattern)

	case "download":
		dlURL, _ := args["url"].(string)
		if dlURL == "" {
			return "", fmt.Errorf("download requires url that triggers a file download")
		}
		dir, err := s.ensureDownloadDir()
		if err != nil {
			return "", err
		}
		wait := s.browser.WaitDownload(dir)
		if err := page.Navigate(dlURL); err != nil {
			return "", err
		}
		info := wait()
		if info == nil {
			return "", fmt.Errorf("download did not complete")
		}
		path := filepath.Join(dir, info.GUID)
		return fmt.Sprintf("Downloaded to %s (suggested: %s)", path, info.SuggestedFilename), nil

	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
}

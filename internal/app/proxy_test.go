package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
)

// A proxy address reads as the listener wrote one, and what couldn't be a
// proxy is refused rather than left to fail as a request without a reason.
func TestParseProxy(t *testing.T) {
	for _, c := range []struct {
		in, want string // want is the address, or "" when it's refused
	}{
		{"127.0.0.1:7890", "http://127.0.0.1:7890"},
		{" 127.0.0.1:7890 ", "http://127.0.0.1:7890"},
		{"http://127.0.0.1:7890", "http://127.0.0.1:7890"},
		{"socks5://127.0.0.1:1080", "socks5://127.0.0.1:1080"},
		{"socks5h://proxy.local:1080", "socks5h://proxy.local:1080"},
		{"http://user:pass@proxy.local:8080", "http://user:pass@proxy.local:8080"},
		{"", ""},
		{"   ", ""},
		{"not a url", ""},
		{":", ""},
		{":8080", ""},
		{"http://:8080", ""},
		{"http://127.0.0.1:", ""},
		{"foo://bar", ""},
		{"mailto:x@y", ""},
		{"ftp://127.0.0.1:21", ""},
		{"127.0.0.1:99999", ""},
		{"127.0.0.1:80x", ""},
		{"127.0.0.1:7890/path", ""},
		{"http://127.0.0.1:7890/?q=1", ""},
	} {
		u, err := parseProxy(c.in)
		if c.want == "" {
			if err == nil {
				t.Errorf("%q: read as %v, want it refused", c.in, u)
			}
			continue
		}
		if err != nil || u.String() != c.want {
			t.Errorf("%q: %v, %v, want %s", c.in, u, err, c.want)
		}
	}
}

// What the settings choose is where the app's requests go: a proxy that
// answers sees them, and none leaves them to go straight out.
func TestProxyCarriesTheAppsRequests(t *testing.T) {
	saw := make(chan string, 4)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw <- r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()

	a := newTestApp()
	a.settings.ProxyMode, a.settings.ProxyURL = "custom", proxy.URL
	a.settings.applyProxy()
	t.Cleanup(func() { settings{}.applyProxy() })

	req, _ := http.NewRequest("GET", "http://example.com/cover.jpg", nil)
	res, err := music.Client(0).Do(req)
	if err != nil {
		t.Fatalf("the request never reached the proxy: %v", err)
	}
	res.Body.Close()
	select {
	case host := <-saw:
		if host != "example.com" {
			t.Errorf("the proxy was asked for %q, want example.com", host)
		}
	default:
		t.Error("the request didn't go through the proxy")
	}
}

// The settings offer the three ways, and the address only for the one that
// has one.
func TestProxySettingsRow(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.settingsView, 780, 800)
	settled(tt)
	if _, ok := tt.Find("Proxy"); !ok {
		t.Fatalf("no Proxy row: %q", tt.Texts())
	}
	if _, ok := tt.Find("Proxy Address"); ok {
		t.Error("asks for an address with no proxy")
	}
	if err := tt.Click("Custom Proxy"); err != nil {
		t.Fatal(err)
	}
	settled(tt)
	if a.settings.ProxyMode != "custom" {
		t.Fatalf("mode %q", a.settings.ProxyMode)
	}
	if _, ok := tt.Find("Proxy Address"); !ok {
		t.Fatalf("doesn't ask where the proxy is: %q", tt.Texts())
	}
	clickSettingRole(t, tt, ui.RoleTextField, "Proxy Address")
	tt.Type("127.0.0.1:7890")
	settled(tt)
	if a.settings.ProxyURL != "127.0.0.1:7890" {
		t.Errorf("kept %q", a.settings.ProxyURL)
	}
	if err := tt.Click("System Proxy"); err != nil {
		t.Fatal(err)
	}
	settled(tt)
	if a.settings.ProxyMode != "system" {
		t.Errorf("mode %q", a.settings.ProxyMode)
	}
	if _, ok := tt.Find("Proxy Address"); ok {
		t.Error("asks for an address with the system's proxy")
	}
}

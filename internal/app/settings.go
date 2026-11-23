package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"mygo-music/internal/i18n"
	"mygo-music/internal/music"
	"mygo-music/internal/speech"
)

// settings are the listener's choices, kept in settings.json in the user
// data directory.
type settings struct {
	// Language is what the app speaks: i18n.English or i18n.Chinese, or
	// i18n.System for the system's.
	Language string `json:"language"`
	// Source is the ID of the source last used.
	Source string `json:"source,omitempty"`
	// Captions writes down episodes as they play, for their captions, on this
	// device, once the listener has the model.
	Captions bool `json:"captions"`
	// DisabledPlugins are the IDs of the plugins turned off. All are on until
	// then, the built-in one included.
	DisabledPlugins []string `json:"disabledPlugins,omitempty"`
	// PluginsChecked is when plugins were last checked for new versions, once a
	// day, and PluginUpdates the versions found, by plugin.
	PluginsChecked time.Time         `json:"pluginsChecked,omitzero"`
	PluginUpdates  map[string]string `json:"pluginUpdates,omitempty"`
	// Notch shows the quick player around the notch: the listener turns
	// it on.
	Notch bool `json:"notch"`
	// NotchActivity shows the cover and the music's levels beside the
	// notch while music plays, which hides the menu bar there.
	NotchActivity bool `json:"notchActivity"`
	// NotchVirtual keeps a notch of the app's own showing at the top of a
	// display without one; else it shows only while the pointer asks.
	NotchVirtual bool `json:"notchVirtual"`
	// NotchGreeted says the notch showed itself, once, as the first song
	// played (greet).
	NotchGreeted bool `json:"notchGreeted"`
	// Quality is what's asked of sources: standard, high or lossless. A source
	// may ignore it.
	Quality string `json:"quality"`
	// Appearance is light, dark, or system to follow the system.
	Appearance string `json:"appearance"`
	// GrowFromCover opens Now Playing out of the player bar's cover, and
	// closes it back into it: an experiment the listener turns on. Off, it
	// slides up from the bottom of the window.
	GrowFromCover bool `json:"growFromCover"`
	// Smoke makes the progress bars comets trailing smoke as music plays,
	// for a few percent of a core: the listener turns it on.
	Smoke bool `json:"smoke"`
	// LyricsStage shows lyrics on the stage, Folia's style, in Now
	// Playing, rather than scrolling.
	LyricsStage bool `json:"lyricsStage"`
	// StageStyle is how the stage sets its lyrics (stageStyles): empty for
	// Folia's classic, or the id of another of its visualizers.
	StageStyle string `json:"stageStyle,omitempty"`
	// PlayerStyle is how Now Playing shows the song playing
	// (playerStyles): empty for its cover, or a retro player's id.
	PlayerStyle string `json:"playerStyle,omitempty"`
	// StageSparks throws sparks off each word on the stage as it is sung,
	// whatever its style.
	StageSparks bool `json:"stageSparks,omitempty"`
	// Volume is where the volume was left, from 0 to 1.
	Volume float64 `json:"volume"`
	// Normalize evens out the loudness of tracks, by what their source
	// measured of each.
	Normalize bool `json:"normalize"`
	// HotComments shows a song's top comments in its quiet parts, in place of
	// the lyrics (quietComment), for a source with comments: the listener
	// turns it on.
	HotComments bool `json:"hotComments"`
	// Fog fills Now Playing's backdrop with fog of the cover's color,
	// rising and swelling with the bass: an experiment the listener turns
	// on.
	Fog bool `json:"fog"`
	// AppIcon is where the app's icon shows on macOS (appIcons); elsewhere
	// it is always in the notification area.
	AppIcon string `json:"appIcon"`
	// CacheMB is how many megabytes the songs kept on disk may take; 0
	// keeps none.
	CacheMB int `json:"cacheMB"`
	// SearchHistory keeps submitted keywords, most recent first.
	SearchHistory []string `json:"searchHistory,omitempty"`
	// ProxyMode is how the app reaches the web (proxyModes): straight out,
	// the system's proxy, or through ProxyURL. Its zero value is straight
	// out, as the app always went.
	ProxyMode string `json:"proxyMode"`
	// ProxyURL is the proxy custom mode goes through, such as
	// http://127.0.0.1:7890.
	ProxyURL string `json:"proxyURL,omitempty"`

	path string // where they were loaded from, and are saved to
}

func defaultSettings() settings {
	// No proxy, as the app has always gone. A new field keeps its zero value
	// here, so that listeners who update land on what they had.
	return settings{Language: i18n.System, NotchActivity: true, Quality: "high", Appearance: "system", Volume: 0.8, Normalize: true, AppIcon: "dock-and-menubar", CacheMB: 2048}
}

// proxyModes are how the app reaches the web, by what it goes through, in the
// order they're offered.
var proxyModes = []struct{ id, label string }{
	{"none", N("No Proxy")},
	{"system", N("System Proxy")},
	{"custom", N("Custom Proxy")},
}

// proxyFunc is what the app's requests go through, by the settings. A custom
// address it can't read leaves requests to go straight out, as none would.
func (s settings) proxyFunc() func(*http.Request) (*url.URL, error) {
	switch s.ProxyMode {
	case "system":
		return http.ProxyFromEnvironment
	case "custom":
		if u, err := parseProxy(s.ProxyURL); err == nil {
			return http.ProxyURL(u)
		}
	}
	// No proxy: the zero value too, of settings kept before there was a
	// choice, when the app went straight out.
	return nil
}

// proxySchemes are the kinds of proxy the app goes through.
var proxySchemes = []string{"http", "https", "socks5", "socks5h"}

// parseProxy reads a proxy's address as the listener wrote it, filling in
// http:// for one that only says where it is. It refuses what couldn't be a
// proxy here rather than letting it fail later, as a request that can't be
// made, with nothing said of why.
func parseProxy(addr string) (*url.URL, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("no proxy address")
	}
	// A scheme is letters, digits and +-., with :// after it. Anything else
	// with a colon, as mailto:x@y, is a host and a port or nothing at all.
	if !strings.Contains(addr, "://") {
		if i := strings.IndexByte(addr, ':'); i >= 0 && !isPort(addr[i+1:]) {
			return nil, fmt.Errorf("%q isn't an address", addr)
		}
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(proxySchemes, strings.ToLower(u.Scheme)) {
		return nil, fmt.Errorf("%q isn't a kind of proxy", u.Scheme)
	}
	// A proxy is a host and a port: no path to be sent as one.
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%q is more than a host and a port", addr)
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("no proxy host")
	}
	// A colon in the host it isn't a host's has nothing after it.
	if u.Port() == "" && strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("%q has no port after its colon", addr)
	}
	return u, nil
}

// isPort reports whether s is a port: digits, and a one a port can be.
func isPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}

func settingsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// loadSettings reads the settings, or returns the defaults.
func loadSettings() settings {
	s := defaultSettings()
	if path, err := settingsPath(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			json.Unmarshal(b, &s)
		}
		s.path = path
	}
	return s
}

// save keeps the settings where they were loaded from; defaults that were
// never loaded, as in tests, are not saved.
func (s settings) save() error {
	path := s.path
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// applyAppearance makes the windows light, dark, or follow the system. Only
// a new one: it repaints every window, waiting for the main thread, and any
// setting changed applies it.
func (s settings) applyAppearance() {
	src := mygo.ThemeSystem
	switch s.Appearance {
	case "light":
		src = mygo.ThemeLight
	case "dark":
		src = mygo.ThemeDark
	}
	if src != mygo.Theme.Source() {
		mygo.Theme.SetSource(src)
	}
}

// applyProxy makes every request the app sends go through the proxy the
// settings choose. The app calls it as it starts, before anything fetches:
// the transport it sets is read by every request after, and setting it again
// while one is on its way would be a data race, so a change to the settings
// takes effect the next time the app starts.
func (s settings) applyProxy() {
	// Plugins and installs go through the same transport already, as their
	// clients are made from it.
	music.SetProxy(s.proxyFunc())
	speech.Client = music.Client(0)
}

// qualityLevels are what can be asked of sources, by what they sound like.
var qualityLevels = []struct{ level, label string }{
	{"standard", N("Standard")},
	{"high", N("High")},
	{"lossless", N("Lossless")},
}

// cacheLimits are the sizes the song cache may take, in megabytes.
var cacheLimits = []struct {
	mb    int
	label string
}{
	{0, N("Off")},
	{1024, N("1 GB")},
	{2048, N("2 GB")},
	{5120, N("5 GB")},
	{10240, N("10 GB")},
}

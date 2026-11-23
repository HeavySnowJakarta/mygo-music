package music

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Agent is who the app says it is when fetching what it plays and shows.
const Agent = "MyGoMusic/1.0"

// transport is what every request of the app goes through: covers, streams,
// what plugins fetch and the model's download. There's one, set for the proxy
// the settings chose as the app starts, before anything fetches, so that its
// proxy is only ever written before it's read.
var transport = &http.Transport{}

// Client is a client going where the proxy lets it, nothing of the web
// otherwise. Timeout is how long its requests may take: 0 for no limit.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Transport: transport, Timeout: timeout}
}

// SetProxy makes every request go through proxy, which is asked for each
// address: nil for straight out, http.ProxyFromEnvironment for the system's
// proxy, http.ProxyURL for one the listener named. The app calls it as it
// starts, before anything fetches: a transport written while another
// goroutine reads it is a data race.
func SetProxy(proxy func(*http.Request) (*url.URL, error)) {
	transport.Proxy = proxy
}

// fetchClient gets covers: they're small, and a slow one shouldn't hold a
// slot for long.
var fetchClient = Client(30 * time.Second)

// maxFetch is the most a cover may take.
const maxFetch = 64 << 20

// ImageHeaders, when set, answers the headers to fetch the picture at u with,
// such as the cookie of a source that keeps its covers behind its session.
// It may answer nil.
var ImageHeaders func(u *url.URL) map[string]string

// SizedImage returns the URL to fetch the picture at u with to show it px
// pixels across: u with its size filled in, where it says it goes with
// {size}, else u itself.
func SizedImage(u string, px int) string {
	if px <= 0 {
		px = placeholderSize
	}
	if !strings.Contains(u, "size") {
		return u
	}
	return sizePlaceholder.ReplaceAllLiteralString(u, strconv.Itoa(px))
}

// placeholderSize is the size a picture whose URL says where its size goes
// is fetched at where nothing asks for one: the most the app shows one at.
const placeholderSize = 600

// sizePlaceholder is where a picture's URL says its size goes, in the URL
// as a source gives it or escaped, as a URL parsed and written again has it.
var sizePlaceholder = regexp.MustCompile(`\{size\}|%7[Bb]size%7[Dd]`)

// Fetch returns what's at url, of the web or a file://, such as a cover.
func Fetch(u string) ([]byte, error) {
	if path, ok := PathOf(u); ok {
		return os.ReadFile(path)
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", Agent)
	var headers map[string]string
	if ImageHeaders != nil {
		headers = ImageHeaders(req.URL)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := HeadersStay(fetchClient, req.URL.Hostname(), headers).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New(res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, maxFetch))
}

// HeadersStay returns client, or, with headers to send, one like it that
// asks a redirect to a host other than host, or one below it, without them,
// so that what a source asks with, such as its token, reaches no one else.
// It's the rule Go keeps for Cookie and Authorization, for every header.
func HeadersStay(client *http.Client, host string, headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return client
	}
	host = strings.ToLower(host)
	c := *client
	c.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if to := strings.ToLower(next.URL.Hostname()); to != host && !strings.HasSuffix(to, "."+host) {
			for k := range headers {
				next.Header.Del(k)
			}
		}
		return nil
	}
	return &c
}

// FileURL is the file:// URL of a path.
func FileURL(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path // C:/Music
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// PathOf returns the path a file:// URL names, and whether u is one.
func PathOf(u string) (string, bool) {
	if !strings.HasPrefix(u, "file:") {
		return "", false
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Path == "" {
		return "", false
	}
	path := parsed.Path
	if runtime.GOOS == "windows" {
		path = strings.TrimPrefix(path, "/")
	}
	return filepath.FromSlash(path), true
}

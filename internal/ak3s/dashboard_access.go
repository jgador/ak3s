package ak3s

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

type dashboardAccess struct {
	hostname     string
	sharedPaths  bool
	authenticate func(*http.Request) bool
}

func (a dashboardAccess) usesSharedPaths() bool {
	return a.sharedPaths || a.hostname != ""
}

func (a dashboardAccess) accepts(r *http.Request) bool {
	if dashboardLocalRequest(r) {
		return true
	}
	if a.hostname == "" || (r.Host != a.hostname && r.Host != a.hostname+":443") {
		return false
	}
	// The NetworkPolicy permits only the NGINX controller to reach this port.
	// NGINX terminates TLS and supplies the original request scheme.
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "https://"+r.Host {
		return false
	}
	site := r.Header.Get("Sec-Fetch-Site")
	return site == "" || site == "same-origin" || site == "none"
}

func dashboardAuthentication(readFile func(string) ([]byte, error)) func(*http.Request) bool {
	return func(r *http.Request) bool {
		username, password, ok := r.BasicAuth()
		if !ok {
			return false
		}
		// Read the mounted Secret for each request so rotation needs no restart.
		wantUser, userErr := readFile("/etc/ak3s-dashboard-auth/username")
		wantPassword, passwordErr := readFile("/etc/ak3s-dashboard-auth/password")
		if userErr != nil || passwordErr != nil || len(wantUser) == 0 || len(wantPassword) == 0 {
			return false
		}
		userHash, passwordHash := sha256.Sum256([]byte(username)), sha256.Sum256([]byte(password))
		wantUserHash, wantPasswordHash := sha256.Sum256(wantUser), sha256.Sum256(wantPassword)
		return subtle.ConstantTimeCompare(userHash[:], wantUserHash[:])&subtle.ConstantTimeCompare(passwordHash[:], wantPasswordHash[:]) == 1
	}
}

// dashboardToolProxy uses fixed in-cluster destinations. Victoria UIs infer their
// API base from the browser path; strip only the public prefix on upstream calls.
// Headlamp uses its native base URL, including for WebSocket connections.
func dashboardToolProxy(target *url.URL, prefix string, stripPrefix bool) http.Handler {
	proxy := &httputil.ReverseProxy{
		Transport: &http.Transport{
			// Internal Service traffic must not use an inherited HTTP proxy.
			DialContext:     (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
		},
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = r.In.Host
			r.SetXForwarded()
			if r.In.Header.Get("X-Forwarded-Proto") == "https" {
				r.Out.Header.Set("X-Forwarded-Proto", "https")
			}
			// Never pass the dashboard password to another application. Headlamp
			// needs its own Bearer token and cookies for Kubernetes authentication.
			if stripPrefix {
				r.Out.Header.Del("Authorization")
				r.Out.Header.Del("Cookie")
			} else if _, _, basic := r.In.BasicAuth(); basic {
				r.Out.Header.Del("Authorization")
			}
		},
		ModifyResponse: func(response *http.Response) error {
			location := response.Header.Get("Location")
			if location == "" {
				return nil
			}
			u, err := url.Parse(location)
			if err != nil || (u.Host != "" && u.Host != target.Host) || !strings.HasPrefix(u.Path, "/") {
				return nil
			}
			u.Scheme, u.Host = "", ""
			if stripPrefix {
				u.Path = prefix + u.Path
				if u.RawPath != "" {
					u.RawPath = prefix + u.RawPath
				}
			}
			response.Header.Set("Location", u.String())
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "Tool unavailable. Check its workload and Service.", http.StatusBadGateway)
		},
		FlushInterval: -1,
	}
	if stripPrefix {
		return http.StripPrefix(prefix, proxy)
	}
	return proxy
}

func dashboardToolHandlers() map[string]http.Handler {
	return map[string]http.Handler{
		"metrics":  dashboardToolProxy(&url.URL{Scheme: "http", Host: "victoria-metrics.observability.svc:8428"}, "/metrics", true),
		"logs":     dashboardToolProxy(&url.URL{Scheme: "http", Host: "victoria-logs.observability.svc:9428"}, "/logs", true),
		"headlamp": dashboardToolProxy(&url.URL{Scheme: "http", Host: "headlamp.headlamp.svc:80"}, "/headlamp", false),
	}
}

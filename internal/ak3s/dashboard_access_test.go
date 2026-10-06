package ak3s

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestDashboardLocalSharedPaths(t *testing.T) {
	c := testConfig(t)
	c.DashboardSharedPaths = true
	metadata, err := dashboardMetadataFor(c)
	if err != nil {
		t.Fatal(err)
	}
	h := dashboardHandler(fstest.MapFS{"index.html": {Data: []byte("AK3S")}}, func(context.Context) (dashboardData, error) {
		return dashboardProjection(metadata, dashboardVersion{}, dashboardResources{}, dashboardResources{}), nil
	}, dashboardAccess{sharedPaths: true, authenticate: func(*http.Request) bool {
		t.Fatal("local port-forward required a public authentication Secret")
		return false
	}})
	for _, tc := range []struct {
		path, host, remote, origin, site, location string
		status                                     int
	}{
		{path: "/", status: 200},
		{path: "/api/snapshot", status: 200},
		{path: "/metrics", location: "/metrics/vmui/", status: 307},
		{path: "/logs", location: "/logs/select/vmui/", status: 307},
		{path: "/headlamp", location: "/headlamp/", status: 307},
		{path: "/metrics", remote: "192.0.2.10:1234", status: 403},
		{path: "/logs", host: "untrusted.example.com", status: 403},
		{path: "/headlamp", origin: "https://untrusted.example.com", status: 403},
		{path: "/api/snapshot", site: "cross-site", status: 403},
	} {
		r := httptest.NewRequest("GET", "http://localhost:6173"+tc.path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		if tc.remote != "" {
			r.RemoteAddr = tc.remote
		}
		if tc.host != "" {
			r.Host = tc.host
		}
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Location") != tc.location {
			t.Fatalf("%s: got status %d and redirect %q", tc.path, w.Code, w.Header().Get("Location"))
		}
		if tc.path == "/api/snapshot" && tc.status == 200 {
			var data dashboardData
			if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if data.Access != "shared" || len(data.Tools) != 3 {
				t.Fatal("missing shared access guide")
			}
			for _, tool := range data.Tools {
				if tool.URL != "/"+tool.ID || tool.PortForward != "" {
					t.Fatal("tool link escaped the current dashboard address")
				}
			}
		}
	}
}

func TestDashboardHTTPSAccess(t *testing.T) {
	password := "fictional-password"
	auth := dashboardAuthentication(func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "/username") {
			return []byte("viewer"), nil
		}
		return []byte(password), nil
	})
	h := dashboardHandler(fstest.MapFS{"index.html": {Data: []byte("AK3S")}}, func(context.Context) (dashboardData, error) {
		return dashboardData{Access: "https"}, nil
	}, dashboardAccess{hostname: "ak3s.example.com", authenticate: auth})
	for _, tc := range []struct {
		path, method, host, origin, site, scheme, password, location string
		status                                                       int
	}{
		{path: "/", status: 401},
		{path: "/api/snapshot", status: 401},
		{path: "/metrics/prometheus/api/v1/query", status: 401},
		{path: "/logs/select/logsql/query", method: "POST", status: 401},
		{path: "/", password: "wrong", status: 401},
		{path: "/", password: password, status: 200},
		{path: "/api/snapshot", password: password, origin: "https://ak3s.example.com", site: "same-origin", status: 200},
		{path: "/api/snapshot?file=private", password: password, status: 400},
		{path: "/api/snapshot", method: "POST", password: password, status: 405},
		{path: "/", password: password, host: "untrusted.example.com", status: 403},
		{path: "/", password: password, host: "ak3s.example.com:8080", status: 403},
		{path: "/", password: password, host: "ak3s.example.com:443", status: 200},
		{path: "/", password: password, scheme: "http", status: 403},
		{path: "/", password: password, origin: "http://ak3s.example.com", status: 403},
		{path: "/", password: password, origin: "https://untrusted.example.com", status: 403},
		{path: "/", password: password, site: "same-site", status: 403},
		{path: "/metrics", password: password, location: "/metrics/vmui/", status: 307},
		{path: "/metrics/", password: password, location: "/metrics/vmui/", status: 307},
		{path: "/logs?query=example%2Fvalue", password: password, location: "/logs/select/vmui/?query=example%2Fvalue", status: 307},
		{path: "/logs/", password: password, location: "/logs/select/vmui/", status: 307},
		{path: "/headlamp", location: "/headlamp/", status: 307},
		{path: "/headlamp-other", status: 401},
		{path: "/metrics-other", password: password, status: 404},
	} {
		t.Run(fmt.Sprintf("%s-%s-%d-%s", tc.path, tc.method, tc.status, tc.origin+tc.site+tc.host+tc.scheme), func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			r := httptest.NewRequest(method, "http://ak3s.example.com"+tc.path, nil)
			r.RemoteAddr = "10.42.0.2:1234"
			r.Header.Set("X-Forwarded-Proto", "https")
			if tc.scheme != "" {
				r.Header.Set("X-Forwarded-Proto", tc.scheme)
			}
			if tc.host != "" {
				r.Host = tc.host
			}
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			if tc.password != "" {
				r.SetBasicAuth("viewer", tc.password)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Location") != tc.location {
				t.Fatalf("got %d, location %q; want %d, %q", w.Code, w.Header().Get("Location"), tc.status, tc.location)
			}
			if tc.status == 401 && w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing login challenge")
			}
		})
	}
	// Mounted Secret updates take effect without a process restart.
	r := httptest.NewRequest("GET", "https://ak3s.example.com/", nil)
	r.SetBasicAuth("viewer", password)
	password = "fictional-new-password"
	if auth(r) {
		t.Fatal("old password accepted after rotation")
	}
	r.SetBasicAuth("viewer", password)
	if !auth(r) {
		t.Fatal("new password rejected")
	}
	for _, missing := range []bool{false, true} {
		auth = dashboardAuthentication(func(string) ([]byte, error) {
			if missing {
				return nil, errors.New("private read error")
			}
			return nil, nil
		})
		if auth(r) {
			t.Fatal("missing or empty credentials accepted")
		}
	}
}

func TestDashboardToolProxyPathsAndCredentials(t *testing.T) {
	for _, tc := range []struct {
		prefix, path, upstreamPath, method, body string
		strip                                    bool
	}{
		{"/metrics", "/metrics/vmui/static/app.js", "/vmui/static/app.js", "GET", "", true},
		{"/metrics", "/metrics/prometheus/api/v1/query", "/prometheus/api/v1/query", "POST", "query=up", true},
		{"/logs", "/logs/select/vmui/static/app.js", "/select/vmui/static/app.js", "GET", "", true},
		{"/logs", "/logs/select/logsql/query", "/select/logsql/query", "POST", "query=example", true},
		{"/headlamp", "/headlamp/clusters/main/api/v1/pods", "/headlamp/clusters/main/api/v1/pods", "POST", "{}", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if r.URL.Path != tc.upstreamPath || r.URL.RawQuery != "q=a%2Fb" || r.Method != tc.method || string(body) != tc.body {
					t.Errorf("incorrect upstream request: %s %s body %q", r.Method, r.URL, body)
				}
				if r.Host != "ak3s.example.com" || r.Header.Get("X-Forwarded-Proto") != "https" {
					t.Error("public origin lost")
				}
				if tc.strip && (r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "") {
					t.Error("dashboard credentials reached tool")
				}
				if !tc.strip && (r.Header.Get("Authorization") != "Bearer fictional-token" || r.Header.Get("Cookie") != "session=fictional") {
					t.Error("Headlamp authentication lost")
				}
				w.Write([]byte("tool response"))
			}))
			defer upstream.Close()
			target, _ := url.Parse(upstream.URL)
			r := httptest.NewRequest(tc.method, "http://ak3s.example.com"+tc.path+"?q=a%2Fb", strings.NewReader(tc.body))
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("Cookie", "session=fictional")
			if tc.strip {
				r.SetBasicAuth("viewer", "fictional-password")
			} else {
				r.Header.Set("Authorization", "Bearer fictional-token")
			}
			w := httptest.NewRecorder()
			dashboardToolProxy(target, tc.prefix, tc.strip).ServeHTTP(w, r)
			if w.Code != 200 || w.Body.String() != "tool response" {
				t.Fatalf("proxy failed: %d", w.Code)
			}
		})
	}
}

func TestDashboardToolProxyRedirectsAndFailures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("Basic password reached Headlamp")
		}
		w.Header().Set("Location", "/vmui/?q=a%2Fb")
		w.WriteHeader(302)
	}))
	target, _ := url.Parse(upstream.URL)
	for _, strip := range []bool{false, true} {
		r := httptest.NewRequest("GET", "http://ak3s.example.com/metrics/redirect", nil)
		r.SetBasicAuth("viewer", "fictional-password")
		w := httptest.NewRecorder()
		dashboardToolProxy(target, "/metrics", strip).ServeHTTP(w, r)
		want := "/vmui/?q=a%2Fb"
		if strip {
			want = "/metrics" + want
		}
		if w.Code != 302 || w.Header().Get("Location") != want {
			t.Fatal("redirect escaped public prefix")
		}
	}
	upstream.Close()
	w := httptest.NewRecorder()
	dashboardToolProxy(target, "/metrics", true).ServeHTTP(w, httptest.NewRequest("GET", "http://ak3s.example.com/metrics/vmui/", nil))
	if w.Code != 502 || strings.Contains(w.Body.String(), target.Host) {
		t.Fatal("upstream failure not sanitized")
	}
}

func TestDashboardHeadlampWebSocketUpgrade(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/headlamp/watch" || r.Header.Get("Authorization") != "Bearer fictional-token" {
			t.Error("upgrade lost path or token")
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		rw.Flush()
		data := make([]byte, 4)
		if _, err := io.ReadFull(rw, data); err != nil {
			t.Error(err)
			return
		}
		rw.Write(data)
		rw.Flush()
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	proxy := httptest.NewServer(dashboardToolProxy(target, "/headlamp", false))
	defer proxy.Close()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(proxy.URL, "http://"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(conn, "GET /headlamp/watch HTTP/1.1\r\nHost: ak3s.example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nAuthorization: Bearer fictional-token\r\n\r\n")
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 101 {
		t.Fatalf("upgrade status %d", response.StatusCode)
	}
	fmt.Fprint(conn, "ping")
	data := make([]byte, 4)
	if _, err := io.ReadFull(reader, data); err != nil || string(data) != "ping" {
		t.Fatalf("upgrade tunnel failed: %v", err)
	}
}

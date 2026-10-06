package ak3s

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const dashboardServiceAccount = "/var/run/secrets/kubernetes.io/serviceaccount/"

type dashboardAPI struct {
	client    *http.Client
	endpoint  string
	readToken func() ([]byte, error)
}

func (api dashboardAPI) query(ctx context.Context, path string, target any) error {
	token, err := api.readToken() // Projected service-account tokens rotate in place.
	if err != nil {
		return errors.New("cannot read dashboard service account")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api.endpoint+path, nil)
	if err != nil {
		return errors.New("cannot create Kubernetes request")
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	res, err := api.client.Do(req)
	if err != nil {
		return errors.New("cannot read Kubernetes API")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New("Kubernetes API rejected dashboard request")
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(target); err != nil {
		return errors.New("invalid Kubernetes API response")
	}
	return nil
}

func (api dashboardAPI) list(ctx context.Context, path string) (dashboardResources, error) {
	var result dashboardResources
	continuation := ""
	for {
		var page dashboardResources
		query := url.Values{"limit": {"100"}}
		if continuation != "" {
			query.Set("continue", continuation)
		}
		if err := api.query(ctx, path+"?"+query.Encode(), &page); err != nil {
			return result, err
		}
		result.Items = append(result.Items, page.Items...)
		if page.Metadata.Continue == "" {
			return result, nil
		}
		if page.Metadata.Continue == continuation {
			return result, errors.New("invalid Kubernetes pagination")
		}
		continuation = page.Metadata.Continue
	}
}

func (api dashboardAPI) snapshot(ctx context.Context, metadata dashboardMetadata) (dashboardData, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var version dashboardVersion
	if err := api.query(ctx, "/version", &version); err != nil {
		return dashboardData{}, err
	}
	nodes, err := api.list(ctx, "/api/v1/nodes")
	if err != nil {
		return dashboardData{}, err
	}
	var workloads dashboardResources
	for _, resource := range []string{"deployments", "statefulsets", "daemonsets"} {
		page, err := api.list(ctx, "/apis/apps/v1/"+resource)
		if err != nil {
			return dashboardData{}, err
		}
		// Typed list items may omit kind; readiness depends on the workload type.
		kind := map[string]string{"deployments": "Deployment", "statefulsets": "StatefulSet", "daemonsets": "DaemonSet"}[resource]
		for i := range page.Items {
			page.Items[i].Kind = kind
		}
		workloads.Items = append(workloads.Items, page.Items...)
	}
	return dashboardProjection(metadata, version, nodes, workloads), nil
}

func dashboardLocalRequest(r *http.Request) bool {
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(remote).IsLoopback() {
		return false
	}
	u, err := url.Parse("http://" + r.Host)
	if err != nil || u.User != nil || u.Path != "" {
		return false
	}
	if u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback() {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	return true
}

func dashboardHandler(assets fs.FS, collect func(context.Context) (dashboardData, error)) http.Handler {
	var mu sync.Mutex
	var cached []byte
	var collected time.Time
	snapshot := func(ctx context.Context) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cached != nil && time.Since(collected) < 5*time.Second {
			return cached, nil
		}
		data, err := collect(ctx)
		if err != nil {
			return nil, err
		}
		cached, err = json.Marshal(data)
		if err == nil {
			collected = time.Now()
		}
		return cached, err
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Only read-only requests are supported.", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/readyz" {
			if _, err := snapshot(r.Context()); err != nil {
				http.Error(w, "Dashboard data unavailable.", http.StatusServiceUnavailable)
			}
			return
		}
		if !dashboardLocalRequest(r) {
			http.Error(w, "Local, same-origin access is required.", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/api/snapshot" {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.RawQuery != "" || r.URL.ForceQuery {
				http.Error(w, `{"error":"This endpoint accepts no parameters."}`, http.StatusBadRequest)
				return
			}
			data, err := snapshot(r.Context())
			if err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"error":"Cannot read cluster data. Check the dashboard service account and Kubernetes API."}`)
				return
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write(data)
			}
			return
		}
		// Hash routes use index.html; unknown files and directories are never listed.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		info, err := fs.Stat(assets, path)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}

// ServeDashboard runs the private in-cluster dashboard with a read-only service account.
func ServeDashboard(ctx context.Context, assets fs.FS) error {
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return errors.New("dashboard assets are missing; build the dashboard image")
	}
	raw, err := os.ReadFile("/etc/ak3s-dashboard/metadata.json")
	if err != nil {
		return errors.New("dashboard configuration is unavailable")
	}
	var metadata dashboardMetadata
	if json.Unmarshal(raw, &metadata) != nil || metadata.ClusterName == "" {
		return errors.New("invalid dashboard configuration")
	}
	ca, err := os.ReadFile(dashboardServiceAccount + "ca.crt")
	if err != nil {
		return errors.New("Kubernetes service-account CA is unavailable")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return errors.New("invalid Kubernetes service-account CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	api := dashboardAPI{endpoint: "https://kubernetes.default.svc", client: &http.Client{
		Timeout: 8 * time.Second, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Kubernetes redirects are not allowed") },
	}, readToken: func() ([]byte, error) { return os.ReadFile(dashboardServiceAccount + "token") }}
	server := &http.Server{Addr: ":8080", ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: time.Minute,
		Handler: dashboardHandler(assets, func(ctx context.Context) (dashboardData, error) { return api.snapshot(ctx, metadata) })}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		case <-stopped:
		}
	}()
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

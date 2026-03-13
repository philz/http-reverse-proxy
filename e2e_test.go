package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func startBackend(t *testing.T, handler http.Handler) (addr string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve(ln)
	return ln.Addr().String()
}

func startServer(t *testing.T, secret string) (addr string, s *server) {
	t.Helper()
	s = &server{secret: secret}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: s}
	t.Cleanup(func() {
		s.close()
		srv.Close()
	})
	go srv.Serve(ln)
	return ln.Addr().String(), s
}

func startClient(t *testing.T, serverAddr, secret, backendAddr string) {
	t.Helper()

	go dialAndServe(serverAddr, secret, backendAddr)

	// Wait for the client to attach by polling.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + serverAddr + "/healthcheck")
		if err == nil {
			io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadGateway {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("client did not attach in time")
}

func TestBasicProxy(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from backend: %s", r.URL.Path)
	}))

	serverAddr, _ := startServer(t, "test-secret")
	startClient(t, serverAddr, "test-secret", backend)

	resp, err := http.Get("http://" + serverAddr + "/foo/bar")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); got != "hello from backend: /foo/bar" {
		t.Errorf("got %q, want %q", got, "hello from backend: /foo/bar")
	}
}

func TestQueryStringPreserved(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "q=%s", r.URL.Query().Get("q"))
	}))

	serverAddr, _ := startServer(t, "s")
	startClient(t, serverAddr, "s", backend)

	resp, err := http.Get("http://" + serverAddr + "/search?q=hello+world")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); got != "q=hello world" {
		t.Errorf("got %q", got)
	}
}

func TestRequestHeaders(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "x-custom=%s", r.Header.Get("X-Custom"))
	}))

	serverAddr, _ := startServer(t, "s")
	startClient(t, serverAddr, "s", backend)

	req, _ := http.NewRequest("GET", "http://"+serverAddr+"/", nil)
	req.Header.Set("X-Custom", "test-value")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); got != "x-custom=test-value" {
		t.Errorf("got %q", got)
	}
}

func TestResponseHeaders(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "yes")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, "created")
	}))

	serverAddr, _ := startServer(t, "s")
	startClient(t, serverAddr, "s", backend)

	resp, err := http.Get("http://" + serverAddr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
	if resp.Header.Get("X-Backend") != "yes" {
		t.Errorf("missing X-Backend header")
	}
}

func TestPOSTWithBody(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "method=%s body=%s", r.Method, string(body))
	}))

	serverAddr, _ := startServer(t, "s")
	startClient(t, serverAddr, "s", backend)

	resp, err := http.Post("http://"+serverAddr+"/submit", "text/plain", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); got != "method=POST body=payload" {
		t.Errorf("got %q", got)
	}
}

func TestHTTPMethods(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Method)
	}))

	serverAddr, _ := startServer(t, "s")
	startClient(t, serverAddr, "s", backend)

	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD"} {
		req, _ := http.NewRequest(method, "http://"+serverAddr+"/", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if method == "HEAD" {
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("HEAD: status = %d", resp.StatusCode)
			}
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != method {
			t.Errorf("%s: got %q", method, body)
		}
	}
}

func TestWrongSecret(t *testing.T) {
	serverAddr, _ := startServer(t, "correct-secret")

	err := dialAndServe(serverAddr, "wrong-secret", "127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error with wrong secret")
	}
	if !strings.Contains(err.Error(), "unexpected status") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMissingSecret(t *testing.T) {
	serverAddr, _ := startServer(t, "the-secret")

	err := dialAndServe(serverAddr, "", "127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error with empty secret")
	}
}

func TestNoBackendConnected(t *testing.T) {
	serverAddr, _ := startServer(t, "s")

	resp, err := http.Get("http://" + serverAddr + "/anything")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

func TestAttachEndpointBadUpgrade(t *testing.T) {
	serverAddr, _ := startServer(t, "s")

	resp, err := http.Get("http://" + serverAddr + attachPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestBadMagic(t *testing.T) {
	serverAddr, _ := startServer(t, "s")

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	req, _ := http.NewRequest("POST", "http://"+serverAddr+attachPath, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", upgradeName)
	req.Header.Set(secretHeader, "s")
	req.Write(conn)

	reader := bufio.NewReader(conn)
	resp, _ := http.ReadResponse(reader, req)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	// Send wrong magic
	conn.Write([]byte("bad-magic-here\n"))

	// Server should close the connection
	buf := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected connection to be closed after bad magic")
	}
}

func TestConcurrentRequests(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		fmt.Fprintf(w, "path=%s", r.URL.Path)
	}))

	serverAddr, _ := startServer(t, "s")
	startClient(t, serverAddr, "s", backend)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := fmt.Sprintf("/req/%d", i)
			resp, err := http.Get("http://" + serverAddr + path)
			if err != nil {
				errs[i] = err
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			want := "path=" + path
			if string(body) != want {
				errs[i] = fmt.Errorf("got %q, want %q", string(body), want)
			}
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("request %d: %v", i, err)
		}
	}
}

func TestLargeBody(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(w, r.Body)
	}))

	serverAddr, _ := startServer(t, "s")
	startClient(t, serverAddr, "s", backend)

	bigBody := strings.Repeat("x", 1<<20)
	resp, err := http.Post("http://"+serverAddr+"/upload", "application/octet-stream", strings.NewReader(bigBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 1<<20 {
		t.Errorf("response body length = %d, want %d", len(body), 1<<20)
	}
}

func TestMultiplePaths(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			fmt.Fprint(w, "alpha")
		case "/b":
			fmt.Fprint(w, "beta")
		case "/c":
			fmt.Fprint(w, "gamma")
		default:
			http.NotFound(w, r)
		}
	}))

	serverAddr, _ := startServer(t, "s")
	startClient(t, serverAddr, "s", backend)

	for _, tc := range []struct {
		path string
		want string
		code int
	}{
		{"/a", "alpha", 200},
		{"/b", "beta", 200},
		{"/c", "gamma", 200},
		{"/d", "404 page not found\n", 404},
	} {
		resp, err := http.Get("http://" + serverAddr + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.code {
			t.Errorf("%s: status = %d, want %d", tc.path, resp.StatusCode, tc.code)
		}
		if string(body) != tc.want {
			t.Errorf("%s: body = %q, want %q", tc.path, string(body), tc.want)
		}
	}
}

func TestBackendErrorForwarded(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))

	serverAddr, _ := startServer(t, "s")
	startClient(t, serverAddr, "s", backend)

	resp, err := http.Get("http://" + serverAddr + "/fail")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestExtraAttachHeaders(t *testing.T) {
	backend := startBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))

	serverAddr, srv := startServer(t, "s")

	extra := http.Header{}
	extra.Set("X-Region", "us-east-1")
	extra.Set("X-Instance", "abc123")
	go dialAndServe(serverAddr, "s", backend, extra)

	// Wait for attach.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + serverAddr + "/healthcheck")
		if err == nil {
			io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadGateway {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	srv.mu.Lock()
	h := srv.attachHeaders
	srv.mu.Unlock()

	if got := h.Get("X-Region"); got != "us-east-1" {
		t.Errorf("X-Region = %q, want %q", got, "us-east-1")
	}
	if got := h.Get("X-Instance"); got != "abc123" {
		t.Errorf("X-Instance = %q, want %q", got, "abc123")
	}
}

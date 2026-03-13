package main

import (
	"crypto/subtle"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"

	"golang.org/x/net/http2"
)

const (
	attachPath   = "/__reverse_proxy"
	secretHeader = "X-Reverse-Proxy-Secret"
	upgradeName  = "reverse-proxy"
	magic        = "reverse-proxy\n"
)

// server is the listen-mode server (Server A).
// It accepts a reverse connection from a client and proxies all
// incoming HTTP requests through that connection.
type server struct {
	secret string

	mu            sync.Mutex
	client        *http.Client
	hijacked      net.Conn
	done          chan struct{} // closed when the current attachment should be torn down
	attachHeaders http.Header  // headers from the most recent attach request
}

func runServer(addr, secret string) error {
	s := &server{secret: secret}
	return http.ListenAndServe(addr, s)
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == attachPath {
		s.handleAttach(w, r)
		return
	}
	s.handleProxy(w, r)
}

func (s *server) handleAttach(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Upgrade") != upgradeName {
		http.Error(w, "bad upgrade", http.StatusBadRequest)
		return
	}
	got := r.Header.Get(secretHeader)
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.secret)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, bufrw, err := hj.Hijack()
	if err != nil {
		log.Printf("hijack error: %v", err)
		return
	}

	bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: reverse-proxy\r\n\r\n")
	bufrw.Flush()

	// Read magic bytes. At this point the client hasn't sent anything beyond
	// the POST request (it's waiting for our 101 response), so conn is safe
	// to read from directly — bufrw.Reader has no extra buffered data.
	gotBytes := make([]byte, len(magic))
	if _, err := io.ReadFull(conn, gotBytes); err != nil {
		log.Printf("failed to read magic: %v", err)
		conn.Close()
		return
	}
	if string(gotBytes) != magic {
		log.Printf("bad magic: %q", string(gotBytes))
		conn.Close()
		return
	}

	// Create HTTP/2 client over the hijacked connection.
	transport := &http2.Transport{AllowHTTP: true}
	clientConn, err := transport.NewClientConn(conn)
	if err != nil {
		log.Printf("failed to create h2 client conn: %v", err)
		conn.Close()
		return
	}

	httpc := &http.Client{Transport: clientConn}
	done := make(chan struct{})

	s.mu.Lock()
	// Close any previous attachment.
	if s.hijacked != nil {
		s.hijacked.Close()
	}
	if s.done != nil {
		close(s.done)
	}
	s.client = httpc
	s.hijacked = conn
	s.done = done
	s.attachHeaders = r.Header.Clone()
	s.mu.Unlock()

	log.Printf("client attached from %s", conn.RemoteAddr())

	// Block until this attachment is superseded or torn down.
	<-done
	log.Printf("client detached from %s", conn.RemoteAddr())
}

func (s *server) handleProxy(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	if client == nil {
		http.Error(w, "no backend connected", http.StatusBadGateway)
		return
	}

	log.Printf("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "localhost"
			req.URL.Path = r.URL.Path
			req.URL.RawQuery = r.URL.RawQuery
			req.Host = r.Host
		},
		Transport: client.Transport,
	}
	proxy.ServeHTTP(w, r)
}

// close shuts down the hijacked connection.
func (s *server) close() {
	s.mu.Lock()
	if s.hijacked != nil {
		s.hijacked.Close()
	}
	if s.done != nil {
		close(s.done)
		s.done = nil
	}
	s.mu.Unlock()
}

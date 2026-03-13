package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"golang.org/x/net/http2"
)

// bufferedConn wraps a net.Conn so reads come from a bufio.Reader
// (draining any buffered bytes first) while writes go directly to the conn.
type bufferedConn struct {
	net.Conn
	r io.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func runClient(localPort, serverAddr, secret string, extraHeaders http.Header) error {
	return dialAndServe(serverAddr, secret, "localhost:"+localPort, extraHeaders)
}

func dialAndServe(serverAddr, secret, targetAddr string, extraHeaders ...http.Header) error {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", serverAddr, err)
	}

	// Send the upgrade request.
	req, err := http.NewRequest("POST", "http://"+serverAddr+attachPath, nil)
	if err != nil {
		conn.Close()
		return err
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", upgradeName)
	req.Header.Set(secretHeader, secret)
	if len(extraHeaders) > 0 {
		for k, vals := range extraHeaders[0] {
			for _, v := range vals {
				req.Header.Set(k, v)
			}
		}
	}

	if err := req.Write(conn); err != nil {
		conn.Close()
		return fmt.Errorf("write upgrade request: %w", err)
	}

	// Read the upgrade response.
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		conn.Close()
		return fmt.Errorf("read upgrade response: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Send magic bytes.
	if _, err := conn.Write([]byte(magic)); err != nil {
		conn.Close()
		return fmt.Errorf("write magic: %w", err)
	}

	// Wrap conn so that any bytes buffered by the reader are not lost.
	bc := &bufferedConn{Conn: conn, r: reader}

	// Serve HTTP/2 over the hijacked connection.
	// The server will make requests to us, and we proxy them to targetAddr.
	target, err := url.Parse("http://" + targetAddr)
	if err != nil {
		conn.Close()
		return fmt.Errorf("parse target: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	h2srv := &http2.Server{}
	h2srv.ServeConn(bc, &http2.ServeConnOpts{
		Handler: proxy,
	})

	log.Printf("connection closed")
	return nil
}

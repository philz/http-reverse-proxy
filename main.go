package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	switch os.Args[1] {
	case "listen":
		listenCmd(os.Args[2:])
	case "attach":
		attachCmd(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  http-reverse-proxy listen --attach-addr :8000 --serve-addr :8001 --secret TOKEN
  http-reverse-proxy attach --forward PORT --secret TOKEN [-H Key:Value]... SERVER_URL`)
	os.Exit(1)
}

func listenCmd(args []string) {
	fs := flag.NewFlagSet("listen", flag.ExitOnError)
	attachAddr := fs.String("attach-addr", ":8000", "Address for attach connections")
	serveAddr := fs.String("serve-addr", ":8001", "Address for serving proxied traffic")
	secret := fs.String("secret", "", "Shared secret")
	fs.Parse(args)

	if *secret == "" {
		fmt.Fprintln(os.Stderr, "error: --secret is required")
		os.Exit(1)
	}

	if err := runServer(*attachAddr, *serveAddr, *secret); err != nil {
		log.Fatal(err)
	}
}

type headerFlag http.Header

func (h *headerFlag) String() string { return "" }
func (h *headerFlag) Set(val string) error {
	k, v, ok := strings.Cut(val, ":")
	if !ok {
		return fmt.Errorf("header must be Key:Value, got %q", val)
	}
	http.Header(*h).Set(strings.TrimSpace(k), strings.TrimSpace(v))
	return nil
}

func attachCmd(args []string) {
	fs := flag.NewFlagSet("attach", flag.ExitOnError)
	forward := fs.String("forward", "", "Local port to forward to (e.g. 1234)")
	secret := fs.String("secret", "", "Shared secret")
	headers := headerFlag(http.Header{})
	fs.Var(&headers, "H", "Extra header (Key:Value), may be repeated")
	fs.Parse(args)

	if *secret == "" {
		fmt.Fprintln(os.Stderr, "error: --secret is required")
		os.Exit(1)
	}
	if *forward == "" {
		fmt.Fprintln(os.Stderr, "error: --forward is required")
		os.Exit(1)
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: http-reverse-proxy attach --forward PORT --secret TOKEN SERVER_URL")
		os.Exit(1)
	}

	serverAddr := fs.Arg(0)
	log.Printf("forwarding to localhost:%s via %s", *forward, serverAddr)
	if err := runClient(*forward, serverAddr, *secret, http.Header(headers)); err != nil {
		log.Fatal(err)
	}
}

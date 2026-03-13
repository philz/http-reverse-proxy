package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	listenAddr := flag.String("listen", "", "Listen address for server mode (e.g. :8000)")
	forward := flag.String("forward", "", "Local port to forward to in client mode (e.g. 1234)")
	secret := flag.String("secret", "", "Shared secret for authentication")
	flag.Parse()

	if *secret == "" {
		fmt.Fprintln(os.Stderr, "error: --secret is required")
		os.Exit(1)
	}

	if *listenAddr != "" && *forward != "" {
		fmt.Fprintln(os.Stderr, "error: --listen and --forward are mutually exclusive")
		os.Exit(1)
	}

	if *listenAddr != "" {
		log.Printf("listening on %s", *listenAddr)
		if err := runServer(*listenAddr, *secret); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *forward != "" {
		args := flag.Args()
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: http-reverse-proxy --forward PORT SERVER_ADDR")
			os.Exit(1)
		}
		serverAddr := args[0]
		log.Printf("forwarding to localhost:%s via %s", *forward, serverAddr)
		if err := runClient(*forward, serverAddr, *secret); err != nil {
			log.Fatal(err)
		}
		return
	}

	fmt.Fprintln(os.Stderr, "error: one of --listen or --forward is required")
	flag.Usage()
	os.Exit(1)
}

//go:build !windows

// Command tui-ssh-server demonstrates serving a LUX TUI over SSH. Each
// session is bridged onto a real pty (creack/pty): the child runs as the
// session leader there, so the LUX engine in the child sees a genuine
// terminal — raw mode, capability probes negotiated end-to-end with the
// client's terminal, SIGWINCH on resize, and exit statuses propagated back.
// The server itself is only byte plumbing; no LUX internals are involved.
//
// With no positional arguments the server serves its own demo TUI (re-exec
// with -child). Pass a command to serve that instead, e.g. the showcase:
//
//	go build .                            # inside examples/07-ssh
//	./tui-ssh-server -addr 127.0.0.1:2222
//	ssh -t -p 2222 localhost              # password: letmein
//
//	./tui-ssh-server -- ../06-showcase/showcase
//	./tui-ssh-server -- /bin/sh           # a plain shell works too
//
// Unix only: creack/pty has no Windows port.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	lux "github.com/GofMan5/lux"
	gssh "github.com/gliderlabs/ssh"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:2222", "listen address")
	password := flag.String("password", "letmein", "shared password for every login")
	child := flag.Bool("child", false, "internal: run the demo TUI instead of the server")
	flag.Parse()

	if *child {
		if err := lux.Run(context.Background(), &demoApp{}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	argv := flag.Args()
	if len(argv) == 0 {
		self, err := os.Executable()
		if err != nil {
			log.Fatal(err)
		}
		argv = []string{self, "-child"}
	}

	srv := &gssh.Server{
		Addr:    *addr,
		Handler: func(s gssh.Session) { serve(s, argv) },
		PasswordHandler: func(ctx gssh.Context, pass string) bool {
			return pass == *password
		},
	}
	log.Printf("tui-ssh-server listening on %s (password %q), serving %v", *addr, *password, argv)
	log.Printf("a host key is generated at startup; expect an unknown-host prompt on first connect")
	log.Fatal(srv.ListenAndServe())
}

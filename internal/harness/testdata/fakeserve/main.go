// Command fakeserve imitates the process surface of `opencode serve` used
// by the ProcessManager integration tests: it serves a Basic-Auth
// /global/health on the requested host:port, reports the address it actually
// bound via FAKE_SERVE_ADDR_FILE, and crashes on demand via /global/crash.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: fakeserve serve --hostname <host> --port <port>")
		os.Exit(2)
	}
	host, port := "127.0.0.1", "0"
	for i := 1; i+1 < len(args); i += 2 {
		switch args[i] {
		case "--hostname":
			host = args[i+1]
		case "--port":
			port = args[i+1]
		}
	}
	password := os.Getenv("OPENCODE_SERVER_PASSWORD")
	if password == "" {
		fmt.Fprintln(os.Stderr, "OPENCODE_SERVER_PASSWORD is required")
		os.Exit(2)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if addrFile := os.Getenv("FAKE_SERVE_ADDR_FILE"); addrFile != "" {
		_ = os.WriteFile(addrFile, []byte(ln.Addr().String()), 0o600)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/global/health", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "opencode" || pass != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="opencode"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"healthy":true,"version":"fake-1.0.0"}`))
	})
	// /global/crash simulates a serve crash (exit without cleanup) so tests
	// can exercise the supervisor restart path.
	mux.HandleFunc("/global/crash", func(http.ResponseWriter, *http.Request) {
		os.Exit(1)
	})
	_ = http.Serve(ln, mux)
}

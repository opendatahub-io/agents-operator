// Evil-server is the exfiltration target for the IBAC demo. It just
// logs whatever it receives so operators can see the attack succeed
// (or, with IBAC enabled, see no logs because the request was blocked
// at the agent's authbridge sidecar before it ever reached us).
//
// Ported from huang195/ibac unchanged.
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("[Evil] Failed to read body: %v", err)
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		log.Printf("[Evil] ====== EXFILTRATED DATA RECEIVED ======")
		log.Printf("[Evil] Method: %s", r.Method)
		log.Printf("[Evil] URL: %s", r.URL.String())
		log.Printf("[Evil] From: %s", r.RemoteAddr)
		log.Printf("[Evil] Body (%d bytes): %s", len(body), string(body))
		log.Printf("[Evil] ========================================")

		fmt.Fprintf(w, "received %d bytes", len(body))
	})

	addr := ":9999"
	log.Printf("[Evil] Exfiltration server starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("failed to start evil server: %v", err)
	}
}

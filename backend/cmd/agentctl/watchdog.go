package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"runtime/pprof"
	"time"
)

// Timings of the stall watchdog, variables so tests can shorten them.
var (
	stallEvery   = 15 * time.Second // how often the service asks itself for the state
	stallAfter   = 10 * time.Second // no answer by then: it is stalled
	stallDumpGap = 30 * time.Minute // at most one stack dump in this long
)

// watchStalls asks the service, over its own socket, for the state the deck
// shows (GET /v1/state), as the app and agentctl do. When that takes longer
// than stallAfter, the log gets every goroutine's stack (what is holding it
// up) and, once it answers again, how long it was stuck.
func watchStalls(sock string) {
	c := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
		DisableKeepAlives: true,
	}}
	var stalledSince, lastDump time.Time
	for {
		time.Sleep(stallEvery)
		ok := askState(c)
		switch {
		case !ok && stalledSince.IsZero():
			stalledSince = time.Now().Add(-stallAfter)
			if time.Since(lastDump) >= stallDumpGap {
				lastDump = time.Now()
				log.Printf("stalled: no answer to GET /v1/state in %s; what every goroutine is doing:", stallAfter)
				_ = pprof.Lookup("goroutine").WriteTo(log.Writer(), 2)
				log.Printf("stalled: end of the goroutine stacks")
			} else {
				log.Printf("stalled: no answer to GET /v1/state in %s (stacks were logged at %s)", stallAfter, lastDump.Format("15:04:05"))
			}
		case ok && !stalledSince.IsZero():
			log.Printf("answering again after %s", time.Since(stalledSince).Round(time.Second))
			stalledSince = time.Time{}
		}
	}
}

// askState: GET /v1/state answered within stallAfter.
func askState(c *http.Client) bool {
	ctx, cancel := context.WithTimeout(context.Background(), stallAfter)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://agentctl/v1/state", nil)
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode < 500
}

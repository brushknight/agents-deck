// Package pricing estimates spend from token counts using public list prices
// (USD per million tokens). Estimates only: plans and discounts are ignored.
package pricing

import (
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

type rate struct {
	label         string
	in, out, read float64
	window        int64
	match         string
}

// Most specific first. Cache writes are billed at 1.25× input (5m TTL); the
// transcript does not always split 5m/1h, so 1h writes are slightly under-estimated.
var rates = []rate{
	{"opus 5.5", 4, 20, 0.20, 1_000_000, "opus-5-5"},
	{"opus 5", 5, 25, 0.50, 1_000_000, "opus-5"},
	{"opus 4.8", 5, 25, 0.50, 1_000_000, "opus-4-8"},
	{"opus 4.7", 5, 25, 0.50, 1_000_000, "opus-4-7"},
	{"opus 4.6", 5, 25, 0.50, 1_000_000, "opus-4-6"},
	{"fable 5.1", 10, 50, 0.25, 1_000_000, "fable-5-1"},
	{"fable 5", 10, 50, 1.00, 1_000_000, "fable-5"},
	{"sonnet 5", 2, 10, 0.20, 1_000_000, "sonnet-5"},
	{"sonnet 4.6", 3, 15, 0.30, 1_000_000, "sonnet-4-6"},
	{"haiku 4.5", 1, 5, 0.10, 200_000, "haiku-4-5"},
}

func lookup(modelID string) (rate, bool) {
	for _, r := range rates {
		if strings.Contains(modelID, r.match) {
			return r, true
		}
	}
	return rate{}, false
}

// Label is a short display name ("opus 5.5"); falls back to the raw id.
func Label(modelID string) string {
	if r, ok := lookup(modelID); ok {
		return r.label
	}
	return strings.TrimPrefix(modelID, "claude-")
}

// Window is the model's context window, 200k when unknown.
func Window(modelID string) int64 {
	if r, ok := lookup(modelID); ok {
		return r.window
	}
	return 200_000
}

// Cost of one request's usage on the given model.
func Cost(modelID string, t model.Tokens) float64 {
	r, ok := lookup(modelID)
	if !ok {
		return 0
	}
	const m = 1_000_000.0
	return float64(t.Input)*r.in/m + float64(t.Output)*r.out/m +
		float64(t.CacheRead)*r.read/m + float64(t.CacheWrite)*r.in*1.25/m
}

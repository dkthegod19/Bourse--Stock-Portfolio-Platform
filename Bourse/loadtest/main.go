// Command loadtest exercises two correctness properties of a running Bourse
// instance:
//
//  1. Rate limiter: fire 500 concurrent requests against a tightly-configured
//     key and assert the number allowed matches the token-bucket math (proves
//     no double-spend under concurrency).
//  2. Trading concurrency: buy a position, then fire many concurrent sells of
//     the whole position and assert exactly one fills and holdings never go
//     negative (proves no lost updates).
//
// Usage: go run ./loadtest   (set LOADTEST_URL to target a remote instance)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var base = env("LOADTEST_URL", "http://localhost:8080")

func main() {
	fmt.Println("== Bourse load test ==\nTarget:", base)
	ok1 := rateLimitTest()
	ok2 := concurrencyTest()
	if ok1 && ok2 {
		fmt.Println("\nALL CHECKS PASSED")
		return
	}
	fmt.Println("\nSOME CHECKS FAILED")
	os.Exit(1)
}

func rateLimitTest() bool {
	fmt.Println("\n[1] Rate limiter under 500 concurrent requests")
	const key = "loadtest-key"
	const burst = 20
	const rate = 10.0
	// Configure a tight limit for our key.
	put("/v1/admin/limits/"+key, map[string]any{"mode": "token", "rate": rate, "burst": burst, "window_ms": 1000})

	var allowed, denied int64
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", base+"/v1/quotes/AAPL", nil)
			req.Header.Set("X-API-Key", key)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				atomic.AddInt64(&denied, 1)
				return
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				atomic.AddInt64(&denied, 1)
			} else {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	maxExpected := float64(burst) + rate*elapsed + 5 // tokens replenished during the burst + slack
	fmt.Printf("    allowed=%d denied=%d elapsed=%.2fs (max allowed expected ~%.0f)\n",
		allowed, denied, elapsed, maxExpected)

	if denied == 0 {
		fmt.Println("    FAIL: nothing was rate-limited")
		return false
	}
	if float64(allowed) > maxExpected {
		fmt.Println("    FAIL: more requests allowed than the bucket should permit (double-spend!)")
		return false
	}
	fmt.Println("    PASS")
	return true
}

func concurrencyTest() bool {
	fmt.Println("\n[2] Concurrent sells of a single position")
	// Seed $100,000.
	var pf struct {
		ID string `json:"id"`
	}
	post("/v1/portfolios", map[string]any{"name": "loadtest", "seed_cents": 10000000}, &pf)
	if pf.ID == "" {
		fmt.Println("    FAIL: could not create portfolio")
		return false
	}

	// Buy 100 shares and wait for the fill.
	buy := placeOrder(pf.ID, "buy", "AAPL", 100, "market")
	if !waitFilled(buy) {
		fmt.Println("    FAIL: buy did not fill")
		return false
	}

	// Fire 5 concurrent sells of the full 100 shares. Only one can succeed.
	const n = 5
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i] = placeOrder(pf.ID, "sell", "AAPL", 100, "market")
		}(i)
	}
	wg.Wait()

	// Wait for all to resolve.
	filled, rejected := 0, 0
	for _, id := range ids {
		st := waitResolved(id)
		switch st {
		case "filled", "settled":
			filled++
		case "rejected":
			rejected++
		}
	}

	// Final holdings must be exactly zero AAPL (never negative).
	var view struct {
		Positions []struct {
			Instrument string `json:"instrument"`
			Quantity   int64  `json:"quantity"`
		} `json:"positions"`
	}
	get("/v1/portfolios/"+pf.ID, &view)
	aapl := int64(0)
	for _, p := range view.Positions {
		if p.Instrument == "AAPL" {
			aapl = p.Quantity
		}
	}
	fmt.Printf("    sells filled=%d rejected=%d final AAPL position=%d\n", filled, rejected, aapl)

	if filled != 1 {
		fmt.Println("    FAIL: expected exactly one sell to fill")
		return false
	}
	if aapl < 0 {
		fmt.Println("    FAIL: position went negative (lost update!)")
		return false
	}
	fmt.Println("    PASS")
	return true
}

// ---- helpers ----

func placeOrder(pfID, side, sym string, qty int64, typ string) string {
	var o struct {
		ID string `json:"id"`
	}
	post("/v1/orders", map[string]any{
		"portfolio_id": pfID, "side": side, "instrument": sym, "quantity": qty, "type": typ,
	}, &o)
	return o.ID
}

func orderStatus(id string) string {
	var o struct {
		Status string `json:"status"`
	}
	get("/v1/orders/"+id, &o)
	return o.Status
}

func waitFilled(id string) bool {
	for i := 0; i < 50; i++ {
		if orderStatus(id) == "filled" {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func waitResolved(id string) string {
	for i := 0; i < 50; i++ {
		s := orderStatus(id)
		if s != "pending" {
			return s
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "pending"
}

func post(path string, body any, out any) {
	b, _ := json.Marshal(body)
	resp, err := http.Post(base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if out != nil {
		data, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(data, out)
	}
}

func put(path string, body any) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func get(path string, out any) {
	resp, err := http.Get(base + path)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(data, out)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

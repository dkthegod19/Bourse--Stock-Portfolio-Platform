package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"net/url"
	"time"
)

// Provider fetches the current price of a symbol in cents.
type Provider interface {
	Quote(ctx context.Context, symbol string) (int64, error)
}

// New returns a Provider based on configuration. "finnhub" needs an API key;
// anything else falls back to the deterministic stub so the project runs fully
// offline.
func New(provider, apiKey string) Provider {
	if provider == "finnhub" && apiKey != "" {
		return &finnhub{apiKey: apiKey, client: &http.Client{Timeout: 8 * time.Second}}
	}
	return &stub{}
}

// stub generates a deterministic-but-moving price per symbol so demos and tests
// work without any external dependency or API key.
type stub struct{}

func (s *stub) Quote(_ context.Context, symbol string) (int64, error) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(symbol))
	base := float64(20+h.Sum32()%480) // $20–$500 base, stable per symbol
	// gentle sine wave over ~5 minutes so prices visibly move
	t := float64(time.Now().Unix())
	wave := 1 + 0.05*math.Sin(t/47.0+float64(h.Sum32()%10))
	price := base * wave
	return int64(price * 100), nil // cents
}

// finnhub calls the free Finnhub quote endpoint.
type finnhub struct {
	apiKey string
	client *http.Client
}

func (f *finnhub) Quote(ctx context.Context, symbol string) (int64, error) {
	u := fmt.Sprintf("https://finnhub.io/api/v1/quote?symbol=%s&token=%s",
		url.QueryEscape(symbol), url.QueryEscape(f.apiKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("finnhub status %d", resp.StatusCode)
	}
	var body struct {
		C float64 `json:"c"` // current price
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	if body.C <= 0 {
		return 0, fmt.Errorf("finnhub: no price for %s", symbol)
	}
	return int64(body.C * 100), nil
}

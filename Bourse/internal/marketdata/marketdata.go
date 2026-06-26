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

// Stock is a tradable instrument on an Indian exchange. BasePaise is the
// reference price (treated as the previous close) in paise; ₹1 = 100 paise.
type Stock struct {
	Symbol    string `json:"symbol"`
	Name      string `json:"name"`
	Sector    string `json:"sector"`
	Exchange  string `json:"exchange"`
	BasePaise int64  `json:"-"`
}

// nseUniverse is a curated set of real NSE large-caps. Prices are realistic
// reference levels in paise so the project runs fully offline with believable
// data. The literals use the form <rupees>_00 (digit separator) so each base is
// readable as "rupees, then paise". This is the single source of truth for what
// can be listed and traded.
var nseUniverse = []Stock{
	{"RELIANCE", "Reliance Industries", "Energy", "NSE", 2950_00},
	{"TCS", "Tata Consultancy Services", "IT", "NSE", 3850_00},
	{"INFY", "Infosys", "IT", "NSE", 1650_00},
	{"HDFCBANK", "HDFC Bank", "Banking", "NSE", 1700_00},
	{"ICICIBANK", "ICICI Bank", "Banking", "NSE", 1250_00},
	{"SBIN", "State Bank of India", "Banking", "NSE", 820_00},
	{"HINDUNILVR", "Hindustan Unilever", "FMCG", "NSE", 2450_00},
	{"ITC", "ITC", "FMCG", "NSE", 440_00},
	{"BHARTIARTL", "Bharti Airtel", "Telecom", "NSE", 1450_00},
	{"LT", "Larsen & Toubro", "Infrastructure", "NSE", 3650_00},
	{"KOTAKBANK", "Kotak Mahindra Bank", "Banking", "NSE", 1780_00},
	{"AXISBANK", "Axis Bank", "Banking", "NSE", 1180_00},
	{"BAJFINANCE", "Bajaj Finance", "NBFC", "NSE", 6900_00},
	{"ASIANPAINT", "Asian Paints", "Consumer", "NSE", 2900_00},
	{"MARUTI", "Maruti Suzuki", "Automobile", "NSE", 12800_00},
	{"TITAN", "Titan Company", "Consumer", "NSE", 3400_00},
	{"SUNPHARMA", "Sun Pharmaceutical", "Pharma", "NSE", 1650_00},
	{"WIPRO", "Wipro", "IT", "NSE", 480_00},
	{"TATAMOTORS", "Tata Motors", "Automobile", "NSE", 980_00},
	{"ADANIENT", "Adani Enterprises", "Conglomerate", "NSE", 3100_00},
	{"HCLTECH", "HCL Technologies", "IT", "NSE", 1550_00},
	{"NESTLEIND", "Nestlé India", "FMCG", "NSE", 2500_00},
	{"ULTRACEMCO", "UltraTech Cement", "Cement", "NSE", 11200_00},
	{"POWERGRID", "Power Grid Corporation", "Power", "NSE", 320_00},
	{"NTPC", "NTPC", "Power", "NSE", 360_00},
}

var universeBySymbol = func() map[string]Stock {
	m := make(map[string]Stock, len(nseUniverse))
	for _, s := range nseUniverse {
		m[s.Symbol] = s
	}
	return m
}()

// Provider fetches the current price of a symbol in paise and knows the
// tradable universe.
type Provider interface {
	Quote(ctx context.Context, symbol string) (int64, error)
	Universe() []Stock
	// PrevClose returns the reference (previous-close) price in paise so callers
	// can compute the day-change. Returns false for unknown symbols.
	PrevClose(symbol string) (int64, bool)
}

// New returns a Provider based on configuration. "finnhub" needs an API key and
// fetches live NSE quotes; anything else falls back to the deterministic stub so
// the project runs fully offline with realistic Indian-market data.
func New(provider, apiKey string) Provider {
	if provider == "finnhub" && apiKey != "" {
		return &finnhub{apiKey: apiKey, client: &http.Client{Timeout: 8 * time.Second}}
	}
	return &stub{}
}

// stub generates a deterministic-but-moving intraday price per symbol, oscillating
// around each stock's reference close so day-change percentages (and therefore
// "trending" rankings) shift over time without any external dependency.
type stub struct{}

func (s *stub) Universe() []Stock { return nseUniverse }

func (s *stub) PrevClose(symbol string) (int64, bool) {
	st, ok := universeBySymbol[symbol]
	if !ok {
		return 0, false
	}
	return st.BasePaise, true
}

func (s *stub) Quote(_ context.Context, symbol string) (int64, error) {
	st, ok := universeBySymbol[symbol]
	if !ok {
		// Unknown symbol: synthesize a stable price so arbitrary tickers still work.
		h := fnv.New32a()
		_, _ = h.Write([]byte(symbol))
		base := float64(10000 + h.Sum32()%500000) // ₹100–₹5000 in paise
		return int64(base), nil
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(symbol))
	seed := float64(h.Sum32() % 1000)
	// Each stock gets its own amplitude (up to ~3.5%) and phase so the leaderboard
	// of top movers reshuffles through the session.
	amp := 0.01 + 0.025*float64(h.Sum32()%100)/100.0
	t := float64(time.Now().Unix())
	wave := math.Sin(t/90.0 + seed)
	price := float64(st.BasePaise) * (1 + amp*wave)
	return int64(price), nil
}

// finnhub calls the free Finnhub quote endpoint. NSE symbols are suffixed ".NS".
type finnhub struct {
	apiKey string
	client *http.Client
}

func (f *finnhub) Universe() []Stock { return nseUniverse }

func (f *finnhub) PrevClose(symbol string) (int64, bool) {
	st, ok := universeBySymbol[symbol]
	if !ok {
		return 0, false
	}
	return st.BasePaise, true
}

func (f *finnhub) Quote(ctx context.Context, symbol string) (int64, error) {
	u := fmt.Sprintf("https://finnhub.io/api/v1/quote?symbol=%s&token=%s",
		url.QueryEscape(symbol+".NS"), url.QueryEscape(f.apiKey))
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
		C float64 `json:"c"` // current price (rupees)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	if body.C <= 0 {
		// Fall back to the reference price if the upstream has no live data
		// (e.g. market closed or symbol unsupported on the free tier).
		if base, ok := f.PrevClose(symbol); ok {
			return base, nil
		}
		return 0, fmt.Errorf("finnhub: no price for %s", symbol)
	}
	return int64(body.C * 100), nil // rupees -> paise
}

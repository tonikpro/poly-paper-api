package trading

import (
	"io"
	"net/http"
	"strings"
	"time"

	clob "github.com/tonikpro/poly-paper-api/api/generated/clob"
)

// ProxyHandler proxies public CLOB endpoints to the real Polymarket API.
type ProxyHandler struct {
	clobURL    string
	httpClient *http.Client
}

func NewProxyHandler(clobURL string) *ProxyHandler {
	return &ProxyHandler{
		clobURL: clobURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (h *ProxyHandler) proxy(w http.ResponseWriter, r *http.Request) {
	// Build target URL: strip /clob prefix
	path := strings.TrimPrefix(r.URL.Path, "/clob")
	targetURL := h.clobURL + path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, `{"error":"proxy error"}`, http.StatusBadGateway)
		return
	}
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, `{"error":"upstream unavailable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// All public endpoints delegate to the proxy

func (h *ProxyHandler) GetOrderBook(w http.ResponseWriter, r *http.Request, params clob.GetOrderBookParams) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetOrderBooks(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetMarkets(w http.ResponseWriter, r *http.Request, params clob.GetMarketsParams) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetMarket(w http.ResponseWriter, r *http.Request, marketId string) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetMidpoint(w http.ResponseWriter, r *http.Request, params clob.GetMidpointParams) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetMidpoints(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetPrice(w http.ResponseWriter, r *http.Request, params clob.GetPriceParams) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetPrices(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetSpread(w http.ResponseWriter, r *http.Request, params clob.GetSpreadParams) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetSpreads(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetTickSize(w http.ResponseWriter, r *http.Request, params clob.GetTickSizeParams) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetNegRisk(w http.ResponseWriter, r *http.Request, params clob.GetNegRiskParams) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetLastTradePrice(w http.ResponseWriter, r *http.Request, params clob.GetLastTradePriceParams) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetLastTradesPrices(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetLiveActivityEvents(w http.ResponseWriter, r *http.Request, eventId string) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetSamplingMarkets(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetSamplingSimplifiedMarkets(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetSimplifiedMarkets(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r)
}

func (h *ProxyHandler) GetServerTime(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r)
}

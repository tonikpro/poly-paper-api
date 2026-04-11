package server

import (
	"net/http"

	"github.com/tonikpro/poly-paper-api/internal/auth"
	"github.com/tonikpro/poly-paper-api/internal/trading"

	clob "github.com/tonikpro/poly-paper-api/api/generated/clob"
)

// CLOBServer satisfies the full clob.ServerInterface.
// It composes auth, trading, and proxy handlers.
type CLOBServer struct {
	clob.Unimplemented
	auth    *auth.CLOBAuthHandler
	trading *trading.CLOBTradingHandler
	proxy   *trading.ProxyHandler
}

func NewCLOBServer(authHandler *auth.CLOBAuthHandler, tradingHandler *trading.CLOBTradingHandler, proxyHandler *trading.ProxyHandler) *CLOBServer {
	return &CLOBServer{
		auth:    authHandler,
		trading: tradingHandler,
		proxy:   proxyHandler,
	}
}

// Compile-time check
var _ clob.ServerInterface = (*CLOBServer)(nil)

// --- Health ---

func (s *CLOBServer) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// --- Auth endpoints ---

func (s *CLOBServer) CreateApiKey(w http.ResponseWriter, r *http.Request, params clob.CreateApiKeyParams) {
	s.auth.CreateApiKey(w, r, params)
}

func (s *CLOBServer) DeriveApiKey(w http.ResponseWriter, r *http.Request, params clob.DeriveApiKeyParams) {
	s.auth.DeriveApiKey(w, r, params)
}

func (s *CLOBServer) GetApiKeys(w http.ResponseWriter, r *http.Request, params clob.GetApiKeysParams) {
	s.auth.GetApiKeys(w, r, params)
}

func (s *CLOBServer) DeleteApiKey(w http.ResponseWriter, r *http.Request, params clob.DeleteApiKeyParams) {
	s.auth.DeleteApiKey(w, r, params)
}

// --- Trading endpoints ---

func (s *CLOBServer) PostOrder(w http.ResponseWriter, r *http.Request, params clob.PostOrderParams) {
	s.trading.PostOrder(w, r, params)
}

func (s *CLOBServer) PostOrders(w http.ResponseWriter, r *http.Request, params clob.PostOrdersParams) {
	s.trading.PostOrders(w, r, params)
}

func (s *CLOBServer) CancelOrder(w http.ResponseWriter, r *http.Request, params clob.CancelOrderParams) {
	s.trading.CancelOrder(w, r, params)
}

func (s *CLOBServer) CancelOrders(w http.ResponseWriter, r *http.Request, params clob.CancelOrdersParams) {
	s.trading.CancelOrders(w, r, params)
}

func (s *CLOBServer) CancelAll(w http.ResponseWriter, r *http.Request, params clob.CancelAllParams) {
	s.trading.CancelAll(w, r, params)
}

func (s *CLOBServer) CancelMarketOrders(w http.ResponseWriter, r *http.Request, params clob.CancelMarketOrdersParams) {
	s.trading.CancelMarketOrders(w, r, params)
}

func (s *CLOBServer) GetOrder(w http.ResponseWriter, r *http.Request, orderId string, params clob.GetOrderParams) {
	s.trading.GetOrder(w, r, orderId, params)
}

func (s *CLOBServer) GetOrders(w http.ResponseWriter, r *http.Request, params clob.GetOrdersParams) {
	s.trading.GetOrders(w, r, params)
}

func (s *CLOBServer) GetTrades(w http.ResponseWriter, r *http.Request, params clob.GetTradesParams) {
	s.trading.GetTrades(w, r, params)
}

func (s *CLOBServer) GetBalanceAllowance(w http.ResponseWriter, r *http.Request, params clob.GetBalanceAllowanceParams) {
	s.trading.GetBalanceAllowance(w, r, params)
}

func (s *CLOBServer) UpdateBalanceAllowance(w http.ResponseWriter, r *http.Request, params clob.UpdateBalanceAllowanceParams) {
	s.trading.UpdateBalanceAllowance(w, r, params)
}

func (s *CLOBServer) GetNotifications(w http.ResponseWriter, r *http.Request, params clob.GetNotificationsParams) {
	s.trading.GetNotifications(w, r, params)
}

func (s *CLOBServer) DropNotifications(w http.ResponseWriter, r *http.Request, params clob.DropNotificationsParams) {
	s.trading.DropNotifications(w, r, params)
}

func (s *CLOBServer) IsOrderScoring(w http.ResponseWriter, r *http.Request, params clob.IsOrderScoringParams) {
	s.trading.IsOrderScoring(w, r, params)
}

func (s *CLOBServer) AreOrdersScoring(w http.ResponseWriter, r *http.Request, params clob.AreOrdersScoringParams) {
	s.trading.AreOrdersScoring(w, r, params)
}

func (s *CLOBServer) PostHeartbeat(w http.ResponseWriter, r *http.Request, params clob.PostHeartbeatParams) {
	s.trading.PostHeartbeat(w, r, params)
}

func (s *CLOBServer) GetFeeRate(w http.ResponseWriter, r *http.Request) {
	s.trading.GetFeeRate(w, r)
}

// --- Proxy endpoints (public market data) ---

func (s *CLOBServer) GetOrderBook(w http.ResponseWriter, r *http.Request, params clob.GetOrderBookParams) {
	s.proxy.GetOrderBook(w, r, params)
}

func (s *CLOBServer) GetOrderBooks(w http.ResponseWriter, r *http.Request) {
	s.proxy.GetOrderBooks(w, r)
}

func (s *CLOBServer) GetMarkets(w http.ResponseWriter, r *http.Request, params clob.GetMarketsParams) {
	s.proxy.GetMarkets(w, r, params)
}

func (s *CLOBServer) GetMarket(w http.ResponseWriter, r *http.Request, marketId string) {
	s.proxy.GetMarket(w, r, marketId)
}

func (s *CLOBServer) GetMidpoint(w http.ResponseWriter, r *http.Request, params clob.GetMidpointParams) {
	s.proxy.GetMidpoint(w, r, params)
}

func (s *CLOBServer) GetMidpoints(w http.ResponseWriter, r *http.Request) {
	s.proxy.GetMidpoints(w, r)
}

func (s *CLOBServer) GetPrice(w http.ResponseWriter, r *http.Request, params clob.GetPriceParams) {
	s.proxy.GetPrice(w, r, params)
}

func (s *CLOBServer) GetPrices(w http.ResponseWriter, r *http.Request) {
	s.proxy.GetPrices(w, r)
}

func (s *CLOBServer) GetSpread(w http.ResponseWriter, r *http.Request, params clob.GetSpreadParams) {
	s.proxy.GetSpread(w, r, params)
}

func (s *CLOBServer) GetSpreads(w http.ResponseWriter, r *http.Request) {
	s.proxy.GetSpreads(w, r)
}

func (s *CLOBServer) GetTickSize(w http.ResponseWriter, r *http.Request, params clob.GetTickSizeParams) {
	s.proxy.GetTickSize(w, r, params)
}

func (s *CLOBServer) GetNegRisk(w http.ResponseWriter, r *http.Request, params clob.GetNegRiskParams) {
	s.proxy.GetNegRisk(w, r, params)
}

func (s *CLOBServer) GetLastTradePrice(w http.ResponseWriter, r *http.Request, params clob.GetLastTradePriceParams) {
	s.proxy.GetLastTradePrice(w, r, params)
}

func (s *CLOBServer) GetLastTradesPrices(w http.ResponseWriter, r *http.Request) {
	s.proxy.GetLastTradesPrices(w, r)
}

func (s *CLOBServer) GetLiveActivityEvents(w http.ResponseWriter, r *http.Request, eventId string) {
	s.proxy.GetLiveActivityEvents(w, r, eventId)
}

func (s *CLOBServer) GetSamplingMarkets(w http.ResponseWriter, r *http.Request) {
	s.proxy.GetSamplingMarkets(w, r)
}

func (s *CLOBServer) GetSamplingSimplifiedMarkets(w http.ResponseWriter, r *http.Request) {
	s.proxy.GetSamplingSimplifiedMarkets(w, r)
}

func (s *CLOBServer) GetSimplifiedMarkets(w http.ResponseWriter, r *http.Request) {
	s.proxy.GetSimplifiedMarkets(w, r)
}

func (s *CLOBServer) GetServerTime(w http.ResponseWriter, r *http.Request) {
	s.proxy.GetServerTime(w, r)
}

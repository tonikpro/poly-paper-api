package trading

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/tonikpro/poly-paper-api/internal/auth"
	"github.com/tonikpro/poly-paper-api/internal/models"

	clob "github.com/tonikpro/poly-paper-api/api/generated/clob"
)

// CLOBTradingHandler handles CLOB trading endpoints (orders, positions, trades, balance).
type CLOBTradingHandler struct {
	svc *Service
}

func NewCLOBTradingHandler(svc *Service) *CLOBTradingHandler {
	return &CLOBTradingHandler{svc: svc}
}

// PostOrder handles POST /order
func (h *CLOBTradingHandler) PostOrder(w http.ResponseWriter, r *http.Request, params clob.PostOrderParams) {
	userID := auth.GetUserID(r.Context())

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, clob.ErrorResponse{Error: ptr("failed to read body")})
		return
	}
	slog.Debug("POST /order raw body", "body", string(bodyBytes), "len", len(bodyBytes))

	var req models.PostOrderRequest
	if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&req); err != nil {
		slog.Error("POST /order decode failed", "error", err, "body", string(bodyBytes))
		writeJSON(w, http.StatusBadRequest, clob.ErrorResponse{Error: ptr("invalid request body")})
		return
	}

	resp, err := h.svc.PlaceOrder(r.Context(), userID, &req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, clob.ErrorResponse{Error: ptr(err.Error())})
		return
	}

	writeJSON(w, http.StatusOK, clob.OrderResponse{
		Success:            &resp.Success,
		ErrorMsg:           &resp.ErrorMsg,
		OrderID:            &resp.OrderID,
		Status:             &resp.Status,
		TakingAmount:       &resp.TakingAmount,
		MakingAmount:       &resp.MakingAmount,
		TransactionsHashes: &resp.TransactionsHashes,
	})
}

// PostOrders handles POST /orders (batch)
func (h *CLOBTradingHandler) PostOrders(w http.ResponseWriter, r *http.Request, params clob.PostOrdersParams) {
	userID := auth.GetUserID(r.Context())

	var reqs []models.PostOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		writeJSON(w, http.StatusBadRequest, clob.ErrorResponse{Error: ptr("invalid request body")})
		return
	}

	var results []clob.OrderResponse
	for _, req := range reqs {
		resp, err := h.svc.PlaceOrder(r.Context(), userID, &req)
		if err != nil {
			errMsg := err.Error()
			results = append(results, clob.OrderResponse{
				Success:  boolPtr(false),
				ErrorMsg: &errMsg,
			})
			continue
		}
		results = append(results, clob.OrderResponse{
			Success:            &resp.Success,
			ErrorMsg:           &resp.ErrorMsg,
			OrderID:            &resp.OrderID,
			Status:             &resp.Status,
			TakingAmount:       &resp.TakingAmount,
			MakingAmount:       &resp.MakingAmount,
			TransactionsHashes: &resp.TransactionsHashes,
		})
	}

	writeJSON(w, http.StatusOK, results)
}

// CancelOrder handles DELETE /order
func (h *CLOBTradingHandler) CancelOrder(w http.ResponseWriter, r *http.Request, params clob.CancelOrderParams) {
	userID := auth.GetUserID(r.Context())

	var req models.CancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, clob.ErrorResponse{Error: ptr("invalid request body")})
		return
	}

	resp, err := h.svc.CancelOrder(r.Context(), userID, req.OrderID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, clob.ErrorResponse{Error: ptr(err.Error())})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// CancelOrders handles DELETE /orders (batch)
func (h *CLOBTradingHandler) CancelOrders(w http.ResponseWriter, r *http.Request, params clob.CancelOrdersParams) {
	userID := auth.GetUserID(r.Context())

	var orderIDs []string
	if err := json.NewDecoder(r.Body).Decode(&orderIDs); err != nil {
		writeJSON(w, http.StatusBadRequest, clob.ErrorResponse{Error: ptr("invalid request body")})
		return
	}

	resp, err := h.svc.CancelOrders(r.Context(), userID, orderIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, clob.ErrorResponse{Error: ptr(err.Error())})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// CancelAll handles DELETE /cancel-all
func (h *CLOBTradingHandler) CancelAll(w http.ResponseWriter, r *http.Request, params clob.CancelAllParams) {
	userID := auth.GetUserID(r.Context())

	resp, err := h.svc.CancelAll(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, clob.ErrorResponse{Error: ptr(err.Error())})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// CancelMarketOrders handles DELETE /cancel-market-orders
func (h *CLOBTradingHandler) CancelMarketOrders(w http.ResponseWriter, r *http.Request, params clob.CancelMarketOrdersParams) {
	userID := auth.GetUserID(r.Context())

	var req struct {
		Market  *string `json:"market"`
		AssetID *string `json:"asset_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, clob.ErrorResponse{Error: ptr("invalid request body")})
		return
	}

	resp, err := h.svc.CancelMarketOrders(r.Context(), userID, req.Market, req.AssetID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, clob.ErrorResponse{Error: ptr(err.Error())})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// GetOrder handles GET /data/order/{orderId}
func (h *CLOBTradingHandler) GetOrder(w http.ResponseWriter, r *http.Request, orderId string, params clob.GetOrderParams) {
	order, err := h.svc.GetOrder(r.Context(), orderId)
	if err != nil || order == nil {
		writeJSON(w, http.StatusNotFound, clob.ErrorResponse{Error: ptr("order not found")})
		return
	}

	writeJSON(w, http.StatusOK, orderToOpenOrder(order))
}

// GetOrders handles GET /data/orders
func (h *CLOBTradingHandler) GetOrders(w http.ResponseWriter, r *http.Request, params clob.GetOrdersParams) {
	userID := auth.GetUserID(r.Context())

	orders, nextCursor, err := h.svc.GetOrders(r.Context(), userID, params.Market, params.AssetId, params.NextCursor)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, clob.ErrorResponse{Error: ptr(err.Error())})
		return
	}

	openOrders := make([]clob.OpenOrder, 0, len(orders))
	for _, o := range orders {
		openOrders = append(openOrders, orderToOpenOrder(o))
	}

	writeJSON(w, http.StatusOK, clob.PaginatedOrders{
		Data:       &openOrders,
		NextCursor: &nextCursor,
	})
}

// GetTrades handles GET /data/trades
func (h *CLOBTradingHandler) GetTrades(w http.ResponseWriter, r *http.Request, params clob.GetTradesParams) {
	userID := auth.GetUserID(r.Context())

	trades, nextCursor, err := h.svc.GetTrades(r.Context(), userID, params.Market, params.AssetId, params.NextCursor)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, clob.ErrorResponse{Error: ptr(err.Error())})
		return
	}

	tradeItems := make([]clob.TradeItem, 0, len(trades))
	for _, t := range trades {
		tradeItems = append(tradeItems, tradeToTradeItem(&t))
	}

	writeJSON(w, http.StatusOK, clob.PaginatedTrades{
		Data:       &tradeItems,
		NextCursor: &nextCursor,
	})
}

// GetBalanceAllowance handles GET /balance-allowance
func (h *CLOBTradingHandler) GetBalanceAllowance(w http.ResponseWriter, r *http.Request, params clob.GetBalanceAllowanceParams) {
	userID := auth.GetUserID(r.Context())

	assetType := "COLLATERAL"
	tokenID := ""
	if params.AssetType != nil {
		assetType = string(*params.AssetType)
	}
	if params.TokenId != nil {
		tokenID = *params.TokenId
	}

	resp, err := h.svc.GetBalanceAllowance(r.Context(), userID, assetType, tokenID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, clob.ErrorResponse{Error: ptr(err.Error())})
		return
	}

	writeJSON(w, http.StatusOK, clob.BalanceAllowanceResponse{
		Balance:   &resp.Balance,
		Allowance: &resp.Allowance,
	})
}

// UpdateBalanceAllowance handles GET /balance-allowance/update
func (h *CLOBTradingHandler) UpdateBalanceAllowance(w http.ResponseWriter, r *http.Request, params clob.UpdateBalanceAllowanceParams) {
	// For paper trading, allowance always equals balance — just return current state
	h.GetBalanceAllowance(w, r, clob.GetBalanceAllowanceParams{
		POLYADDRESS:    params.POLYADDRESS,
		POLYSIGNATURE:  params.POLYSIGNATURE,
		POLYTIMESTAMP:  params.POLYTIMESTAMP,
		POLYAPIKEY:     params.POLYAPIKEY,
		POLYPASSPHRASE: params.POLYPASSPHRASE,
	})
}

// Stub endpoints that return sensible defaults

func (h *CLOBTradingHandler) GetNotifications(w http.ResponseWriter, r *http.Request, params clob.GetNotificationsParams) {
	writeJSON(w, http.StatusOK, []any{})
}

func (h *CLOBTradingHandler) DropNotifications(w http.ResponseWriter, r *http.Request, params clob.DropNotificationsParams) {
	w.WriteHeader(http.StatusOK)
}

func (h *CLOBTradingHandler) IsOrderScoring(w http.ResponseWriter, r *http.Request, params clob.IsOrderScoringParams) {
	writeJSON(w, http.StatusOK, map[string]bool{"scoring": false})
}

func (h *CLOBTradingHandler) AreOrdersScoring(w http.ResponseWriter, r *http.Request, params clob.AreOrdersScoringParams) {
	writeJSON(w, http.StatusOK, map[string]bool{"scoring": false})
}

func (h *CLOBTradingHandler) PostHeartbeat(w http.ResponseWriter, r *http.Request, params clob.PostHeartbeatParams) {
	const heartbeatID = "paper-heartbeat"
	writeJSON(w, http.StatusOK, clob.HeartbeatResponse{HeartbeatId: heartbeatID})
}

// --- Helpers ---

func orderToOpenOrder(o *models.Order) clob.OpenOrder {
	createdAt := o.CreatedAt.Format("2006-01-02T15:04:05Z")
	return clob.OpenOrder{
		Id:              &o.ID,
		Status:          &o.Status,
		Owner:           &o.Owner,
		MakerAddress:    &o.Maker,
		Market:          &o.Market,
		AssetId:         &o.AssetID,
		Side:            &o.Side,
		OriginalSize:    &o.OriginalSize,
		SizeMatched:     &o.SizeMatched,
		Price:           &o.Price,
		AssociateTrades: &[]string{},
		Outcome:         &o.Outcome,
		CreatedAt:       &createdAt,
		Expiration:      &o.Expiration,
		OrderType:       &o.OrderType,
	}
}

func tradeToTradeItem(t *models.Trade) clob.TradeItem {
	return clob.TradeItem{
		Id:              &t.ID,
		TakerOrderId:    &t.TakerOrderID,
		Market:          &t.Market,
		AssetId:         &t.AssetID,
		Side:            &t.Side,
		Size:            &t.Size,
		FeeRateBps:      &t.FeeRateBps,
		Price:           &t.Price,
		Status:          &t.Status,
		MatchTime:       &t.MatchTime,
		LastUpdate:      &t.LastUpdate,
		Outcome:         &t.Outcome,
		Owner:           &t.Owner,
		MakerAddress:    &t.MakerAddress,
		BucketIndex:     &t.BucketIndex,
		TransactionHash: &t.TransactionHash,
		TraderSide:      &t.TraderSide,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func ptr(s string) *string { return &s }
func boolPtr(b bool) *bool { return &b }

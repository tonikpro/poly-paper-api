package auth

import (
	"encoding/json"
	"net/http"

	dashboard "github.com/tonikpro/poly-paper-api/api/generated/dashboard"
)

// DashboardHandler implements the dashboard ServerInterface for auth-related endpoints
type DashboardHandler struct {
	svc     *Service
	queries *DashboardQueries
}

func NewDashboardHandler(svc *Service, queries *DashboardQueries) *DashboardHandler {
	return &DashboardHandler{svc: svc, queries: queries}
}

func (h *DashboardHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req dashboard.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password required"})
		return
	}

	user, token, err := h.svc.Register(r.Context(), string(req.Email), req.Password)
	if err != nil {
		if err.Error() == "email already exists" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registration failed"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"token": token,
		"user": map[string]string{
			"id":          user.ID,
			"email":       user.Email,
			"eth_address": user.EthAddress,
		},
	})
}

func (h *DashboardHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req dashboard.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password required"})
		return
	}

	user, token, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": token,
		"user": map[string]string{
			"id":          user.ID,
			"email":       user.Email,
			"eth_address": user.EthAddress,
		},
	})
}

func (h *DashboardHandler) GetEthAddress(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	user, err := h.svc.GetUserByID(r.Context(), userID)
	if err != nil || user == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "user not found"})
		return
	}

	privateKey, err := h.svc.GetEthPrivateKey(r.Context(), user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve private key"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"eth_address": user.EthAddress,
		"private_key": privateKey,
	})
}

func (h *DashboardHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	wallet, err := h.queries.GetWallet(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get wallet"})
		return
	}
	writeJSON(w, http.StatusOK, wallet)
}

func (h *DashboardHandler) Deposit(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	var req dashboard.FundsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid amount"})
		return
	}
	if err := h.queries.Deposit(r.Context(), userID, req.Amount); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *DashboardHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	var req dashboard.FundsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid amount"})
		return
	}
	if err := h.queries.Withdraw(r.Context(), userID, req.Amount); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *DashboardHandler) GetDashboardOrders(w http.ResponseWriter, r *http.Request, params dashboard.GetDashboardOrdersParams) {
	userID := GetUserID(r.Context())
	limit := 50
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}
	orders, total, err := h.queries.GetOrders(r.Context(), userID, params.Status, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get orders"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": orders, "total": total})
}

func (h *DashboardHandler) GetDashboardPositions(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	positions, err := h.queries.GetPositions(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get positions"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"positions": positions})
}

func (h *DashboardHandler) GetDashboardTrades(w http.ResponseWriter, r *http.Request, params dashboard.GetDashboardTradesParams) {
	userID := GetUserID(r.Context())
	limit := 50
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}
	trades, total, err := h.queries.GetTrades(r.Context(), userID, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get trades"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": trades, "total": total})
}

// --- Dashboard API key management (JWT-protected) ---

func (h *DashboardHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	keys, err := h.svc.GetAPIKeys(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list api keys"})
		return
	}
	if keys == nil {
		keys = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"apiKeys": keys})
}

func (h *DashboardHandler) CreateAPIKeyDashboard(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	key, err := h.svc.CreateAPIKey(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create api key"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"apiKey":     key.APIKey,
		"secret":     key.APISecret,
		"passphrase": key.Passphrase,
	})
}

func (h *DashboardHandler) DeleteAPIKeyDashboard(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	var req struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "apiKey required"})
		return
	}
	if err := h.svc.DeleteAPIKeyForUser(r.Context(), userID, req.APIKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete api key"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

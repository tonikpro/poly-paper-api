package auth

import (
	"encoding/json"
	"net/http"

	clob "github.com/tonikpro/poly-paper-api/api/generated/clob"
	"github.com/tonikpro/poly-paper-api/internal/models"
)

// CLOBAuthHandler handles CLOB auth endpoints (api key management)
type CLOBAuthHandler struct {
	svc *Service
}

func NewCLOBAuthHandler(svc *Service) *CLOBAuthHandler {
	return &CLOBAuthHandler{svc: svc}
}

// CreateApiKey handles POST /auth/api-key (L1 auth — middleware already verified)
func (h *CLOBAuthHandler) CreateApiKey(w http.ResponseWriter, r *http.Request, params clob.CreateApiKeyParams) {
	userID := GetUserID(r.Context())

	key, err := h.svc.CreateAPIKey(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create api key"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"apiKey":     key.ID,
		"secret":     key.APISecret,
		"passphrase": key.Passphrase,
	})
}

// DeriveApiKey handles GET /auth/derive-api-key (L1 auth — middleware already verified)
func (h *CLOBAuthHandler) DeriveApiKey(w http.ResponseWriter, r *http.Request, params clob.DeriveApiKeyParams) {
	userID := GetUserID(r.Context())

	key, err := h.svc.DeriveAPIKey(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no api key found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"apiKey":     key.ID,
		"secret":     key.APISecret,
		"passphrase": key.Passphrase,
	})
}

// GetApiKeys handles GET /auth/api-keys (L2 auth — middleware already verified)
func (h *CLOBAuthHandler) GetApiKeys(w http.ResponseWriter, r *http.Request, params clob.GetApiKeysParams) {
	userID := GetUserID(r.Context())

	keys, err := h.svc.GetAPIKeys(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get api keys"})
		return
	}
	if keys == nil {
		keys = []models.APIKey{}
	}

	resp := struct {
		ApiKeys []clob.ApiCreds `json:"apiKeys"`
	}{
		ApiKeys: make([]clob.ApiCreds, 0, len(keys)),
	}
	for _, key := range keys {
		apiKey := key.ID
		secret := key.APISecret
		passphrase := key.Passphrase
		resp.ApiKeys = append(resp.ApiKeys, clob.ApiCreds{
			ApiKey:     &apiKey,
			Secret:     &secret,
			Passphrase: &passphrase,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// DeleteApiKey handles DELETE /auth/api-key (L2 auth — middleware already verified)
func (h *CLOBAuthHandler) DeleteApiKey(w http.ResponseWriter, r *http.Request, params clob.DeleteApiKeyParams) {
	// The API key to delete comes from the POLY_API_KEY header
	apiKey := r.Header.Get("POLY_API_KEY")
	if apiKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing api key"})
		return
	}

	if err := h.svc.DeleteAPIKey(r.Context(), apiKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete api key"})
		return
	}

	writeJSON(w, http.StatusOK, json.RawMessage(`{}`))
}

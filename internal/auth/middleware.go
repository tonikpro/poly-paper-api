package auth

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
)

type contextKey string

const UserIDKey contextKey = "user_id"
const EthAddressKey contextKey = "eth_address"

// JWTMiddleware protects dashboard routes with Bearer token auth
func JWTMiddleware(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
				return
			}

			userID, err := svc.ValidateJWT(parts[1])
			if err != nil {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// L1Middleware protects CLOB auth endpoints with EIP-712 signature verification
func L1Middleware(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			address := r.Header.Get("POLY_ADDRESS")
			signature := r.Header.Get("POLY_SIGNATURE")
			timestamp := r.Header.Get("POLY_TIMESTAMP")
			nonce := r.Header.Get("POLY_NONCE")

			if address == "" || signature == "" || timestamp == "" {
				http.Error(w, `{"error":"missing L1 auth headers"}`, http.StatusUnauthorized)
				return
			}

			if nonce == "" {
				nonce = "0"
			}

			user, err := svc.VerifyL1Auth(r.Context(), address, signature, timestamp, nonce)
			if err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, user.ID)
			ctx = context.WithValue(ctx, EthAddressKey, user.EthAddress)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// L2Middleware protects CLOB trading endpoints with HMAC-SHA256 verification
func L2Middleware(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			address := r.Header.Get("POLY_ADDRESS")
			signature := r.Header.Get("POLY_SIGNATURE")
			timestamp := r.Header.Get("POLY_TIMESTAMP")
			apiKey := r.Header.Get("POLY_API_KEY")
			passphrase := r.Header.Get("POLY_PASSPHRASE")

			if address == "" || signature == "" || timestamp == "" || apiKey == "" || passphrase == "" {
				http.Error(w, `{"error":"missing L2 auth headers"}`, http.StatusUnauthorized)
				return
			}

			// Read body for HMAC verification, then restore it
			var bodyStr string
			if r.Body != nil {
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
					return
				}
				bodyStr = string(bodyBytes)
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}

			// Build the path — SDK signs over path WITHOUT query params
			path := r.URL.Path
			// Strip the /clob prefix since SDK sends paths without it
			path = strings.TrimPrefix(path, "/clob")

			user, err := svc.VerifyL2Auth(r.Context(), address, signature, timestamp, apiKey, passphrase, r.Method, path, bodyStr)
			if err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, user.ID)
			ctx = context.WithValue(ctx, EthAddressKey, user.EthAddress)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserID extracts user ID from context
func GetUserID(ctx context.Context) string {
	if v, ok := ctx.Value(UserIDKey).(string); ok {
		return v
	}
	return ""
}

// GetEthAddress extracts eth address from context
func GetEthAddress(ctx context.Context) string {
	if v, ok := ctx.Value(EthAddressKey).(string); ok {
		return v
	}
	return ""
}

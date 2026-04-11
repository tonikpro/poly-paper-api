package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/tonikpro/poly-paper-api/internal/config"
	"github.com/tonikpro/poly-paper-api/internal/models"
)

const (
	timestampWindow = 300 // ±300 seconds
	driftWarning    = 30  // log warning if drift > 30s
)

type Service struct {
	repo *Repository
	cfg  *config.Config
}

func NewService(repo *Repository, cfg *config.Config) *Service {
	return &Service{repo: repo, cfg: cfg}
}

// --- Registration & Login ---

func (s *Service) Register(ctx context.Context, email, password string) (*models.User, string, error) {
	existing, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, "", err
	}
	if existing != nil {
		return nil, "", fmt.Errorf("email already exists")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("hash password: %w", err)
	}

	// Generate Ethereum keypair
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate eth key: %w", err)
	}

	ethAddress := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	privateKeyBytes := crypto.FromECDSA(privateKey)

	encryptedKey, err := s.encryptPrivateKey(privateKeyBytes)
	if err != nil {
		return nil, "", fmt.Errorf("encrypt private key: %w", err)
	}

	user, err := s.repo.CreateUser(ctx, email, string(hash), ethAddress, encryptedKey)
	if err != nil {
		return nil, "", err
	}

	// Create collateral wallet with $1000 default
	if err := s.repo.CreateCollateralWallet(ctx, user.ID, "1000.000000"); err != nil {
		return nil, "", fmt.Errorf("create wallet: %w", err)
	}

	token, err := s.generateJWT(user.ID)
	if err != nil {
		return nil, "", err
	}

	return user, token, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (*models.User, string, error) {
	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, "", err
	}
	if user == nil {
		return nil, "", fmt.Errorf("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, "", fmt.Errorf("invalid credentials")
	}

	token, err := s.generateJWT(user.ID)
	if err != nil {
		return nil, "", err
	}

	return user, token, nil
}

func (s *Service) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	return s.repo.GetUserByID(ctx, id)
}

func (s *Service) GetEthPrivateKey(ctx context.Context, user *models.User) (string, error) {
	if user.EthPrivateKeyEncrypted == nil {
		return "", fmt.Errorf("no private key stored")
	}
	decrypted, err := s.decryptPrivateKey(user.EthPrivateKeyEncrypted)
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(decrypted), nil
}

// --- JWT ---

func (s *Service) generateJWT(userID string) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

func (s *Service) ValidateJWT(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil {
		return "", err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("invalid token")
	}

	userID, ok := claims["sub"].(string)
	if !ok {
		return "", fmt.Errorf("invalid token claims")
	}

	return userID, nil
}

// --- L1 Auth: EIP-712 ---

func (s *Service) VerifyL1Auth(ctx context.Context, address, signature, timestampStr, nonceStr string) (*models.User, error) {
	// Parse and validate timestamp
	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp")
	}

	now := time.Now().Unix()
	drift := now - ts
	if drift < 0 {
		drift = -drift
	}
	if drift > timestampWindow {
		return nil, fmt.Errorf("timestamp outside allowed window")
	}
	if drift > driftWarning {
		slog.Warn("L1 auth clock drift detected", "drift_seconds", drift, "address", address)
	}

	// Parse nonce
	nonce, err := strconv.ParseInt(nonceStr, 10, 64)
	if err != nil {
		nonce = 0
	}

	// Replay protection
	used, err := s.repo.IsNonceUsed(ctx, address, nonce)
	if err != nil {
		return nil, fmt.Errorf("check nonce: %w", err)
	}
	if used {
		return nil, fmt.Errorf("nonce already used")
	}

	// Verify EIP-712 signature
	if err := s.verifyEIP712Signature(address, timestampStr, nonceStr, signature); err != nil {
		return nil, fmt.Errorf("invalid signature: %w", err)
	}

	// Look up user
	user, err := s.repo.GetUserByEthAddress(ctx, address)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("unknown address")
	}

	// Record nonce
	if err := s.repo.RecordNonce(ctx, address, nonce, ts); err != nil {
		return nil, fmt.Errorf("record nonce: %w", err)
	}

	return user, nil
}

func (s *Service) verifyEIP712Signature(address, timestamp, nonce, signature string) error {
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
			},
			"ClobAuth": {
				{Name: "address", Type: "address"},
				{Name: "timestamp", Type: "string"},
				{Name: "nonce", Type: "uint256"},
				{Name: "message", Type: "string"},
			},
		},
		PrimaryType: "ClobAuth",
		Domain: apitypes.TypedDataDomain{
			Name:    "ClobAuthDomain",
			Version: "1",
			ChainId: math.NewHexOrDecimal256(137),
		},
		Message: apitypes.TypedDataMessage{
			"address":   address,
			"timestamp": timestamp,
			"nonce":     nonce,
			"message":   "This message attests that I control the given wallet",
		},
	}

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return fmt.Errorf("hash domain: %w", err)
	}

	messageHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return fmt.Errorf("hash message: %w", err)
	}

	// EIP-712 hash: keccak256("\x19\x01" || domainSeparator || messageHash)
	rawData := fmt.Sprintf("\x19\x01%s%s", string(domainSeparator), string(messageHash))
	hash := crypto.Keccak256Hash([]byte(rawData))

	// Decode signature
	sigBytes, err := hex.DecodeString(strings.TrimPrefix(signature, "0x"))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if len(sigBytes) != 65 {
		return fmt.Errorf("invalid signature length")
	}

	// Adjust v value for recovery
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}

	// Recover public key
	pubKey, err := crypto.SigToPub(hash.Bytes(), sigBytes)
	if err != nil {
		return fmt.Errorf("recover pubkey: %w", err)
	}

	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	expectedAddr := common.HexToAddress(address)

	if recoveredAddr != expectedAddr {
		return fmt.Errorf("signature address mismatch: got %s, expected %s", recoveredAddr.Hex(), expectedAddr.Hex())
	}

	return nil
}

// --- L2 Auth: HMAC-SHA256 ---

func (s *Service) VerifyL2Auth(ctx context.Context, address, signature, timestampStr, apiKeyStr, passphrase, method, path, body string) (*models.User, error) {
	// Validate timestamp
	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp")
	}

	now := time.Now().Unix()
	drift := now - ts
	if drift < 0 {
		drift = -drift
	}
	if drift > timestampWindow {
		return nil, fmt.Errorf("timestamp outside allowed window")
	}
	if drift > driftWarning {
		slog.Warn("L2 auth clock drift detected", "drift_seconds", drift, "address", address)
	}

	// Look up API key
	apiKey, err := s.repo.GetAPIKeyByKey(ctx, apiKeyStr)
	if err != nil {
		return nil, err
	}
	if apiKey == nil {
		return nil, fmt.Errorf("invalid api key")
	}

	// Passphrase binding
	if apiKey.Passphrase != passphrase {
		return nil, fmt.Errorf("invalid passphrase")
	}

	// Address binding: API key must belong to the claimed address
	user, err := s.repo.GetUserByID(ctx, apiKey.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	if !strings.EqualFold(user.EthAddress, address) {
		return nil, fmt.Errorf("address mismatch")
	}

	// Verify HMAC signature
	if err := s.verifyHMAC(apiKey.APISecret, timestampStr, method, path, body, signature); err != nil {
		return nil, fmt.Errorf("invalid signature: %w", err)
	}

	return user, nil
}

func (s *Service) verifyHMAC(secret, timestamp, method, path, body, expectedSig string) error {
	// Decode base64 secret
	secretBytes, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return fmt.Errorf("decode secret: %w", err)
	}

	// Build canonical message: "{timestamp}{METHOD}{path}{body}"
	message := timestamp + strings.ToUpper(method) + path + body

	// Compute HMAC-SHA256
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(message))
	computed := mac.Sum(nil)

	// URL-safe base64 encode
	computedSig := base64.URLEncoding.EncodeToString(computed)

	// Also try standard base64 for compatibility
	computedSigStd := base64.StdEncoding.EncodeToString(computed)

	if computedSig != expectedSig && computedSigStd != expectedSig {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

// --- API Key Management ---

func (s *Service) CreateAPIKey(ctx context.Context, userID string) (*models.APIKey, error) {
	apiKeyBytes := make([]byte, 32)
	if _, err := rand.Read(apiKeyBytes); err != nil {
		return nil, err
	}
	apiKey := hex.EncodeToString(apiKeyBytes)

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, err
	}
	apiSecret := base64.StdEncoding.EncodeToString(secretBytes)

	passphraseBytes := make([]byte, 16)
	if _, err := rand.Read(passphraseBytes); err != nil {
		return nil, err
	}
	passphrase := hex.EncodeToString(passphraseBytes)

	return s.repo.CreateAPIKey(ctx, userID, apiKey, apiSecret, passphrase)
}

func (s *Service) DeriveAPIKey(ctx context.Context, userID string) (*models.APIKey, error) {
	key, err := s.repo.GetAPIKeyByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, fmt.Errorf("no api key found")
	}
	return key, nil
}

func (s *Service) GetAPIKeys(ctx context.Context, userID string) ([]string, error) {
	return s.repo.GetAPIKeysByUserID(ctx, userID)
}

func (s *Service) DeleteAPIKey(ctx context.Context, apiKey string) error {
	return s.repo.DeleteAPIKey(ctx, apiKey)
}

// --- Encryption helpers ---

func (s *Service) encryptPrivateKey(data []byte) ([]byte, error) {
	key := s.encryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceBytes := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		return nil, err
	}

	return gcm.Seal(nonceBytes, nonceBytes, data, nil), nil
}

func (s *Service) decryptPrivateKey(data []byte) ([]byte, error) {
	key := s.encryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func (s *Service) encryptionKey() []byte {
	h := sha256.Sum256([]byte(s.cfg.EncryptionKey))
	return h[:]
}

// --- Helpers ---

// GenerateEthKeyFromPrivate is used for signing in tests
func GenerateEthKeyFromPrivate(hexKey string) (*ecdsa.PrivateKey, error) {
	key := strings.TrimPrefix(hexKey, "0x")
	privKeyBytes, err := hex.DecodeString(key)
	if err != nil {
		return nil, err
	}
	return crypto.ToECDSA(privKeyBytes)
}

// SignEIP712 signs a ClobAuth EIP-712 message (for testing/client use)
func SignEIP712(privateKey *ecdsa.PrivateKey, address, timestamp, nonce string) (string, error) {
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
			},
			"ClobAuth": {
				{Name: "address", Type: "address"},
				{Name: "timestamp", Type: "string"},
				{Name: "nonce", Type: "uint256"},
				{Name: "message", Type: "string"},
			},
		},
		PrimaryType: "ClobAuth",
		Domain: apitypes.TypedDataDomain{
			Name:    "ClobAuthDomain",
			Version: "1",
			ChainId: math.NewHexOrDecimal256(137),
		},
		Message: apitypes.TypedDataMessage{
			"address":   address,
			"timestamp": timestamp,
			"nonce":     nonce,
			"message":   "This message attests that I control the given wallet",
		},
	}

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return "", err
	}

	messageHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return "", err
	}

	rawData := fmt.Sprintf("\x19\x01%s%s", string(domainSeparator), string(messageHash))
	hash := crypto.Keccak256Hash([]byte(rawData))

	sig, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		return "", err
	}

	sig[64] += 27 // EIP-155 v value
	return "0x" + hex.EncodeToString(sig), nil
}

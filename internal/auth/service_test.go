package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/tonikpro/poly-paper-api/internal/config"
)

func testService() *Service {
	cfg := &config.Config{
		JWTSecret:     "test-jwt-secret",
		EncryptionKey: "test-encryption-key-32bytes!!!!!!",
	}
	return &Service{cfg: cfg}
}

func TestEIP712SignAndVerify(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "42"

	sig, err := SignEIP712(privateKey, address, timestamp, nonce)
	if err != nil {
		t.Fatal("SignEIP712 failed:", err)
	}

	svc := testService()
	if err := svc.verifyEIP712Signature(address, timestamp, nonce, sig); err != nil {
		t.Fatal("verifyEIP712Signature failed:", err)
	}
}

func TestEIP712WrongAddress(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	sig, err := SignEIP712(privateKey, address, timestamp, "0")
	if err != nil {
		t.Fatal(err)
	}

	svc := testService()
	err = svc.verifyEIP712Signature("0x0000000000000000000000000000000000000001", timestamp, "0", sig)
	if err == nil {
		t.Fatal("expected error for wrong address")
	}
}

func TestHMACVerification(t *testing.T) {
	secret := base64.StdEncoding.EncodeToString([]byte("test-secret-key-32bytes-long!!!!"))
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	// Compute expected signature
	secretBytes, _ := base64.StdEncoding.DecodeString(secret)
	message := timestamp + "GET" + "/order" + ""
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(message))
	expectedSig := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	svc := testService()
	if err := svc.verifyHMAC(secret, timestamp, "GET", "/order", "", expectedSig); err != nil {
		t.Fatal("verifyHMAC failed:", err)
	}
}

func TestHMACWithBody(t *testing.T) {
	secret := base64.StdEncoding.EncodeToString([]byte("another-secret-key-32bytes!!!!!"))
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	body := `{"order":{"salt":"123"}}`

	secretBytes, _ := base64.StdEncoding.DecodeString(secret)
	message := timestamp + "POST" + "/order" + body
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(message))
	expectedSig := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	svc := testService()
	if err := svc.verifyHMAC(secret, timestamp, "POST", "/order", body, expectedSig); err != nil {
		t.Fatal("verifyHMAC with body failed:", err)
	}
}

func TestHMACWrongSignature(t *testing.T) {
	secret := base64.StdEncoding.EncodeToString([]byte("test-secret-key-32bytes-long!!!!"))

	svc := testService()
	if err := svc.verifyHMAC(secret, "12345", "GET", "/order", "", "wrong-signature"); err == nil {
		t.Fatal("expected error for wrong signature")
	}
}

func TestEncryptDecryptPrivateKey(t *testing.T) {
	svc := testService()

	data := []byte("private-key-bytes-here-32-bytes!")
	encrypted, err := svc.encryptPrivateKey(data)
	if err != nil {
		t.Fatal("encrypt failed:", err)
	}

	decrypted, err := svc.decryptPrivateKey(encrypted)
	if err != nil {
		t.Fatal("decrypt failed:", err)
	}

	if string(decrypted) != string(data) {
		t.Fatalf("mismatch: got %q, want %q", decrypted, data)
	}
}

func TestJWTGenerateAndValidate(t *testing.T) {
	svc := testService()

	token, err := svc.generateJWT("user-123")
	if err != nil {
		t.Fatal("generateJWT failed:", err)
	}

	userID, err := svc.ValidateJWT(token)
	if err != nil {
		t.Fatal("ValidateJWT failed:", err)
	}

	if userID != "user-123" {
		t.Fatalf("got userID=%q, want %q", userID, "user-123")
	}
}

func TestJWTInvalidToken(t *testing.T) {
	svc := testService()

	_, err := svc.ValidateJWT("invalid.token.here")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestTimestampWindow(t *testing.T) {
	tests := []struct {
		name    string
		drift   int64
		outside bool
	}{
		{"within window", 100, false},
		{"at boundary", 300, false},
		{"outside window", 301, true},
		{"future within", -100, false},
		{"future outside", -301, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().Unix()
			ts := now - tt.drift
			drift := now - ts
			if drift < 0 {
				drift = -drift
			}
			if (drift > timestampWindow) != tt.outside {
				t.Errorf("drift=%d: got outside=%v, want %v", tt.drift, drift > timestampWindow, tt.outside)
			}
		})
	}
}

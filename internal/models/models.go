package models

import (
	"time"
)

type User struct {
	ID                     string    `json:"id"`
	Email                  string    `json:"email"`
	PasswordHash           string    `json:"-"`
	EthAddress             string    `json:"eth_address,omitempty"`
	EthPrivateKeyEncrypted []byte    `json:"-"`
	CreatedAt              time.Time `json:"created_at"`
}

type APIKey struct {
	ID         string    `json:"apiKey"`
	UserID     string    `json:"-"`
	APISecret  string    `json:"secret"`
	Passphrase string    `json:"passphrase"`
	CreatedAt  time.Time `json:"created_at"`
}

type APIKeysResponse struct {
	APIKeys []APIKey `json:"apiKeys"`
}

type Wallet struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	AssetType string    `json:"asset_type"` // COLLATERAL or CONDITIONAL
	TokenID   string    `json:"token_id"`   // empty for COLLATERAL
	Balance   string    `json:"balance"`
	Allowance string    `json:"allowance"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Market struct {
	ID           string    `json:"id"`
	ConditionID  string    `json:"condition_id"`
	Question     string    `json:"question"`
	Slug         string    `json:"slug"`
	Active       bool      `json:"active"`
	Closed       bool      `json:"closed"`
	NegRisk      bool      `json:"neg_risk"`
	TickSize     string    `json:"tick_size"`
	MinOrderSize string    `json:"min_order_size"`
	SyncedAt     time.Time `json:"synced_at"`
}

type OutcomeToken struct {
	TokenID  string `json:"token_id"`
	MarketID string `json:"market_id"`
	Outcome  string `json:"outcome"` // YES or NO
	Winner   *bool  `json:"winner"`  // nil = unresolved
}

// SignedOrder represents the wire-format order from Polymarket SDK.
type SignedOrder struct {
	Salt          int64  `json:"salt"`
	Maker         string `json:"maker"`
	Signer        string `json:"signer"`
	Taker         string `json:"taker"`
	TokenID       string `json:"tokenId"`
	MakerAmount   string `json:"makerAmount"`
	TakerAmount   string `json:"takerAmount"`
	Expiration    string `json:"expiration"`
	Nonce         string `json:"nonce"`
	FeeRateBps    string `json:"feeRateBps"`
	Side          string `json:"side"`
	SignatureType int    `json:"signatureType"`
	Signature     string `json:"signature"`
}

// PostOrderRequest matches Polymarket's POST /order body
type PostOrderRequest struct {
	Order     SignedOrder `json:"order"`
	Owner     string      `json:"owner"`
	OrderType string      `json:"orderType"`
	PostOnly  bool        `json:"postOnly,omitempty"`
}

// OrderResponse matches Polymarket's POST /order response
type OrderResponse struct {
	Success            bool     `json:"success"`
	ErrorMsg           string   `json:"errorMsg"`
	OrderID            string   `json:"orderID"`
	TransactionsHashes []string `json:"transactionsHashes"`
	Status             string   `json:"status"`
	TakingAmount       string   `json:"takingAmount"`
	MakingAmount       string   `json:"makingAmount"`
}

// Order is the DB representation
type Order struct {
	ID              string    `json:"id"`
	UserID          string    `json:"-"`
	Salt            string    `json:"salt"`
	Maker           string    `json:"maker"`
	Signer          string    `json:"signer"`
	Taker           string    `json:"taker"`
	TokenID         string    `json:"tokenId"`
	MakerAmount     string    `json:"makerAmount"`
	TakerAmount     string    `json:"takerAmount"`
	Side            string    `json:"side"`
	Expiration      string    `json:"expiration"`
	Nonce           string    `json:"nonce"`
	FeeRateBps      string    `json:"feeRateBps"`
	SignatureType   int       `json:"signatureType"`
	Signature       string    `json:"signature"`
	Price           string    `json:"price"`
	OriginalSize    string    `json:"original_size"`
	SizeMatched     string    `json:"size_matched"`
	Status          string    `json:"status"`
	OrderType       string    `json:"order_type"`
	PostOnly        bool      `json:"postOnly"`
	Owner           string    `json:"owner"`
	Market          string    `json:"market"`
	AssetID         string    `json:"asset_id"`
	Outcome         string    `json:"outcome,omitempty"`
	AssociateTrades []string  `json:"associate_trades"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// OpenOrder matches Polymarket's GET /data/orders response item
type OpenOrder struct {
	ID              string   `json:"id"`
	Status          string   `json:"status"`
	Owner           string   `json:"owner"`
	MakerAddress    string   `json:"maker_address"`
	Market          string   `json:"market"`
	AssetID         string   `json:"asset_id"`
	Side            string   `json:"side"`
	OriginalSize    string   `json:"original_size"`
	SizeMatched     string   `json:"size_matched"`
	Price           string   `json:"price"`
	AssociateTrades []string `json:"associate_trades"`
	Outcome         string   `json:"outcome"`
	CreatedAt       string   `json:"created_at"`
	Expiration      string   `json:"expiration"`
	OrderType       string   `json:"order_type"`
}

// Trade matches Polymarket's GET /data/trades response item
type Trade struct {
	ID              string `json:"id"`
	TakerOrderID    string `json:"taker_order_id"`
	UserID          string `json:"-"`
	Market          string `json:"market"`
	AssetID         string `json:"asset_id"`
	Side            string `json:"side"`
	Size            string `json:"size"`
	FeeRateBps      string `json:"fee_rate_bps"`
	Price           string `json:"price"`
	Status          string `json:"status"`
	MatchTime       string `json:"match_time"`
	LastUpdate      string `json:"last_update"`
	Outcome         string `json:"outcome"`
	Owner           string `json:"owner"`
	MakerAddress    string `json:"maker_address"`
	BucketIndex     int    `json:"bucket_index"`
	TransactionHash string `json:"transaction_hash"`
	TraderSide      string `json:"trader_side"`
	FillKey         string `json:"-"`
	MakerOrders     []byte `json:"maker_orders"`
}

// PaginatedResponse is the cursor-paginated response envelope
type PaginatedResponse struct {
	NextCursor string      `json:"next_cursor"`
	Data       interface{} `json:"data"`
}

// BalanceAllowanceResponse matches Polymarket's response
type BalanceAllowanceResponse struct {
	Balance   string `json:"balance"`
	Allowance string `json:"allowance"`
}

// CancelRequest matches Polymarket's DELETE /order body
type CancelRequest struct {
	OrderID string `json:"orderID"`
}

type CancelOrdersResponse struct {
	Canceled    []string          `json:"canceled"`
	NotCanceled map[string]string `json:"not_canceled"`
}

type Position struct {
	ID          string    `json:"id"`
	UserID      string    `json:"-"`
	TokenID     string    `json:"token_id"`
	MarketID    string    `json:"market_id"`
	Outcome     string    `json:"outcome"`
	Size        string    `json:"size"`
	AvgPrice    string    `json:"avg_price"`
	RealizedPnl string    `json:"realized_pnl"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

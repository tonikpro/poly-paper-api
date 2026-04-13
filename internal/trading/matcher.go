package trading

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// ErrOrderBookNotFound is returned when the orderbook API returns 404,
// meaning the market no longer exists (e.g. it has resolved or been closed).
var ErrOrderBookNotFound = errors.New("orderbook not found")

// OrderBookResponse is Polymarket's GET /book response
type OrderBookResponse struct {
	Market   string         `json:"market"`
	AssetID  string         `json:"asset_id"`
	Hash     string         `json:"hash"`
	Bids     []OrderBookRow `json:"bids"`
	Asks     []OrderBookRow `json:"asks"`
	UpdateAt string         `json:"update_at"`
}

type OrderBookRow struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

type Matcher struct {
	clobURL    string
	httpClient *http.Client
}

func NewMatcher(clobURL string) *Matcher {
	return &Matcher{
		clobURL: clobURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// FetchOrderBook fetches the current order book from Polymarket for a token.
func (m *Matcher) FetchOrderBook(tokenID string) (*OrderBookResponse, error) {
	url := fmt.Sprintf("%s/book?token_id=%s", m.clobURL, tokenID)
	resp, err := m.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch orderbook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %s", ErrOrderBookNotFound, string(body))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("orderbook API returned %d: %s", resp.StatusCode, string(body))
	}

	var book OrderBookResponse
	if err := json.NewDecoder(resp.Body).Decode(&book); err != nil {
		return nil, fmt.Errorf("decode orderbook: %w", err)
	}
	return &book, nil
}

// MatchResult represents the result of matching an order against the book.
type MatchResult struct {
	Filled     bool
	FillPrice  float64
	FillSize   float64
	Remaining  float64
	Partial    bool
}

// MatchOrder checks if an order can be filled against the Polymarket orderbook.
// Returns how much can be filled and at what price.
func (m *Matcher) MatchOrder(tokenID, side string, orderPrice, orderSize float64) (*MatchResult, error) {
	book, err := m.FetchOrderBook(tokenID)
	if err != nil {
		return nil, err
	}

	slog.Info("MatchOrder: orderbook fetched",
		"token_id", tokenID, "side", side,
		"order_price", orderPrice, "order_size", orderSize,
		"num_bids", len(book.Bids), "num_asks", len(book.Asks))

	if len(book.Asks) > 0 {
		slog.Info("MatchOrder: best ask", "price", book.Asks[0].Price, "size", book.Asks[0].Size)
	}
	if len(book.Bids) > 0 {
		slog.Info("MatchOrder: best bid", "price", book.Bids[0].Price, "size", book.Bids[0].Size)
	}

	result := &MatchResult{Remaining: orderSize}

	if side == "BUY" {
		// BUY: match against asks (lowest first)
		asks := parseLevels(book.Asks)
		sort.Slice(asks, func(i, j int) bool { return asks[i].price < asks[j].price })

		var totalFilled float64
		var weightedPrice float64

		for _, level := range asks {
			if orderPrice < level.price {
				break // our bid is below this ask
			}
			canFill := orderSize - totalFilled
			if canFill <= 0 {
				break
			}
			fillAtLevel := min(canFill, level.size)
			weightedPrice += fillAtLevel * level.price
			totalFilled += fillAtLevel
		}

		if totalFilled > 0 {
			result.Filled = true
			result.FillPrice = weightedPrice / totalFilled
			result.FillSize = totalFilled
			result.Remaining = orderSize - totalFilled
			result.Partial = result.Remaining > 0
		}
	} else {
		// SELL: match against bids (highest first)
		bids := parseLevels(book.Bids)
		sort.Slice(bids, func(i, j int) bool { return bids[i].price > bids[j].price })

		var totalFilled float64
		var weightedPrice float64

		for _, level := range bids {
			if orderPrice > level.price {
				break // our ask is above this bid
			}
			canFill := orderSize - totalFilled
			if canFill <= 0 {
				break
			}
			fillAtLevel := min(canFill, level.size)
			weightedPrice += fillAtLevel * level.price
			totalFilled += fillAtLevel
		}

		if totalFilled > 0 {
			result.Filled = true
			result.FillPrice = weightedPrice / totalFilled
			result.FillSize = totalFilled
			result.Remaining = orderSize - totalFilled
			result.Partial = result.Remaining > 0
		}
	}

	return result, nil
}

type priceLevel struct {
	price float64
	size  float64
}

func parseLevels(rows []OrderBookRow) []priceLevel {
	levels := make([]priceLevel, 0, len(rows))
	for _, row := range rows {
		p, err := strconv.ParseFloat(row.Price, 64)
		if err != nil {
			continue
		}
		s, err := strconv.ParseFloat(row.Size, 64)
		if err != nil {
			continue
		}
		levels = append(levels, priceLevel{price: p, size: s})
	}
	return levels
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

package trading

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// ErrOrderBookNotFound is returned when Polymarket returns 404 for a token.
// This means the market is resolved or closed — orders for this token should be rejected.
var ErrOrderBookNotFound = errors.New("orderbook not found")

// OrderBookResponse is Polymarket's GET /book response.
type OrderBookResponse struct {
	Market  string         `json:"market"`
	AssetID string         `json:"asset_id"`
	Hash    string         `json:"hash"`
	Bids    []OrderBookRow `json:"bids"`
	Asks    []OrderBookRow `json:"asks"`
}

// OrderBookRow is a single price level in the orderbook.
type OrderBookRow struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// MatchResult holds the outcome of matching an order against the book.
type MatchResult struct {
	FillSize  float64 // how many tokens were filled
	FillPrice float64 // weighted average fill price
	Remaining float64 // unfilled size
	Partial   bool    // true if only partially filled
}

// OrderBookClient fetches the live Polymarket orderbook via HTTP.
type OrderBookClient struct {
	clobURL    string
	httpClient *http.Client
}

// NewOrderBookClient creates a client that fetches from the given CLOB URL.
func NewOrderBookClient(clobURL string) *OrderBookClient {
	return &OrderBookClient{
		clobURL:    clobURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchOrderBook retrieves the current orderbook for a token from Polymarket.
// Returns ErrOrderBookNotFound if the market is closed/resolved (404).
func (c *OrderBookClient) FetchOrderBook(tokenID string) (*OrderBookResponse, error) {
	url := fmt.Sprintf("%s/book?token_id=%s", c.clobURL, tokenID)
	resp, err := c.httpClient.Get(url)
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

// MatchOrder checks how much of an order can be filled against the given orderbook.
// This is a pure function — it does not make any HTTP calls.
//
// BUY: matches against asks sorted ascending — fills levels where ask_price <= limitPrice.
// SELL: matches against bids sorted descending — fills levels where bid_price >= limitPrice.
// Returns a MatchResult with FillSize=0 if nothing can be filled.
func MatchOrder(book *OrderBookResponse, side string, limitPrice, size float64) *MatchResult {
	result := &MatchResult{Remaining: size}

	if side == "BUY" {
		asks := parseLevels(book.Asks)
		sort.Slice(asks, func(i, j int) bool { return asks[i].price < asks[j].price })

		var totalFilled, weightedPrice float64
		for _, level := range asks {
			if limitPrice < level.price {
				break
			}
			canFill := size - totalFilled
			if canFill <= 0 {
				break
			}
			fillAtLevel := canFill
			if fillAtLevel > level.size {
				fillAtLevel = level.size
			}
			weightedPrice += fillAtLevel * level.price
			totalFilled += fillAtLevel
		}

		if totalFilled > 0 {
			result.FillSize = totalFilled
			result.FillPrice = weightedPrice / totalFilled
			result.Remaining = size - totalFilled
			result.Partial = result.Remaining > 0.000001
		}
	} else {
		bids := parseLevels(book.Bids)
		sort.Slice(bids, func(i, j int) bool { return bids[i].price > bids[j].price })

		var totalFilled, weightedPrice float64
		for _, level := range bids {
			if limitPrice > level.price {
				break
			}
			canFill := size - totalFilled
			if canFill <= 0 {
				break
			}
			fillAtLevel := canFill
			if fillAtLevel > level.size {
				fillAtLevel = level.size
			}
			weightedPrice += fillAtLevel * level.price
			totalFilled += fillAtLevel
		}

		if totalFilled > 0 {
			result.FillSize = totalFilled
			result.FillPrice = weightedPrice / totalFilled
			result.Remaining = size - totalFilled
			result.Partial = result.Remaining > 0.000001
		}
	}

	return result
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

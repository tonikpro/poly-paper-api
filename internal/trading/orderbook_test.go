package trading

import (
	"fmt"
	"testing"
)

func makeBook(bids, asks [][]float64) *OrderBookResponse {
	b := &OrderBookResponse{}
	for _, level := range bids {
		b.Bids = append(b.Bids, OrderBookRow{
			Price: fmt.Sprintf("%.4f", level[0]),
			Size:  fmt.Sprintf("%.6f", level[1]),
		})
	}
	for _, level := range asks {
		b.Asks = append(b.Asks, OrderBookRow{
			Price: fmt.Sprintf("%.4f", level[0]),
			Size:  fmt.Sprintf("%.6f", level[1]),
		})
	}
	return b
}

func TestMatchOrder_BuyFullFill(t *testing.T) {
	// BUY limit 0.70, asks at 0.60 (size 50) and 0.65 (size 60) — total 110 available
	book := makeBook(nil, [][]float64{{0.60, 50}, {0.65, 60}})
	result := MatchOrder(book, "BUY", 0.70, 100)

	if result.FillSize < 99.9999 || result.FillSize > 100.0001 {
		t.Errorf("FillSize: got %.6f, want 100", result.FillSize)
	}
	// Weighted avg = (50*0.60 + 50*0.65) / 100 = 0.625
	if result.FillPrice < 0.624 || result.FillPrice > 0.626 {
		t.Errorf("FillPrice: got %.4f, want ~0.625", result.FillPrice)
	}
	if result.Remaining > 0.0001 {
		t.Errorf("Remaining: got %.6f, want 0", result.Remaining)
	}
	if result.Partial {
		t.Error("Partial: got true, want false")
	}
}

func TestMatchOrder_BuyPartialFill(t *testing.T) {
	// BUY limit 0.70, only 40 shares available
	book := makeBook(nil, [][]float64{{0.65, 40}})
	result := MatchOrder(book, "BUY", 0.70, 100)

	if result.FillSize < 39.9999 || result.FillSize > 40.0001 {
		t.Errorf("FillSize: got %.6f, want 40", result.FillSize)
	}
	if result.Remaining < 59.9999 || result.Remaining > 60.0001 {
		t.Errorf("Remaining: got %.6f, want 60", result.Remaining)
	}
	if !result.Partial {
		t.Error("Partial: got false, want true")
	}
}

func TestMatchOrder_BuyPriceTooLow(t *testing.T) {
	// BUY limit 0.50, ask at 0.60 — should not fill
	book := makeBook(nil, [][]float64{{0.60, 100}})
	result := MatchOrder(book, "BUY", 0.50, 100)

	if result.FillSize > 0.0001 {
		t.Errorf("FillSize: got %.6f, want 0", result.FillSize)
	}
	if result.Remaining < 99.9999 {
		t.Errorf("Remaining: got %.6f, want 100", result.Remaining)
	}
}

func TestMatchOrder_SellFullFill(t *testing.T) {
	// SELL limit 0.30, bids at 0.40 (size 60) and 0.35 (size 50) — total 110 available
	book := makeBook([][]float64{{0.40, 60}, {0.35, 50}}, nil)
	result := MatchOrder(book, "SELL", 0.30, 100)

	if result.FillSize < 99.9999 || result.FillSize > 100.0001 {
		t.Errorf("FillSize: got %.6f, want 100", result.FillSize)
	}
	// Weighted avg = (60*0.40 + 40*0.35) / 100 = 0.38
	if result.FillPrice < 0.379 || result.FillPrice > 0.381 {
		t.Errorf("FillPrice: got %.4f, want ~0.38", result.FillPrice)
	}
}

func TestMatchOrder_SellPriceTooHigh(t *testing.T) {
	// SELL limit 0.80, best bid is 0.70 — should not fill
	book := makeBook([][]float64{{0.70, 100}}, nil)
	result := MatchOrder(book, "SELL", 0.80, 100)

	if result.FillSize > 0.0001 {
		t.Errorf("FillSize: got %.6f, want 0", result.FillSize)
	}
}

func TestMatchOrder_EmptyBook(t *testing.T) {
	book := makeBook(nil, nil)
	result := MatchOrder(book, "BUY", 0.70, 100)
	if result.FillSize > 0.0001 {
		t.Errorf("FillSize: got %.6f, want 0 on empty book", result.FillSize)
	}
}

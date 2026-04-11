package trading

import (
	"testing"
)

func TestDeriveOrderPriceAndSize_Buy(t *testing.T) {
	// BUY: maker gives 50 USDC (50000000 raw), receives 100 tokens (100000000 raw)
	// price = 50000000/100000000 = 0.5, size = 100000000/1e6 = 100
	price, size, err := deriveOrderPriceAndSize("BUY", "50000000", "100000000")
	if err != nil {
		t.Fatal(err)
	}

	if price != 0.5 {
		t.Errorf("price: got %f, want 0.5", price)
	}
	if size != 100 {
		t.Errorf("size: got %f, want 100", size)
	}
}

func TestDeriveOrderPriceAndSize_Sell(t *testing.T) {
	// SELL: maker gives 100 tokens (100000000 raw), receives 70 USDC (70000000 raw)
	// price = 70000000/100000000 = 0.7, size = 100000000/1e6 = 100
	price, size, err := deriveOrderPriceAndSize("SELL", "100000000", "70000000")
	if err != nil {
		t.Fatal(err)
	}

	if price != 0.7 {
		t.Errorf("price: got %f, want 0.7", price)
	}
	if size != 100 {
		t.Errorf("size: got %f, want 100", size)
	}
}

func TestDeriveOrderPriceAndSize_Invalid(t *testing.T) {
	_, _, err := deriveOrderPriceAndSize("BUY", "invalid", "100")
	if err == nil {
		t.Fatal("expected error for invalid makerAmount")
	}

	_, _, err = deriveOrderPriceAndSize("BUY", "100", "invalid")
	if err == nil {
		t.Fatal("expected error for invalid takerAmount")
	}
}

func TestMatchAgainstBook_BuyFill(t *testing.T) {
	book := &OrderBookResponse{
		Asks: []OrderBookRow{
			{Price: "0.50", Size: "100"},
			{Price: "0.55", Size: "50"},
		},
		Bids: []OrderBookRow{
			{Price: "0.45", Size: "80"},
		},
	}

	// BUY at 0.52 should fill against the 0.50 ask
	result := matchAgainstBook(book, "BUY", 0.52, 50)
	if !result.Filled {
		t.Fatal("expected fill")
	}
	if result.FillSize != 50 {
		t.Errorf("fill size: got %f, want 50", result.FillSize)
	}
	if result.FillPrice != 0.50 {
		t.Errorf("fill price: got %f, want 0.50", result.FillPrice)
	}
}

func TestMatchAgainstBook_BuyNoFill(t *testing.T) {
	book := &OrderBookResponse{
		Asks: []OrderBookRow{
			{Price: "0.60", Size: "100"},
		},
		Bids: []OrderBookRow{},
	}

	// BUY at 0.50 should NOT fill (ask is at 0.60)
	result := matchAgainstBook(book, "BUY", 0.50, 50)
	if result.Filled {
		t.Fatal("expected no fill")
	}
}

func TestMatchAgainstBook_SellFill(t *testing.T) {
	book := &OrderBookResponse{
		Asks: []OrderBookRow{},
		Bids: []OrderBookRow{
			{Price: "0.55", Size: "200"},
			{Price: "0.50", Size: "100"},
		},
	}

	// SELL at 0.50 should fill against the 0.55 bid
	result := matchAgainstBook(book, "SELL", 0.50, 80)
	if !result.Filled {
		t.Fatal("expected fill")
	}
	if result.FillSize != 80 {
		t.Errorf("fill size: got %f, want 80", result.FillSize)
	}
	if result.FillPrice != 0.55 {
		t.Errorf("fill price: got %f, want 0.55", result.FillPrice)
	}
}

func TestMatchAgainstBook_PartialFill(t *testing.T) {
	book := &OrderBookResponse{
		Asks: []OrderBookRow{
			{Price: "0.50", Size: "30"},
		},
		Bids: []OrderBookRow{},
	}

	// BUY 100 at 0.55, but only 30 available at 0.50
	result := matchAgainstBook(book, "BUY", 0.55, 100)
	if !result.Filled {
		t.Fatal("expected partial fill")
	}
	if !result.Partial {
		t.Fatal("expected partial flag")
	}
	if result.FillSize != 30 {
		t.Errorf("fill size: got %f, want 30", result.FillSize)
	}
	if result.Remaining != 70 {
		t.Errorf("remaining: got %f, want 70", result.Remaining)
	}
}

func TestMatchAgainstBook_MultiLevel(t *testing.T) {
	book := &OrderBookResponse{
		Asks: []OrderBookRow{
			{Price: "0.50", Size: "20"},
			{Price: "0.52", Size: "30"},
			{Price: "0.60", Size: "100"},
		},
		Bids: []OrderBookRow{},
	}

	// BUY 40 at 0.55 — should fill 20@0.50 + 20@0.52
	result := matchAgainstBook(book, "BUY", 0.55, 40)
	if !result.Filled {
		t.Fatal("expected fill")
	}
	if result.FillSize != 40 {
		t.Errorf("fill size: got %f, want 40", result.FillSize)
	}
	// Weighted avg: (20*0.50 + 20*0.52) / 40 = 0.51
	expectedAvg := (20*0.50 + 20*0.52) / 40
	if result.FillPrice < expectedAvg-0.001 || result.FillPrice > expectedAvg+0.001 {
		t.Errorf("fill price: got %f, want ~%f", result.FillPrice, expectedAvg)
	}
}

func TestMatchAgainstBook_EmptyBook(t *testing.T) {
	book := &OrderBookResponse{
		Asks: []OrderBookRow{},
		Bids: []OrderBookRow{},
	}

	result := matchAgainstBook(book, "BUY", 0.50, 100)
	if result.Filled {
		t.Fatal("expected no fill on empty book")
	}
}

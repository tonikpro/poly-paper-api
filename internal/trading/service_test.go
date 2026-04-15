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


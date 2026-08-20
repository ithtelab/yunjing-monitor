package server

import "testing"

func TestNormalizeBillingStructuredAndLegacy(t *testing.T) {
	price, amount, currency, cycle, err := normalizeBilling("", "", 0.5, "usd", "month", true)
	if err != nil {
		t.Fatal(err)
	}
	if price != "USD 0.5" || amount != 0.5 || currency != "USD" || cycle != "monthly" {
		t.Fatalf("structured billing = %q %.2f %q %q", price, amount, currency, cycle)
	}

	price, amount, currency, cycle, err = normalizeBilling("$12.00", "年", 0, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if price != "$12.00" || amount != 12 || currency != "USD" || cycle != "annual" {
		t.Fatalf("legacy billing = %q %.2f %q %q", price, amount, currency, cycle)
	}

	amount, currency, cycle = parseLegacyBilling("JP¥980/月", "")
	if amount != 980 || currency != "JPY" || cycle != "monthly" {
		t.Fatalf("JPY legacy billing = %.2f %q %q", amount, currency, cycle)
	}

	if _, _, _, _, err := normalizeBilling("", "", 0, "", "", true); err == nil {
		t.Fatal("required empty billing should fail")
	}
	if _, _, _, _, err := normalizeBilling("", "", 10, "BTC", "monthly", true); err == nil {
		t.Fatal("unsupported currency should fail")
	}
}

func TestParseLegacyBillingUnpricedStaysCustom(t *testing.T) {
	amount, currency, cycle := parseLegacyBilling("联系询价", "")
	if amount != 0 || currency != "" || cycle != "custom" {
		t.Fatalf("unpriced legacy billing = %.2f %q %q", amount, currency, cycle)
	}
}

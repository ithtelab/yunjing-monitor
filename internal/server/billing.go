package server

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var legacyPriceNumber = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

var validCurrencies = map[string]bool{
	"CNY": true,
	"USD": true,
	"HKD": true,
	"EUR": true,
	"JPY": true,
}

var validBillingCycles = map[string]bool{
	"monthly":    true,
	"quarterly":  true,
	"semiannual": true,
	"annual":     true,
	"one_time":   true,
	"custom":     true,
}

func normalizeBilling(price, legacyCycle string, amount float64, currency, cycle string, required bool) (string, float64, string, string, error) {
	price = strings.TrimSpace(price)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	cycle = normalizeBillingCycle(cycle)
	structured := amount > 0 || currency != "" || cycle != ""

	if structured {
		if amount <= 0 || amount > 1_000_000_000 {
			return "", 0, "", "", fmt.Errorf("invalid price_amount")
		}
		if !validCurrencies[currency] {
			return "", 0, "", "", fmt.Errorf("invalid price_currency")
		}
		if !validBillingCycles[cycle] {
			return "", 0, "", "", fmt.Errorf("invalid billing_cycle")
		}
		if price == "" {
			price = formatStructuredPrice(amount, currency)
		}
		return price, amount, currency, cycle, nil
	}

	if price == "" {
		if required {
			return "", 0, "", "", fmt.Errorf("price is required")
		}
		return "", 0, "", "", nil
	}

	parsedAmount, parsedCurrency, parsedCycle := parseLegacyBilling(price, legacyCycle)
	return price, parsedAmount, parsedCurrency, parsedCycle, nil
}

func normalizeBillingCycle(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "month", "monthly", "月", "月付", "每月":
		return "monthly"
	case "quarter", "quarterly", "季", "季度", "季付":
		return "quarterly"
	case "half-year", "semiannual", "semi-annual", "半年", "半年付":
		return "semiannual"
	case "year", "yearly", "annual", "年", "年付", "每年":
		return "annual"
	case "once", "one-time", "one_time", "一次", "一次性":
		return "one_time"
	case "custom", "自定义":
		return "custom"
	default:
		return v
	}
}

func parseLegacyBilling(price, legacyCycle string) (float64, string, string) {
	compact := strings.ReplaceAll(strings.TrimSpace(price), ",", "")
	upper := strings.ToUpper(compact)
	number := legacyPriceNumber.FindString(compact)
	if number == "" {
		return 0, "", "custom"
	}
	amount, err := strconv.ParseFloat(number, 64)
	if err != nil || amount <= 0 {
		return 0, "", "custom"
	}

	currency := ""
	switch {
	case strings.Contains(upper, "HK$") || strings.Contains(upper, "HKD"):
		currency = "HKD"
	case strings.Contains(upper, "US$") || strings.Contains(upper, "USD") || strings.Contains(compact, "$"):
		currency = "USD"
	case strings.Contains(upper, "JPY") || strings.Contains(upper, "JP¥") || strings.Contains(upper, "JP￥"):
		currency = "JPY"
	case strings.Contains(upper, "CNY") || strings.Contains(upper, "RMB") || strings.Contains(compact, "¥") || strings.Contains(compact, "￥"):
		currency = "CNY"
	case strings.Contains(upper, "EUR") || strings.Contains(compact, "€"):
		currency = "EUR"
	}
	if currency == "" {
		return 0, "", "custom"
	}

	cycleText := strings.ToLower(strings.TrimSpace(legacyCycle + " " + price))
	cycle := "custom"
	switch {
	case strings.Contains(cycleText, "半年") || strings.Contains(cycleText, "semi"):
		cycle = "semiannual"
	case strings.Contains(cycleText, "季度") || strings.Contains(cycleText, "季付") || strings.Contains(cycleText, "quarter"):
		cycle = "quarterly"
	case strings.Contains(cycleText, "月") || strings.Contains(cycleText, "month"):
		cycle = "monthly"
	case strings.Contains(cycleText, "年") || strings.Contains(cycleText, "year") || strings.Contains(cycleText, "annual"):
		cycle = "annual"
	case strings.Contains(cycleText, "一次") || strings.Contains(cycleText, "one-time"):
		cycle = "one_time"
	}
	return amount, currency, cycle
}

func formatStructuredPrice(amount float64, currency string) string {
	formatted := strconv.FormatFloat(amount, 'f', 2, 64)
	formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
	return currency + " " + formatted
}

func legacyCycleLabel(cycle string) string {
	switch normalizeBillingCycle(cycle) {
	case "monthly":
		return "月"
	case "quarterly":
		return "季"
	case "semiannual":
		return "半年"
	case "annual":
		return "年"
	case "one_time":
		return "一次性"
	default:
		return "自定义"
	}
}

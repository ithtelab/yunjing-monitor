package server

import (
	"strings"
	"time"
)

func monthlyPriceInCurrency(amount float64, currency, cycle, target string, rates ExchangeRateSnapshot) float64 {
	if amount <= 0 {
		return 0
	}
	divisor := map[string]float64{"monthly": 1, "quarterly": 3, "semiannual": 6, "annual": 12}[cycle]
	if divisor == 0 {
		return 0
	}
	fromRate, targetRate := rates.Rates[strings.ToUpper(currency)], rates.Rates[strings.ToUpper(target)]
	if fromRate <= 0 || targetRate <= 0 {
		return 0
	}
	return amount / divisor / fromRate * targetRate
}

func (s *Server) notifyMarketApproval(nodeID string) {
	listing, ok := s.store.GetMarketListing(nodeID)
	if !ok || !listing.ForSale || normalizeModerationStatus(listing.ModerationStatus) != "approved" {
		return
	}
	views := s.buildMarketViews([]MarketListing{listing}, true)
	if len(views) == 0 {
		return
	}
	view := views[0]
	rates := manualExchangeRate(s.store.GetSettings().ManualUSDCNY, true, "fallback")
	if s.exchangeRates != nil {
		rates = s.exchangeRates.Snapshot(s.store.GetSettings())
	}
	now := time.Now().Unix()
	matches := make([]map[string]any, 0)
	s.features.mu.Lock()
	profile := s.features.data.NodeProfiles[nodeID]
	for index := range s.features.data.Subscriptions {
		subscription := &s.features.data.Subscriptions[index]
		if !subscription.Enabled || subscription.OwnerID == listing.OwnerID || listContains(subscription.MatchedNodeIDs, nodeID) {
			continue
		}
		if !subscriptionRegionMatches(*subscription, view) || !subscriptionTagMatches(*subscription, profile.Tags) || view.MemTotal < subscription.MinMemory {
			continue
		}
		if subscription.MaxPrice > 0 {
			price := monthlyPriceInCurrency(view.PriceAmount, view.PriceCurrency, view.BillingCycle, subscription.Currency, rates)
			if price <= 0 || price > subscription.MaxPrice {
				continue
			}
		}
		subscription.MatchedNodeIDs = append(subscription.MatchedNodeIDs, nodeID)
		if len(subscription.MatchedNodeIDs) > 200 {
			subscription.MatchedNodeIDs = append([]string(nil), subscription.MatchedNodeIDs[len(subscription.MatchedNodeIDs)-200:]...)
		}
		subscription.MatchCount++
		subscription.LastMatchedAt = now
		matches = append(matches, map[string]any{"subscription_id": subscription.ID, "subscription_name": subscription.Name, "owner_id": subscription.OwnerID, "node_id": nodeID, "display_name": view.DisplayName, "region": view.Region, "price": view.Price, "matched_at": now})
	}
	_ = s.features.saveLocked()
	s.features.mu.Unlock()
	s.dispatchPlatformEvent("market.approved", map[string]any{"owner_id": listing.OwnerID, "node_id": nodeID, "display_name": view.DisplayName, "region": view.Region, "price": view.Price, "matched_subscriptions": len(matches)})
	for _, match := range matches {
		s.dispatchPlatformEvent("market.subscription.match", match)
	}
}

func subscriptionRegionMatches(subscription MarketSubscription, listing MarketListingView) bool {
	if len(subscription.Regions) == 0 {
		return true
	}
	for _, region := range subscription.Regions {
		if strings.EqualFold(region, listing.Region) || strings.EqualFold(region, listing.RegionCode) {
			return true
		}
	}
	return false
}

func subscriptionTagMatches(subscription MarketSubscription, tags []string) bool {
	if len(subscription.Tags) == 0 {
		return true
	}
	for _, wanted := range subscription.Tags {
		for _, tag := range tags {
			if strings.EqualFold(wanted, tag) {
				return true
			}
		}
	}
	return false
}

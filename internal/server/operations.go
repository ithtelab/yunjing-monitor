package server

import (
	"net/http"
	"time"
)

type OperationsOverview struct {
	GeneratedAt int64 `json:"generated_at"`
	Nodes       struct {
		Total        int `json:"total"`
		Online       int `json:"online"`
		Offline      int `json:"offline"`
		Pending      int `json:"pending"`
		Expired      int `json:"expired"`
		Expiring     int `json:"expiring"`
		HighResource int `json:"high_resource"`
	} `json:"nodes"`
	Market struct {
		Listings       int `json:"listings"`
		ForSale        int `json:"for_sale"`
		Advertisements int `json:"advertisements"`
		ActiveAds      int `json:"active_ads"`
		FriendLinks    int `json:"friend_links"`
	} `json:"market"`
	Assets struct {
		Priced        int     `json:"priced"`
		Unpriced      int     `json:"unpriced"`
		AnnualCNY     float64 `json:"annual_cny"`
		RateUpdatedAt int64   `json:"rate_updated_at"`
		RateStale     bool    `json:"rate_stale"`
	} `json:"assets"`
	Backup struct {
		Enabled bool            `json:"enabled"`
		Count   int             `json:"count"`
		Latest  *BackupMetadata `json:"latest,omitempty"`
	} `json:"backup"`
	Update  UpdateStatus `json:"update"`
	Version string       `json:"version"`
}

func (s *Server) handleAdminOperations(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		http.Error(w, "admin login required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, s.operationsOverview())
}

func (s *Server) operationsOverview() OperationsOverview {
	var out OperationsOverview
	now := time.Now().Unix()
	out.GeneratedAt, out.Version, out.Update = now, buildVersion, UpdateStatus{State: "idle"}
	if s.updates != nil {
		out.Update = s.updates.readStatus()
	}
	nodes := s.store.AdminNodes(s.cfg.OfflineWait)
	hosts := s.store.AkileHosts()
	hostByID := make(map[string]AkileHost, len(hosts))
	for _, host := range hosts {
		hostByID[host.Host.Name] = host
	}
	alertSettings := defaultAlertSettings()
	if s.alerts != nil {
		alertSettings = s.alerts.Settings()
	}
	rates := manualExchangeRate(s.store.GetSettings().ManualUSDCNY, true, "fallback")
	if s.exchangeRates != nil {
		rates = s.exchangeRates.Snapshot(s.store.GetSettings())
	}
	out.Assets.RateUpdatedAt, out.Assets.RateStale = rates.UpdatedAt, rates.Stale
	for _, node := range nodes {
		out.Nodes.Total++
		if node.LastSeen == 0 {
			out.Nodes.Pending++
		} else if node.Online {
			out.Nodes.Online++
		} else {
			out.Nodes.Offline++
		}
		if due := node.Info.DueTime; due > 0 {
			if due < now {
				out.Nodes.Expired++
			} else if due-now <= 7*86400 {
				out.Nodes.Expiring++
			}
		}
		if host, ok := hostByID[node.NodeID]; ok {
			if host.State.CPU >= alertSettings.CPUThreshold || percent(host.State.MemUsed, host.Host.MemTotal) >= alertSettings.MemoryThreshold || percent(host.State.DiskUsed, host.State.DiskTotal) >= alertSettings.DiskThreshold {
				out.Nodes.HighResource++
			}
		}
		if node.Info.PriceAmount > 0 && node.Info.PriceCurrency != "" && node.Info.BillingCycle != "" {
			out.Assets.Priced++
			out.Assets.AnnualCNY += annualCNY(node.Info.PriceAmount, node.Info.PriceCurrency, node.Info.BillingCycle, rates)
		} else {
			out.Assets.Unpriced++
		}
	}
	listings := s.store.ListMarketListings(false)
	out.Market.Listings = len(listings)
	for _, listing := range listings {
		if listing.ForSale {
			out.Market.ForSale++
		}
	}
	ads := s.store.ListAdvertisements(true, time.Now())
	out.Market.Advertisements = len(ads)
	for _, ad := range ads {
		if ad.Enabled && (ad.StartAt == 0 || ad.StartAt <= now) && (ad.EndAt == 0 || ad.EndAt >= now) {
			out.Market.ActiveAds++
		}
	}
	out.Market.FriendLinks = len(s.store.ListFriendLinks(false))
	out.Backup.Enabled = s.backupReady() == nil
	if out.Backup.Enabled {
		if backups, err := s.listBackups(); err == nil {
			out.Backup.Count = len(backups)
			if len(backups) > 0 {
				latest := backups[0]
				out.Backup.Latest = &latest
			}
		}
	}
	return out
}

func annualCNY(amount float64, currency, cycle string, rates ExchangeRateSnapshot) float64 {
	multiplier := map[string]float64{"monthly": 12, "quarterly": 4, "semiannual": 2, "annual": 1}[cycle]
	if multiplier == 0 {
		return 0
	}
	currencyRate := rates.Rates[currency]
	if currencyRate <= 0 || rates.Rate <= 0 {
		return 0
	}
	return amount / currencyRate * rates.Rate * multiplier
}

func (s *Server) handleHealthLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"status": "ok", "version": buildVersion, "time": time.Now().UTC().Format(time.RFC3339)})
}

func (s *Server) handleHealthReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.store == nil || s.sessions == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"status": "ready", "version": buildVersion})
}

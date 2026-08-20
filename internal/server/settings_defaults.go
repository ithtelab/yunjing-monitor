package server

import (
	"strings"
	"time"
)

var defaultVisitorStatsItems = []string{"online", "today_ips", "total_views"}

func defaultSettings() Settings {
	return Settings{
		SiteName: "云镜监控", FriendLinksTitle: "友情链接", MarketEnabled: true,
		VisitorStatsItems: append([]string(nil), defaultVisitorStatsItems...),
		TimeZone:          "Asia/Shanghai", ExchangeRateMode: "auto", ManualUSDCNY: 7.2,
		UserNodeLimit: 3,
	}
}

func normalizeSettings(settings Settings) Settings {
	settings.SiteName = strings.TrimSpace(settings.SiteName)
	if settings.SiteName == "" {
		settings.SiteName = "云镜监控"
	}
	settings.FooterText = strings.TrimSpace(settings.FooterText)
	settings.FriendLinksTitle = strings.TrimSpace(settings.FriendLinksTitle)
	if settings.FriendLinksTitle == "" {
		settings.FriendLinksTitle = "友情链接"
	}
	settings.VisitorStatsItems = normalizeVisitorStatsItems(settings.VisitorStatsItems)
	settings.TimeZone = strings.TrimSpace(settings.TimeZone)
	if _, err := time.LoadLocation(settings.TimeZone); err != nil {
		settings.TimeZone = "Asia/Shanghai"
	}
	settings.ExchangeRateMode = strings.ToLower(strings.TrimSpace(settings.ExchangeRateMode))
	if settings.ExchangeRateMode != "manual" {
		settings.ExchangeRateMode = "auto"
	}
	if settings.ManualUSDCNY < 1 || settings.ManualUSDCNY > 20 {
		settings.ManualUSDCNY = 7.2
	}
	if settings.UserNodeLimit <= 0 || settings.UserNodeLimit > 100 {
		settings.UserNodeLimit = 3
	}
	return settings
}

func validSiteTimeZone(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	_, err := time.LoadLocation(value)
	return err == nil
}

func normalizeVisitorStatsItems(items []string) []string {
	if len(items) == 0 {
		return append([]string(nil), defaultVisitorStatsItems...)
	}
	valid := map[string]bool{"online": true, "today_ips": true, "today_views": true, "total_views": true}
	out := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if valid[item] && !seen[item] && len(out) < 3 {
			out = append(out, item)
			seen[item] = true
		}
	}
	if len(out) == 0 {
		return append([]string(nil), defaultVisitorStatsItems...)
	}
	return out
}

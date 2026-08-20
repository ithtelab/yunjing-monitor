package server

import (
	"strings"
	"unicode"
)

// regionKeyword maps common Chinese / English labels to a 2-letter region code
// used for public-panel grouping and flags.
var regionKeyword = []struct {
	Keyword string
	Code    string
	Name    string
}{
	{"香港", "HK", "香港"},
	{"Hong Kong", "HK", "香港"},
	{"HK", "HK", "香港"},
	{"澳门", "MO", "澳门"},
	{"Macao", "MO", "澳门"},
	{"Macau", "MO", "澳门"},
	{"台湾", "TW", "台湾"},
	{"Taiwan", "TW", "台湾"},
	{"日本", "JP", "日本"},
	{"东京", "JP", "日本"},
	{"大阪", "JP", "日本"},
	{"Japan", "JP", "日本"},
	{"Tokyo", "JP", "日本"},
	{"Osaka", "JP", "日本"},
	{"JP", "JP", "日本"},
	{"韩国", "KR", "韩国"},
	{"首尔", "KR", "韩国"},
	{"Korea", "KR", "韩国"},
	{"Seoul", "KR", "韩国"},
	{"KR", "KR", "韩国"},
	{"新加坡", "SG", "新加坡"},
	{"Singapore", "SG", "新加坡"},
	{"SG", "SG", "新加坡"},
	{"美国", "US", "美国"},
	{"洛杉矶", "US", "美国"},
	{"圣何塞", "US", "美国"},
	{"西雅图", "US", "美国"},
	{"纽约", "US", "美国"},
	{"芝加哥", "US", "美国"},
	{"达拉斯", "US", "美国"},
	{"USA", "US", "美国"},
	{"United States", "US", "美国"},
	{"US", "US", "美国"},
	{"LA", "US", "美国"},
	{"SJC", "US", "美国"},
	{"SEA", "US", "美国"},
	{"NYC", "US", "美国"},
	{"英国", "GB", "英国"},
	{"伦敦", "GB", "英国"},
	{"UK", "GB", "英国"},
	{"GB", "GB", "英国"},
	{"London", "GB", "英国"},
	{"德国", "DE", "德国"},
	{"法兰克福", "DE", "德国"},
	{"Germany", "DE", "德国"},
	{"Frankfurt", "DE", "德国"},
	{"DE", "DE", "德国"},
	{"法国", "FR", "法国"},
	{"巴黎", "FR", "法国"},
	{"France", "FR", "法国"},
	{"Paris", "FR", "法国"},
	{"FR", "FR", "法国"},
	{"荷兰", "NL", "荷兰"},
	{"阿姆斯特丹", "NL", "荷兰"},
	{"Netherlands", "NL", "荷兰"},
	{"Amsterdam", "NL", "荷兰"},
	{"NL", "NL", "荷兰"},
	{"加拿大", "CA", "加拿大"},
	{"Canada", "CA", "加拿大"},
	{"CA", "CA", "加拿大"},
	{"澳大利亚", "AU", "澳大利亚"},
	{"悉尼", "AU", "澳大利亚"},
	{"Australia", "AU", "澳大利亚"},
	{"Sydney", "AU", "澳大利亚"},
	{"AU", "AU", "澳大利亚"},
	{"中国", "CN", "中国"},
	{"北京", "CN", "中国"},
	{"上海", "CN", "中国"},
	{"广州", "CN", "中国"},
	{"深圳", "CN", "中国"},
	{"China", "CN", "中国"},
	{"CN", "CN", "中国"},
}

// inferRegion extracts region name + 2-letter code from free-form text
// (display name / explicit region field / legacy node id prefix).
func inferRegion(values ...string) (name, code string) {
	for _, raw := range values {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		// Prefer longer keyword matches first (already ordered roughly by specificity).
		lower := strings.ToLower(text)
		for _, item := range regionKeyword {
			if strings.Contains(text, item.Keyword) || strings.Contains(lower, strings.ToLower(item.Keyword)) {
				return item.Name, item.Code
			}
		}
		// Fallback: leading two-letter code like US-node-001 / hk_xxx
		if match := leadingRegionCode(text); match != "" {
			return match, match
		}
	}
	return "", ""
}

func leadingRegionCode(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) < 2 {
		return ""
	}
	a, b := unicode.ToUpper(runes[0]), unicode.ToUpper(runes[1])
	if a < 'A' || a > 'Z' || b < 'A' || b > 'Z' {
		return ""
	}
	if len(runes) > 2 {
		sep := runes[2]
		if sep != '-' && sep != '_' && !unicode.IsSpace(sep) {
			// allow pure 2-letter region fields like "HK"
			if len(runes) != 2 {
				return ""
			}
		}
	}
	code := string([]rune{a, b})
	if code == "UK" {
		return "GB"
	}
	return code
}

func normalizeDisplayName(displayName, nodeID string) string {
	displayName = strings.TrimSpace(displayName)
	if displayName != "" {
		return displayName
	}
	return strings.TrimSpace(nodeID)
}

func resolveNodeMeta(displayName, region, nodeID string) (name, regionName, regionCode string) {
	name = normalizeDisplayName(displayName, nodeID)
	regionName = strings.TrimSpace(region)
	if regionName != "" {
		// If user only typed a free-form region, still try to map a code.
		if inferredName, code := inferRegion(regionName); code != "" {
			if regionName == "" {
				regionName = inferredName
			}
			// Prefer the user's region text as display region name when provided.
			if regionName == "" {
				regionName = inferredName
			}
			return name, regionName, code
		}
		// Keep user region text; code empty → frontend falls back to name prefix.
		return name, regionName, ""
	}
	regionName, regionCode = inferRegion(name, nodeID)
	return name, regionName, regionCode
}

package server

import (
	"encoding/json"
	"os"
	"strings"
)

const officialChangelogHeading = "<h1>更新日志</h1>"

func migrateSiteContent(content siteContent) (siteContent, bool) {
	changed := false
	announcement := strings.ReplaceAll(content.Announcement, `https://t.me/ithte"`, `https://t.me/ithte888"`)
	announcement = strings.ReplaceAll(announcement, "站长 TG @ithte<", "站长 TG @ithte888<")
	if announcement != content.Announcement {
		content.Announcement = announcement
		changed = true
	}

	customOnly := stripEmbeddedOfficialChangelog(content.Changelog)
	if customOnly != content.Changelog {
		content.Changelog = customOnly
		changed = true
	}
	return content, changed
}

// stripEmbeddedOfficialChangelog removes only versioned blocks known to be
// official. Unversioned historical and administrator-authored HTML is kept.
func stripEmbeddedOfficialChangelog(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSpace(strings.TrimPrefix(value, officialChangelogHeading))
	notes, err := loadEmbeddedReleaseNotes()
	if err != nil {
		return value
	}
	for _, note := range notes {
		searchFrom := 0
		for {
			markerRel := strings.Index(value[searchFrom:], note.Version)
			if markerRel < 0 {
				break
			}
			marker := searchFrom + markerRel
			start := strings.LastIndex(value[:marker], "<h3")
			if start < 0 {
				searchFrom = marker + len(note.Version)
				continue
			}
			headingEndRel := strings.Index(value[start:], "</h3>")
			if headingEndRel < 0 || marker >= start+headingEndRel {
				searchFrom = marker + len(note.Version)
				continue
			}
			endRel := strings.Index(value[start+headingEndRel:], "</ul>")
			if endRel < 0 {
				searchFrom = marker + len(note.Version)
				continue
			}
			end := start + headingEndRel + endRel + len("</ul>")
			value = strings.TrimSpace(value[:start] + "\n\n" + value[end:])
			searchFrom = 0
		}
	}
	return strings.TrimSpace(value)
}

func (s *Server) ensureSiteContentMigrations() error {
	content := siteContent{}
	raw, err := os.ReadFile(s.contentPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &content); err != nil {
			return err
		}
	}
	migrated, changed := migrateSiteContent(content)
	if !changed {
		return nil
	}
	return s.saveSiteContent(migrated)
}

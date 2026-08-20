package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func cleanBackupWebDAVURL(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("BACKUP_WEBDAV_URL must be an absolute HTTPS directory URL without credentials, query, or fragment")
	}
	if err := validateNotificationURL(u.Scheme + "://" + u.Host); err != nil {
		return "", fmt.Errorf("invalid BACKUP_WEBDAV_URL: %w", err)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (s *Server) uploadBackupWebDAV(id, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPut, s.cfg.BackupWebDAVURL+"/"+url.PathEscape(id)+".mpbackup", file)
	if err != nil {
		return err
	}
	request.ContentLength = info.Size()
	request.Header.Set("Content-Type", "application/octet-stream")
	if s.cfg.BackupWebDAVUser != "" || s.cfg.BackupWebDAVPassword != "" {
		request.SetBasicAuth(s.cfg.BackupWebDAVUser, s.cfg.BackupWebDAVPassword)
	}
	client := notificationHTTPClient()
	client.Timeout = 30 * time.Second
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("WebDAV upload returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (s *Server) maybeCreateScheduledBackup() {
	if s.cfg.BackupInterval <= 0 || s.backupReady() != nil {
		return
	}
	backups, err := s.listBackups()
	if err == nil && len(backups) > 0 {
		latest, parseErr := time.Parse(time.RFC3339, backups[0].CreatedAt)
		if parseErr == nil && time.Since(latest) < s.cfg.BackupInterval {
			return
		}
	}
	if !s.backupMu.TryLock() {
		return
	}
	defer s.backupMu.Unlock()
	metadata, err := s.createBackup("scheduled")
	if err != nil {
		go s.dispatchPlatformEvent("backup.failed", map[string]any{"reason": "scheduled", "error": cleanFeatureText(err.Error(), 240), "created_at": time.Now().Unix()})
		return
	}
	if metadata.RemoteState == "failed" {
		go s.dispatchPlatformEvent("backup.failed", map[string]any{"reason": "remote_sync", "backup_id": metadata.ID, "error": cleanFeatureText(metadata.RemoteError, 240), "created_at": time.Now().Unix()})
	}
}

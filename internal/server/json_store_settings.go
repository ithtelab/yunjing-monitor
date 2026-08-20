package server

func (s *Store) SiteName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Settings.SiteName == "" {
		return "云镜监控"
	}
	return s.Settings.SiteName
}

func (s *Store) GetSettings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeSettings(s.Settings)
}

func (s *Store) UpdateSettings(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Settings = normalizeSettings(settings)
	return s.saveLocked()
}

package server

import "sort"

func (s *Store) ListServiceMonitors(ownerID string, all bool) []ServiceMonitor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ServiceMonitor, 0, len(s.ServiceMonitors))
	for _, monitor := range s.ServiceMonitors {
		if all || monitor.OwnerID == ownerID {
			out = append(out, monitor)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

func (s *Store) GetServiceMonitor(id string) (ServiceMonitor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	monitor, ok := s.ServiceMonitors[id]
	return monitor, ok
}

func (s *Store) SaveServiceMonitor(monitor ServiceMonitor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ServiceMonitors[monitor.ID] = monitor
	return s.saveLocked()
}

func (s *Store) DeleteServiceMonitor(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ServiceMonitors, id)
	delete(s.ServiceMonitorStates, id)
	delete(s.ServiceMonitorResults, id)
	return s.saveLocked()
}

func (s *Store) GetServiceMonitorState(id string) (ServiceMonitorState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.ServiceMonitorStates[id]
	return state, ok
}

func (s *Store) SaveServiceMonitorCheck(monitor ServiceMonitor, state ServiceMonitorState, result ServiceMonitorResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ServiceMonitors[monitor.ID] = monitor
	s.ServiceMonitorStates[monitor.ID] = state
	items := append(s.ServiceMonitorResults[monitor.ID], result)
	if len(items) > 1000 {
		items = append([]ServiceMonitorResult(nil), items[len(items)-1000:]...)
	}
	s.ServiceMonitorResults[monitor.ID] = items
	return s.saveLocked()
}

func (s *Store) ListServiceMonitorResults(monitorID string, limit int) []ServiceMonitorResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	items := s.ServiceMonitorResults[monitorID]
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	out := append([]ServiceMonitorResult(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt > out[j].CheckedAt })
	return out
}

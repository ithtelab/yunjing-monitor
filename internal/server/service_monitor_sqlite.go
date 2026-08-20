package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"sort"
)

func (s *SQLiteStore) ListServiceMonitors(ownerID string, all bool) []ServiceMonitor {
	query := `SELECT payload_json FROM service_monitors ORDER BY rowid`
	args := []any{}
	if !all {
		query = `SELECT payload_json FROM service_monitors WHERE owner_id = ? ORDER BY rowid`
		args = append(args, ownerID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		log.Printf("sqlite service monitor list failed: %v", err)
		return []ServiceMonitor{}
	}
	defer rows.Close()
	out := []ServiceMonitor{}
	for rows.Next() {
		var payload string
		var monitor ServiceMonitor
		if err := rows.Scan(&payload); err != nil || json.Unmarshal([]byte(payload), &monitor) != nil {
			continue
		}
		out = append(out, monitor)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

func (s *SQLiteStore) GetServiceMonitor(id string) (ServiceMonitor, bool) {
	var payload string
	if err := s.db.QueryRow(`SELECT payload_json FROM service_monitors WHERE id = ?`, id).Scan(&payload); err != nil {
		return ServiceMonitor{}, false
	}
	var monitor ServiceMonitor
	if err := json.Unmarshal([]byte(payload), &monitor); err != nil {
		return ServiceMonitor{}, false
	}
	return monitor, true
}

func (s *SQLiteStore) SaveServiceMonitor(monitor ServiceMonitor) error {
	payload, err := json.Marshal(monitor)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO service_monitors(id, owner_id, enabled, next_run_at, payload_json)
		VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET owner_id=excluded.owner_id,
		enabled=excluded.enabled, next_run_at=excluded.next_run_at, payload_json=excluded.payload_json`,
		monitor.ID, monitor.OwnerID, boolToInt(monitor.Enabled), monitor.NextRunAt, string(payload))
	return err
}

func (s *SQLiteStore) DeleteServiceMonitor(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM service_monitor_results WHERE monitor_id = ?`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM service_monitor_states WHERE monitor_id = ?`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM service_monitors WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetServiceMonitorState(id string) (ServiceMonitorState, bool) {
	var payload string
	err := s.db.QueryRow(`SELECT payload_json FROM service_monitor_states WHERE monitor_id = ?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return ServiceMonitorState{}, false
	}
	var state ServiceMonitorState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return ServiceMonitorState{}, false
	}
	return state, true
}

func (s *SQLiteStore) SaveServiceMonitorCheck(monitor ServiceMonitor, state ServiceMonitorState, result ServiceMonitorResult) error {
	monitorPayload, err := json.Marshal(monitor)
	if err != nil {
		return err
	}
	statePayload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	resultPayload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO service_monitors(id, owner_id, enabled, next_run_at, payload_json)
		VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET owner_id=excluded.owner_id,
		enabled=excluded.enabled, next_run_at=excluded.next_run_at, payload_json=excluded.payload_json`,
		monitor.ID, monitor.OwnerID, boolToInt(monitor.Enabled), monitor.NextRunAt, string(monitorPayload)); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO service_monitor_states(monitor_id, payload_json) VALUES(?,?)
		ON CONFLICT(monitor_id) DO UPDATE SET payload_json=excluded.payload_json`, monitor.ID, string(statePayload)); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT OR REPLACE INTO service_monitor_results(id, monitor_id, owner_id, checked_at, payload_json) VALUES(?,?,?,?,?)`,
		result.ID, monitor.ID, monitor.OwnerID, result.CheckedAt, string(resultPayload)); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM service_monitor_results WHERE monitor_id = ? AND id NOT IN
		(SELECT id FROM service_monitor_results WHERE monitor_id = ? ORDER BY checked_at DESC, id DESC LIMIT 1000)`, monitor.ID, monitor.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListServiceMonitorResults(monitorID string, limit int) []ServiceMonitorResult {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT payload_json FROM service_monitor_results WHERE monitor_id = ? ORDER BY checked_at DESC, id DESC LIMIT ?`, monitorID, limit)
	if err != nil {
		return []ServiceMonitorResult{}
	}
	defer rows.Close()
	out := []ServiceMonitorResult{}
	for rows.Next() {
		var payload string
		var result ServiceMonitorResult
		if err := rows.Scan(&payload); err != nil || json.Unmarshal([]byte(payload), &result) != nil {
			continue
		}
		out = append(out, result)
	}
	return out
}

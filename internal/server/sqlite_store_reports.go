package server

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"vps-agent/internal/agent"
	serverapp "vps-agent/internal/server/application"
	serverdomain "vps-agent/internal/server/domain"
)

func (s *SQLiteStore) UpsertReport(metrics agent.Metrics, maxNodes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	exists, err := reportExistsTx(tx, metrics.NodeID)
	if err != nil {
		return err
	}
	if !exists {
		count, err := countRowsTx(tx, "reports")
		if err != nil {
			return err
		}
		if count >= maxNodes {
			return fmt.Errorf("max nodes reached")
		}
	}
	if err := upsertReportTx(tx, metrics); err != nil {
		return err
	}
	if err := insertPlannedIfMissingTx(tx, metrics.NodeID, time.Now().Unix()); err != nil {
		return err
	}
	if err := updateTrafficTx(tx, metrics, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) UpsertInfo(info HostInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	info.AuthSecret = ""
	info.TrafficResetDay = serverdomain.NormalizeTrafficResetDay(info.TrafficResetDay)
	if planned, exists, err := getPlannedTx(tx, info.Name); err != nil {
		return err
	} else if exists {
		if info.DisplayName == "" {
			info.DisplayName = planned.DisplayName
		} else {
			planned.DisplayName = strings.TrimSpace(info.DisplayName)
		}
		if info.Region == "" {
			info.Region = planned.Region
		} else {
			planned.Region = strings.TrimSpace(info.Region)
		}
		if info.RegionCode == "" {
			info.RegionCode = planned.RegionCode
		} else {
			planned.RegionCode = strings.TrimSpace(info.RegionCode)
		}
		if err := upsertPlannedTx(tx, planned); err != nil {
			return err
		}
	}
	if err := upsertInfoTx(tx, info); err != nil {
		return err
	}
	if err := syncTrafficResetDayTx(tx, info.Name, info.TrafficResetDay, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) SetNodeDueTime(nodeID string, dueTime int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, exists, err := getPlannedTx(tx, nodeID); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("node not found")
	}
	info, exists, err := getInfoTx(tx, nodeID)
	if err != nil {
		return err
	}
	if !exists {
		info = HostInfo{Name: nodeID, TrafficResetDay: serverdomain.NormalizeTrafficResetDay(0)}
	}
	info.Name = nodeID
	info.DueTime = dueTime
	if err := upsertInfoTx(tx, info); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) SetNodeBilling(nodeID, price, legacyCycle string, amount float64, currency, billingCycle string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, exists, err := getPlannedTx(tx, nodeID); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("node not found")
	}
	info, exists, err := getInfoTx(tx, nodeID)
	if err != nil {
		return err
	}
	if !exists {
		info = HostInfo{Name: nodeID, TrafficResetDay: serverdomain.NormalizeTrafficResetDay(0)}
	}
	info.Name = nodeID
	info.Price = price
	info.Cycle = legacyCycle
	info.PriceAmount = amount
	info.PriceCurrency = currency
	info.BillingCycle = billingCycle
	if err := upsertInfoTx(tx, info); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) InfoList() []HostInfo {
	infos, err := s.loadInfos()
	if err != nil {
		log.Printf("sqlite info list failed: %v", err)
		return nil
	}
	out := make([]HostInfo, 0, len(infos))
	for _, info := range infos {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *SQLiteStore) AkileHosts() []AkileHost {
	reports, err := s.loadReports()
	if err != nil {
		log.Printf("sqlite reports read failed: %v", err)
		return nil
	}
	planned, err := s.loadPlanned()
	if err != nil {
		log.Printf("sqlite planned read failed: %v", err)
		return nil
	}
	traffic, err := s.loadTraffic()
	if err != nil {
		log.Printf("sqlite traffic read failed: %v", err)
		return nil
	}
	infos, err := s.loadInfos()
	if err != nil {
		log.Printf("sqlite infos read failed: %v", err)
		infos = map[string]HostInfo{}
	}
	out := make([]AkileHost, 0, len(planned)+len(reports))
	for _, metrics := range reports {
		host := serverapp.ToAkileHost(metrics, traffic[metrics.NodeID])
		displayName, region, regionCode := sqliteNodeMeta(planned, infos, metrics.NodeID)
		serverapp.ApplyHostMeta(&host, displayName, region, regionCode)
		out = append(out, host)
	}
	for name := range planned {
		if _, ok := reports[name]; ok {
			continue
		}
		host := serverapp.OfflineAkileHost(name)
		displayName, region, regionCode := sqliteNodeMeta(planned, infos, name)
		serverapp.ApplyHostMeta(&host, displayName, region, regionCode)
		out = append(out, host)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].Host.DisplayName
		if left == "" {
			left = out[i].Host.Name
		}
		right := out[j].Host.DisplayName
		if right == "" {
			right = out[j].Host.Name
		}
		return left < right
	})
	return out
}

func sqliteNodeMeta(planned map[string]PlannedNode, infos map[string]HostInfo, nodeID string) (displayName, region, regionCode string) {
	if p, ok := planned[nodeID]; ok {
		displayName, region, regionCode = p.DisplayName, p.Region, p.RegionCode
	}
	if info, ok := infos[nodeID]; ok {
		if displayName == "" {
			displayName = info.DisplayName
		}
		if region == "" {
			region = info.Region
		}
		if regionCode == "" {
			regionCode = info.RegionCode
		}
	}
	return displayName, region, regionCode
}

func (s *SQLiteStore) loadInfos() (map[string]HostInfo, error) {
	rows, err := s.db.Query(`SELECT node_id, info_json FROM host_infos`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]HostInfo{}
	for rows.Next() {
		var nodeID, payload string
		if err := rows.Scan(&nodeID, &payload); err != nil {
			return nil, err
		}
		var info HostInfo
		if err := json.Unmarshal([]byte(payload), &info); err != nil {
			return nil, err
		}
		info.Name = nodeID
		info.AuthSecret = ""
		info.TrafficResetDay = serverdomain.NormalizeTrafficResetDay(info.TrafficResetDay)
		out[nodeID] = info
	}
	return out, rows.Err()
}

func (s *SQLiteStore) loadReports() (map[string]agent.Metrics, error) {
	rows, err := s.db.Query(`SELECT node_id, metrics_json FROM reports`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]agent.Metrics{}
	for rows.Next() {
		var nodeID, payload string
		if err := rows.Scan(&nodeID, &payload); err != nil {
			return nil, err
		}
		var metrics agent.Metrics
		if err := json.Unmarshal([]byte(payload), &metrics); err != nil {
			return nil, err
		}
		metrics.NodeID = nodeID
		out[nodeID] = metrics
	}
	return out, rows.Err()
}

func (s *SQLiteStore) loadTraffic() (map[string]TrafficStat, error) {
	rows, err := s.db.Query(`SELECT node_id, reset_day, period_start, next_reset, last_rx_bytes, last_tx_bytes, rx_total, tx_total, updated_at FROM traffic_stats`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]TrafficStat{}
	for rows.Next() {
		var nodeID string
		var stat TrafficStat
		var lastRx, lastTx, rxTotal, txTotal string
		if err := rows.Scan(&nodeID, &stat.ResetDay, &stat.PeriodStart, &stat.NextReset, &lastRx, &lastTx, &rxTotal, &txTotal, &stat.UpdatedAt); err != nil {
			return nil, err
		}
		var err error
		if stat.LastRxBytes, err = parseUintText(lastRx); err != nil {
			return nil, err
		}
		if stat.LastTxBytes, err = parseUintText(lastTx); err != nil {
			return nil, err
		}
		if stat.RxTotal, err = parseUintText(rxTotal); err != nil {
			return nil, err
		}
		if stat.TxTotal, err = parseUintText(txTotal); err != nil {
			return nil, err
		}
		out[nodeID] = stat
	}
	return out, rows.Err()
}

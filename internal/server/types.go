package server

import (
	serverapp "vps-agent/internal/server/application"
	"vps-agent/internal/server/domain"
)

type Settings = domain.Settings
type FriendLink = domain.FriendLink
type PlannedNode = domain.PlannedNode
type AdminNode = domain.AdminNode
type NodeBackup = domain.NodeBackup
type NodeBackupRecord = domain.NodeBackupRecord
type HostInfo = domain.HostInfo
type TrafficStat = domain.TrafficStat
type Owner = domain.Owner
type MarketListing = domain.MarketListing
type MarketCategory = domain.MarketCategory
type MarketListingView = domain.MarketListingView
type SellerTrustView = domain.SellerTrustView
type MarketListingUpdate = domain.MarketListingUpdate
type NodeBillingUpdate = domain.NodeBillingUpdate
type MarketSubmission = domain.MarketSubmission
type OwnerNodeMutation = domain.OwnerNodeMutation
type OwnerAdminMutation = domain.OwnerAdminMutation
type ListingModerationMutation = domain.ListingModerationMutation
type AuditEvent = domain.AuditEvent
type RecycledNode = domain.RecycledNode
type RecycleRecord = domain.RecycleRecord
type ServiceMonitor = domain.ServiceMonitor
type ServiceMonitorState = domain.ServiceMonitorState
type ServiceMonitorResult = domain.ServiceMonitorResult
type Advertisement = domain.Advertisement
type AdLayoutSettings = domain.AdLayoutSettings
type MarketAdsResponse = domain.MarketAdsResponse
type AkileHost = serverapp.AkileHost
type AkileHostMeta = serverapp.AkileHostMeta
type AkileHostState = serverapp.AkileHostState

type dataStore = serverapp.Store

var (
	_ serverapp.Store = (*Store)(nil)
	_ serverapp.Store = (*SQLiteStore)(nil)
)

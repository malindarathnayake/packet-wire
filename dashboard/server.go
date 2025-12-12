package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
)

// Config holds dashboard configuration
type Config struct {
	RedisURL              string
	RedisPassword         string
	DashboardPort         string
	DashboardHost         string
	MetricsRetentionHours int
	ClientTimeoutSeconds  int
}

// Client represents a sender or receiver client
type Client struct {
	ID                 string    `json:"id"`
	Type               string    `json:"type"` // "sender" or "receiver"
	Status             string    `json:"status"` // "online", "offline", "ready"
	SourceIP           string    `json:"source_ip,omitempty"`
	LocalIP            string    `json:"local_ip,omitempty"`
	ExternalIP         string    `json:"external_ip,omitempty"`
	DestinationIP      string    `json:"destination_ip,omitempty"`
	DestinationPort    int       `json:"destination_port,omitempty"`
	ListenIP           string    `json:"listen_ip,omitempty"`
	ListenPort         int       `json:"listen_port,omitempty"`
	DetectedSourceIP   string    `json:"detected_source_ip,omitempty"`
	PacketsSent        int64     `json:"packets_sent,omitempty"`
	BytesSent          int64     `json:"bytes_sent,omitempty"`
	PacketsReceived    int64     `json:"packets_received,omitempty"`
	BytesReceived      int64     `json:"bytes_received,omitempty"`
	EncryptionEnabled  bool      `json:"encryption_enabled"`
	DecodeSuccessCount int64     `json:"decode_success_count,omitempty"`
	DecodeFailureCount int64     `json:"decode_failure_count,omitempty"`
	AvgLatencyMs       float64   `json:"avg_latency_ms,omitempty"`
	LastSeen           time.Time `json:"last_seen"`
}

// IPGroup represents a group of clients sharing the same IP
type IPGroup struct {
	ID                 string     `json:"id"`
	IP                 string     `json:"ip"`
	Type               string     `json:"type"` // "sender_group" or "receiver_group"
	Status             string     `json:"status"`
	ClientCount        int        `json:"client_count"`
	PortCount          int        `json:"port_count"`
	Ports              []PortInfo `json:"ports"`
	PacketsSent        int64      `json:"packets_sent,omitempty"`
	BytesSent          int64      `json:"bytes_sent,omitempty"`
	PacketsReceived    int64      `json:"packets_received,omitempty"`
	BytesReceived      int64      `json:"bytes_received,omitempty"`
	EncryptionEnabled  bool       `json:"encryption_enabled"`
	DecodeSuccessCount int64      `json:"decode_success_count,omitempty"`
	DecodeFailureCount int64      `json:"decode_failure_count,omitempty"`
	AvgLatencyMs       float64    `json:"avg_latency_ms,omitempty"`
	LastSeen           time.Time  `json:"last_seen"`
	DestinationIP      string     `json:"destination_ip,omitempty"`
	DetectedSourceIP   string     `json:"detected_source_ip,omitempty"`
}

// PortInfo represents metrics for a single port
type PortInfo struct {
	Port            int     `json:"port"`
	ClientID        string  `json:"client_id"`
	PacketsSent     int64   `json:"packets_sent,omitempty"`
	PacketsReceived int64   `json:"packets_received,omitempty"`
	AvgLatencyMs    float64 `json:"avg_latency_ms,omitempty"`
	Status          string  `json:"status"`
}

// Edge represents a connection between sender and receiver
type Edge struct {
	From            string  `json:"from"`
	To              string  `json:"to"`
	PacketsSent     int64   `json:"packets_sent"`
	PacketsReceived int64   `json:"packets_received"`
	PacketLossRate  float64 `json:"packet_loss_rate"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	IsMirror        bool    `json:"is_mirror"`
}

// NetworkTopology represents the full network state
type NetworkTopology struct {
	Nodes     []Client  `json:"nodes"`
	Edges     []Edge    `json:"edges"`
	Timestamp time.Time `json:"timestamp"`
}

// GroupedNetworkTopology represents the network state with IP grouping
type GroupedNetworkTopology struct {
	Groups    []IPGroup `json:"groups"`
	Edges     []Edge    `json:"edges"`
	Timestamp time.Time `json:"timestamp"`
}

// SenderMetrics is the payload from sender clients
type SenderMetrics struct {
	ClientID          string    `json:"client_id"`
	ClientType        string    `json:"client_type"`
	Timestamp         time.Time `json:"timestamp"`
	SourceIP          string    `json:"source_ip"`
	LocalIP           string    `json:"local_ip,omitempty"`
	ExternalIP        string    `json:"external_ip,omitempty"`
	DestinationIP     string    `json:"destination_ip"`
	DestinationPort   int       `json:"destination_port"`
	PacketsSent       int64     `json:"packets_sent"`
	BytesSent         int64     `json:"bytes_sent"`
	EncryptionEnabled bool      `json:"encryption_enabled"`
	LastSendTime      time.Time `json:"last_send_time,omitempty"`
}

// ReceiverMetrics is the payload from receiver clients
type ReceiverMetrics struct {
	ClientID           string    `json:"client_id"`
	ClientType         string    `json:"client_type"`
	Timestamp          time.Time `json:"timestamp"`
	ListenIP           string    `json:"listen_ip"`
	LocalIP            string    `json:"local_ip,omitempty"`
	ExternalIP         string    `json:"external_ip,omitempty"`
	ListenPort         int       `json:"listen_port"`
	DetectedSourceIP   string    `json:"detected_source_ip,omitempty"`
	PacketsReceived    int64     `json:"packets_received"`
	BytesReceived      int64     `json:"bytes_received"`
	EncryptionEnabled  bool      `json:"encryption_enabled"`
	DecodeSuccessCount int64     `json:"decode_success_count"`
	DecodeFailureCount int64     `json:"decode_failure_count"`
	AvgLatencyMs       float64   `json:"avg_latency_ms"`
	LastReceiveTime    time.Time `json:"last_receive_time,omitempty"`
}

// WebSocketMessage is sent to connected browsers
type WebSocketMessage struct {
	Type string      `json:"type"` // "node_update", "node_offline", "edge_update", "full_topology"
	Data interface{} `json:"data"`
}

// DashboardServer manages client state and WebSocket connections
type DashboardServer struct {
	config      Config
	redisClient *redis.Client
	wsClients   map[*websocket.Conn]bool
	wsMutex     sync.RWMutex
	upgrader    websocket.Upgrader
	ctx         context.Context
	cancel      context.CancelFunc
}

var cfg Config

func main() {
	log.Println("=== Packet Wire Dashboard Starting ===")

	// Setup shutdown context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("received signal %v, initiating shutdown", sig)
		cancel()
	}()

	// Load configuration
	cfg = loadConfig()

	// Initialize Redis client
	log.Println("Step 1: Connecting to Redis...")
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisURL,
		Password: cfg.RedisPassword,
		DB:       0,
	})

	// Test Redis connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Printf("✓ Connected to Redis at %s", cfg.RedisURL)
	defer redisClient.Close()

	// Create dashboard server
	server := &DashboardServer{
		config:      cfg,
		redisClient: redisClient,
		wsClients:   make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		ctx:    ctx,
		cancel: cancel,
	}

	// Start background jobs
	go server.runOfflineDetection(ctx)
	go server.runMetricsCleanup(ctx)

	// Setup HTTP routes
	log.Println("Step 2: Setting up HTTP routes...")
	http.HandleFunc("/health", server.handleHealth)
	http.HandleFunc("/api/metrics", server.handleMetrics)
	http.HandleFunc("/api/network", server.handleNetwork)
	http.HandleFunc("/api/network-grouped", server.handleNetworkGrouped)
	http.HandleFunc("/ws", server.handleWebSocket)
	http.HandleFunc("/dashboard-config.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "dashboard-config.json")
	})
	http.Handle("/", http.FileServer(http.Dir(".")))
	log.Println("✓ HTTP routes configured")

	log.Println("=== Startup Complete ===")
	fmt.Printf("\n")
	fmt.Printf("Dashboard URL: http://%s:%s\n", cfg.DashboardHost, cfg.DashboardPort)
	fmt.Printf("Metrics endpoint: POST http://%s:%s/api/metrics\n", cfg.DashboardHost, cfg.DashboardPort)
	fmt.Printf("Network endpoint: GET http://%s:%s/api/network\n", cfg.DashboardHost, cfg.DashboardPort)
	fmt.Printf("WebSocket: ws://%s:%s/ws\n", cfg.DashboardHost, cfg.DashboardPort)
	fmt.Printf("\nDashboard is ready to serve requests.\n\n")

	// Start HTTP server
	addr := fmt.Sprintf("%s:%s", cfg.DashboardHost, cfg.DashboardPort)
	httpServer := &http.Server{Addr: addr}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for shutdown
	<-ctx.Done()
	log.Println("Shutting down HTTP server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Dashboard shutdown complete")
}

func loadConfig() Config {
	return Config{
		RedisURL:              getEnv("REDIS_URL", "localhost:6379"),
		RedisPassword:         getEnv("REDIS_PASSWORD", ""),
		DashboardPort:         getEnv("DASHBOARD_PORT", "8080"),
		DashboardHost:         getEnv("DASHBOARD_HOST", "0.0.0.0"),
		MetricsRetentionHours: getEnvInt("METRICS_RETENTION_HOURS", 6),
		ClientTimeoutSeconds:  getEnvInt("CLIENT_TIMEOUT_SECONDS", 15),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

// handleHealth returns dashboard health status
func (s *DashboardServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check Redis connection
	if err := s.redisClient.Ping(s.ctx).Err(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// handleMetrics receives metrics from sender/receiver clients
func (s *DashboardServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, `{"status":"error","error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Parse the incoming JSON to determine client type
	var rawMetrics map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&rawMetrics); err != nil {
		http.Error(w, `{"status":"error","error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	clientType, _ := rawMetrics["client_type"].(string)
	clientID, _ := rawMetrics["client_id"].(string)

	if clientID == "" {
		http.Error(w, `{"status":"error","error":"client_id is required"}`, http.StatusBadRequest)
		return
	}

	if clientType != "sender" && clientType != "receiver" {
		http.Error(w, `{"status":"error","error":"client_type must be 'sender' or 'receiver'"}`, http.StatusBadRequest)
		return
	}

	// Store the metrics in Redis
	metricsJSON, _ := json.Marshal(rawMetrics)
	clientKey := fmt.Sprintf("client:%s", clientID)

	// Store client data as hash
	now := time.Now()
	pipe := s.redisClient.Pipeline()
	pipe.HSet(s.ctx, clientKey, map[string]interface{}{
		"type":           clientType,
		"status":         "online",
		"last_seen":      now.Format(time.RFC3339),
		"latest_metrics": string(metricsJSON),
	})

	// Store metrics history in sorted set
	metricsHistoryKey := fmt.Sprintf("metrics:%s", clientID)
	pipe.ZAdd(s.ctx, metricsHistoryKey, &redis.Z{
		Score:  float64(now.UnixMilli()),
		Member: string(metricsJSON),
	})

	// Set TTL on metrics history
	retentionDuration := time.Duration(s.config.MetricsRetentionHours) * time.Hour
	pipe.Expire(s.ctx, metricsHistoryKey, retentionDuration)

	if _, err := pipe.Exec(s.ctx); err != nil {
		log.Printf("Redis error storing metrics for %s: %v", clientID, err)
		http.Error(w, `{"status":"error","error":"failed to store metrics"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("Received metrics from %s (%s)", clientID, clientType)

	// Broadcast update to WebSocket clients
	client := s.buildClientFromMetrics(rawMetrics, clientType)
	s.broadcastUpdate("node_update", client)

	// Recalculate and broadcast edges
	s.updateAndBroadcastEdges()

	// Return success response
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"client_id":   clientID,
		"received_at": now.Format(time.RFC3339),
	})
}

// handleNetwork returns the current network topology
func (s *DashboardServer) handleNetwork(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	topology := s.buildNetworkTopology()
	json.NewEncoder(w).Encode(topology)
}

// handleNetworkGrouped returns the network topology grouped by IP
func (s *DashboardServer) handleNetworkGrouped(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	topology := s.buildGroupedTopology()
	json.NewEncoder(w).Encode(topology)
}

// buildGroupedTopology groups clients by their IP addresses
func (s *DashboardServer) buildGroupedTopology() GroupedNetworkTopology {
	// First get the raw topology
	rawTopology := s.buildNetworkTopology()

	// Group senders by source IP
	senderGroups := make(map[string]*IPGroup)
	// Group receivers by listen IP
	receiverGroups := make(map[string]*IPGroup)

	for _, client := range rawTopology.Nodes {
		if client.Type == "sender" {
			ip := client.SourceIP
			if ip == "" {
				ip = "unknown"
			}

			group, exists := senderGroups[ip]
			if !exists {
				group = &IPGroup{
					ID:     fmt.Sprintf("sender_group_%s", ip),
					IP:     ip,
					Type:   "sender_group",
					Status: "offline",
					Ports:  []PortInfo{},
				}
				senderGroups[ip] = group
			}

			// Aggregate metrics
			group.ClientCount++
			group.PacketsSent += client.PacketsSent
			group.BytesSent += client.BytesSent
			if client.EncryptionEnabled {
				group.EncryptionEnabled = true
			}
			if client.DestinationIP != "" {
				group.DestinationIP = client.DestinationIP
			}
			if client.LastSeen.After(group.LastSeen) {
				group.LastSeen = client.LastSeen
			}
			if client.Status == "online" {
				group.Status = "online"
			}

			// Add port info
			group.Ports = append(group.Ports, PortInfo{
				Port:        client.DestinationPort,
				ClientID:    client.ID,
				PacketsSent: client.PacketsSent,
				Status:      client.Status,
			})
			group.PortCount = len(group.Ports)

		} else if client.Type == "receiver" {
			ip := client.ListenIP
			if ip == "" {
				ip = "unknown"
			}

			group, exists := receiverGroups[ip]
			if !exists {
				group = &IPGroup{
					ID:     fmt.Sprintf("receiver_group_%s", ip),
					IP:     ip,
					Type:   "receiver_group",
					Status: "offline",
					Ports:  []PortInfo{},
				}
				receiverGroups[ip] = group
			}

			// Aggregate metrics
			group.ClientCount++
			group.PacketsReceived += client.PacketsReceived
			group.BytesReceived += client.BytesReceived
			group.DecodeSuccessCount += client.DecodeSuccessCount
			group.DecodeFailureCount += client.DecodeFailureCount
			if client.EncryptionEnabled {
				group.EncryptionEnabled = true
			}
			if client.DetectedSourceIP != "" {
				group.DetectedSourceIP = client.DetectedSourceIP
			}
			if client.LastSeen.After(group.LastSeen) {
				group.LastSeen = client.LastSeen
			}
			if client.Status == "online" {
				group.Status = "online"
			}

			// Calculate weighted average latency
			totalPackets := group.PacketsReceived
			if totalPackets > 0 {
				prevWeight := float64(totalPackets - client.PacketsReceived)
				currWeight := float64(client.PacketsReceived)
				group.AvgLatencyMs = (group.AvgLatencyMs*prevWeight + client.AvgLatencyMs*currWeight) / float64(totalPackets)
			}

			// Add port info
			group.Ports = append(group.Ports, PortInfo{
				Port:            client.ListenPort,
				ClientID:        client.ID,
				PacketsReceived: client.PacketsReceived,
				AvgLatencyMs:    client.AvgLatencyMs,
				Status:          client.Status,
			})
			group.PortCount = len(group.Ports)
		}
	}

	// Sort ports by packets (descending) and limit to top 10
	for _, group := range senderGroups {
		sortPortsByPacketsSent(group.Ports)
		if len(group.Ports) > 10 {
			group.Ports = group.Ports[:10]
		}
	}
	for _, group := range receiverGroups {
		sortPortsByPacketsReceived(group.Ports)
		if len(group.Ports) > 10 {
			group.Ports = group.Ports[:10]
		}
	}

	// Convert maps to slices
	var groups []IPGroup
	for _, g := range senderGroups {
		groups = append(groups, *g)
	}
	for _, g := range receiverGroups {
		groups = append(groups, *g)
	}

	// Create grouped edges based on IP matching
	edges := s.correlateGroupedConnections(senderGroups, receiverGroups)

	return GroupedNetworkTopology{
		Groups:    groups,
		Edges:     edges,
		Timestamp: time.Now(),
	}
}

// correlateGroupedConnections creates edges between IP groups
func (s *DashboardServer) correlateGroupedConnections(senderGroups, receiverGroups map[string]*IPGroup) []Edge {
	var edges []Edge

	for _, senderGroup := range senderGroups {
		for _, receiverGroup := range receiverGroups {
			// Match if receiver detected packets from this sender's IP group
			if receiverGroup.DetectedSourceIP != "" && receiverGroup.DetectedSourceIP == senderGroup.IP {
				packetLoss := 0.0
				if senderGroup.PacketsSent > 0 {
					packetLoss = 1.0 - float64(receiverGroup.PacketsReceived)/float64(senderGroup.PacketsSent)
					if packetLoss < 0 {
						packetLoss = 0
					}
				}

				edges = append(edges, Edge{
					From:            senderGroup.ID,
					To:              receiverGroup.ID,
					PacketsSent:     senderGroup.PacketsSent,
					PacketsReceived: receiverGroup.PacketsReceived,
					PacketLossRate:  packetLoss,
					AvgLatencyMs:    receiverGroup.AvgLatencyMs,
					IsMirror:        false,
				})
			}
		}
	}

	return edges
}

// sortPortsByPacketsSent sorts ports by packets sent descending
func sortPortsByPacketsSent(ports []PortInfo) {
	for i := 0; i < len(ports)-1; i++ {
		for j := i + 1; j < len(ports); j++ {
			if ports[j].PacketsSent > ports[i].PacketsSent {
				ports[i], ports[j] = ports[j], ports[i]
			}
		}
	}
}

// sortPortsByPacketsReceived sorts ports by packets received descending
func sortPortsByPacketsReceived(ports []PortInfo) {
	for i := 0; i < len(ports)-1; i++ {
		for j := i + 1; j < len(ports); j++ {
			if ports[j].PacketsReceived > ports[i].PacketsReceived {
				ports[i], ports[j] = ports[j], ports[i]
			}
		}
	}
}

// handleWebSocket manages WebSocket connections for live updates
func (s *DashboardServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Register client
	s.wsMutex.Lock()
	s.wsClients[conn] = true
	s.wsMutex.Unlock()

	log.Printf("WebSocket client connected (total: %d)", len(s.wsClients))

	// Send full topology on connect
	topology := s.buildNetworkTopology()
	msg := WebSocketMessage{Type: "full_topology", Data: topology}
	if data, err := json.Marshal(msg); err == nil {
		conn.WriteMessage(websocket.TextMessage, data)
	}

	// Handle incoming messages (mainly for keepalive)
	defer func() {
		s.wsMutex.Lock()
		delete(s.wsClients, conn)
		s.wsMutex.Unlock()
		conn.Close()
		log.Printf("WebSocket client disconnected (remaining: %d)", len(s.wsClients))
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// buildClientFromMetrics converts raw metrics to Client struct
func (s *DashboardServer) buildClientFromMetrics(metrics map[string]interface{}, clientType string) Client {
	client := Client{
		ID:       getString(metrics, "client_id"),
		Type:     clientType,
		Status:   "online",
		LastSeen: time.Now(),
	}

	if clientType == "sender" {
		client.SourceIP = getString(metrics, "source_ip")
		client.LocalIP = getString(metrics, "local_ip")
		client.ExternalIP = getString(metrics, "external_ip")
		client.DestinationIP = getString(metrics, "destination_ip")
		client.DestinationPort = getInt(metrics, "destination_port")
		client.PacketsSent = getInt64(metrics, "packets_sent")
		client.BytesSent = getInt64(metrics, "bytes_sent")
		client.EncryptionEnabled = getBool(metrics, "encryption_enabled")
	} else {
		client.ListenIP = getString(metrics, "listen_ip")
		client.LocalIP = getString(metrics, "local_ip")
		client.ExternalIP = getString(metrics, "external_ip")
		client.ListenPort = getInt(metrics, "listen_port")
		client.DetectedSourceIP = getString(metrics, "detected_source_ip")
		client.PacketsReceived = getInt64(metrics, "packets_received")
		client.BytesReceived = getInt64(metrics, "bytes_received")
		client.EncryptionEnabled = getBool(metrics, "encryption_enabled")
		client.DecodeSuccessCount = getInt64(metrics, "decode_success_count")
		client.DecodeFailureCount = getInt64(metrics, "decode_failure_count")
		client.AvgLatencyMs = getFloat64(metrics, "avg_latency_ms")
	}

	return client
}

// buildNetworkTopology constructs the full network state from Redis
func (s *DashboardServer) buildNetworkTopology() NetworkTopology {
	topology := NetworkTopology{
		Nodes:     []Client{},
		Edges:     []Edge{},
		Timestamp: time.Now(),
	}

	// Get all client keys
	keys, err := s.redisClient.Keys(s.ctx, "client:*").Result()
	if err != nil {
		log.Printf("Error getting client keys: %v", err)
		return topology
	}

	var senders []Client
	var receivers []Client

	for _, key := range keys {
		data, err := s.redisClient.HGetAll(s.ctx, key).Result()
		if err != nil {
			continue
		}

		metricsJSON := data["latest_metrics"]
		if metricsJSON == "" {
			continue
		}

		var metrics map[string]interface{}
		if err := json.Unmarshal([]byte(metricsJSON), &metrics); err != nil {
			continue
		}

		clientType := data["type"]
		client := s.buildClientFromMetrics(metrics, clientType)
		client.Status = data["status"]

		if lastSeen, err := time.Parse(time.RFC3339, data["last_seen"]); err == nil {
			client.LastSeen = lastSeen
		}

		topology.Nodes = append(topology.Nodes, client)

		if clientType == "sender" {
			senders = append(senders, client)
		} else {
			receivers = append(receivers, client)
		}
	}

	// Calculate edges based on source IP matching
	topology.Edges = s.correlateConnections(senders, receivers)

	return topology
}

// correlateConnections matches senders to receivers based on IP
func (s *DashboardServer) correlateConnections(senders, receivers []Client) []Edge {
	var edges []Edge

	for _, sender := range senders {
		for _, receiver := range receivers {
			// Match if receiver detected packets from this sender's source IP
			if receiver.DetectedSourceIP != "" && receiver.DetectedSourceIP == sender.SourceIP {
				packetLoss := 0.0
				if sender.PacketsSent > 0 {
					packetLoss = 1.0 - float64(receiver.PacketsReceived)/float64(sender.PacketsSent)
					if packetLoss < 0 {
						packetLoss = 0
					}
				}

				edges = append(edges, Edge{
					From:            sender.ID,
					To:              receiver.ID,
					PacketsSent:     sender.PacketsSent,
					PacketsReceived: receiver.PacketsReceived,
					PacketLossRate:  packetLoss,
					AvgLatencyMs:    receiver.AvgLatencyMs,
					IsMirror:        false,
				})
			}
		}
	}

	// Detect mirrors: multiple receivers from same sender with similar packet counts
	s.detectMirrors(edges)

	return edges
}

// detectMirrors marks edges as mirrors if multiple receivers get similar traffic from same sender
func (s *DashboardServer) detectMirrors(edges []Edge) {
	// Group by sender
	bySender := make(map[string][]*Edge)
	for i := range edges {
		bySender[edges[i].From] = append(bySender[edges[i].From], &edges[i])
	}

	// If sender has multiple receivers, check for mirrors
	for _, receiverEdges := range bySender {
		if len(receiverEdges) <= 1 {
			continue
		}

		// Check if packet counts are similar (within 5%)
		basePackets := receiverEdges[0].PacketsReceived
		for i := 1; i < len(receiverEdges); i++ {
			if basePackets == 0 {
				continue
			}
			ratio := float64(receiverEdges[i].PacketsReceived) / float64(basePackets)
			if ratio > 0.95 && ratio < 1.05 {
				receiverEdges[i].IsMirror = true
			}
		}
	}
}

// broadcastUpdate sends an update to all WebSocket clients
func (s *DashboardServer) broadcastUpdate(msgType string, data interface{}) {
	msg := WebSocketMessage{Type: msgType, Data: data}
	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return
	}

	s.wsMutex.RLock()
	defer s.wsMutex.RUnlock()

	for conn := range s.wsClients {
		if err := conn.WriteMessage(websocket.TextMessage, msgJSON); err != nil {
			log.Printf("WebSocket write error: %v", err)
		}
	}
}

// updateAndBroadcastEdges recalculates edges and broadcasts them
func (s *DashboardServer) updateAndBroadcastEdges() {
	topology := s.buildNetworkTopology()
	for _, edge := range topology.Edges {
		s.broadcastUpdate("edge_update", edge)
	}
}

// runOfflineDetection periodically checks for offline clients
func (s *DashboardServer) runOfflineDetection(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkOfflineClients()
		}
	}
}

// checkOfflineClients marks clients as offline if no heartbeat received
func (s *DashboardServer) checkOfflineClients() {
	keys, err := s.redisClient.Keys(s.ctx, "client:*").Result()
	if err != nil {
		return
	}

	timeout := time.Duration(s.config.ClientTimeoutSeconds) * time.Second

	for _, key := range keys {
		data, err := s.redisClient.HGetAll(s.ctx, key).Result()
		if err != nil {
			continue
		}

		lastSeenStr := data["last_seen"]
		currentStatus := data["status"]

		lastSeen, err := time.Parse(time.RFC3339, lastSeenStr)
		if err != nil {
			continue
		}

		if time.Since(lastSeen) > timeout && currentStatus == "online" {
			// Mark as offline
			s.redisClient.HSet(s.ctx, key, "status", "offline")

			clientID := key[7:] // Remove "client:" prefix
			log.Printf("Client %s marked offline (no heartbeat for %v)", clientID, timeout)

			// Broadcast offline status
			s.broadcastUpdate("node_offline", map[string]interface{}{
				"id":        clientID,
				"last_seen": lastSeenStr,
			})
		}
	}
}

// runMetricsCleanup periodically removes old metrics history
func (s *DashboardServer) runMetricsCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupOldMetrics()
		}
	}
}

// cleanupOldMetrics removes metrics older than retention period
func (s *DashboardServer) cleanupOldMetrics() {
	keys, err := s.redisClient.Keys(s.ctx, "metrics:*").Result()
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-time.Duration(s.config.MetricsRetentionHours) * time.Hour)
	cutoffScore := float64(cutoff.UnixMilli())

	for _, key := range keys {
		removed, err := s.redisClient.ZRemRangeByScore(s.ctx, key, "-inf", fmt.Sprintf("%f", cutoffScore)).Result()
		if err == nil && removed > 0 {
			log.Printf("Cleaned up %d old metrics entries from %s", removed, key)
		}
	}
}

// Helper functions for type conversion
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

func getInt64(m map[string]interface{}, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

func getFloat64(m map[string]interface{}, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0.0
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

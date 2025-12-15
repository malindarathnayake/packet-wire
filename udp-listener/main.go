package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// TimestampPrefix marks packets with embedded timestamp for latency calculation
	TimestampPrefix    = "TIMESTAMP|"
	TimestampDelimiter = "|"

	// TestMessagePrefix marks test packets with sequence numbers
	// Format: TEST|{seq_num}|{payload}
	TestMessagePrefix = "TEST|"

	// ACK format for enhanced reply mode
	// Format: ACK|{seq_num}|{receive_timestamp_ns}|{id_name}
	AckPrefix = "ACK|"
)

type Config struct {
	ListenAddr           string `json:"listen_addr"`
	CaptureDir           string `json:"capture_dir"`
	BufferSize           int    `json:"buffer_size"`
	ReplyMode            bool   `json:"reply_mode"`
	IDName               string `json:"id_name"`
	AckMessage           string `json:"ack_message"`
	EncryptionPassphrase string `json:"encryption_passphrase"`
	FileReceiveMode      bool   `json:"file_receive_mode"`
	FileReceiveDir       string `json:"file_receive_dir"`

	// Dashboard metrics configuration
	ClientID              string `json:"client_id"`
	DashboardURL          string `json:"dashboard_url"`
	HeartbeatIntervalSecs int    `json:"heartbeat_interval_seconds"`
	ListenIP              string `json:"listen_ip"`
	LocalIP               string `json:"local_ip"`
	ExternalIP            string `json:"external_ip"`

	// Test mode configuration
	TestMode       bool   `json:"test_mode"`        // Enable detailed test message tracking
	TestReportFile string `json:"test_report_file"` // Output file for test report (JSON)
	EnhancedAck    bool   `json:"enhanced_ack"`     // Use structured ACK format: ACK|seq|ts|id
}

// Metrics tracks runtime statistics with thread-safe access
type Metrics struct {
	PacketsReceived    int64
	BytesReceived      int64
	DecodeSuccessCount int64
	DecodeFailureCount int64
	TotalLatencyNs     int64 // Sum of all latencies for averaging
	LatencySampleCount int64
	LastReceiveTime    time.Time
	DetectedSourceIP   string // Detected from incoming packets
	mu                 sync.RWMutex
}

// TestRecord tracks a single test message for drop analysis
type TestRecord struct {
	SeqNum          int64     `json:"seq_num"`
	ReceiveTime     time.Time `json:"receive_time"`
	AckTime         time.Time `json:"ack_time"`
	SourceAddr      string    `json:"source_addr"`
	SenderTimestamp int64     `json:"sender_timestamp_ns,omitempty"` // From TIMESTAMP prefix
	LatencyMs       float64   `json:"latency_ms,omitempty"`
	PayloadSize     int       `json:"payload_size"`
	AckSent         bool      `json:"ack_sent"`
	Message         string    `json:"message,omitempty"` // Optional: first 100 chars
}

// TestReport is the output format for test-mode analysis
type TestReport struct {
	ListenerID       string       `json:"listener_id"`
	ListenAddr       string       `json:"listen_addr"`
	StartTime        time.Time    `json:"start_time"`
	EndTime          time.Time    `json:"end_time"`
	TotalReceived    int64        `json:"total_received"`
	TotalAcked       int64        `json:"total_acked"`
	FirstSeq         int64        `json:"first_seq"`
	LastSeq          int64        `json:"last_seq"`
	ExpectedCount    int64        `json:"expected_count,omitempty"` // LastSeq - FirstSeq + 1 if sequential
	MissingSeqs      []int64      `json:"missing_seqs,omitempty"`   // Detected gaps
	AvgLatencyMs     float64      `json:"avg_latency_ms"`
	MinLatencyMs     float64      `json:"min_latency_ms"`
	MaxLatencyMs     float64      `json:"max_latency_ms"`
	Records          []TestRecord `json:"records"`
}

// TestTracker manages test message tracking
type TestTracker struct {
	records   []TestRecord
	seqMap    map[int64]bool // Track seen sequence numbers for gap detection
	startTime time.Time
	mu        sync.RWMutex
}

// NewTestTracker creates a new test tracker
func NewTestTracker() *TestTracker {
	return &TestTracker{
		records:   make([]TestRecord, 0, 10000),
		seqMap:    make(map[int64]bool),
		startTime: time.Now(),
	}
}

// AddRecord adds a test record
func (t *TestTracker) AddRecord(rec TestRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = append(t.records, rec)
	if rec.SeqNum > 0 {
		t.seqMap[rec.SeqNum] = true
	}
}

// GenerateReport creates the final test report
func (t *TestTracker) GenerateReport(listenerID, listenAddr string) TestReport {
	t.mu.RLock()
	defer t.mu.RUnlock()

	report := TestReport{
		ListenerID:    listenerID,
		ListenAddr:    listenAddr,
		StartTime:     t.startTime,
		EndTime:       time.Now(),
		TotalReceived: int64(len(t.records)),
		Records:       t.records,
	}

	if len(t.records) == 0 {
		return report
	}

	// Calculate statistics
	var totalLatency, minLatency, maxLatency float64
	var latencyCount int64
	var firstSeq, lastSeq int64 = -1, -1
	var ackedCount int64

	for _, rec := range t.records {
		if rec.AckSent {
			ackedCount++
		}
		if rec.LatencyMs > 0 {
			totalLatency += rec.LatencyMs
			latencyCount++
			if minLatency == 0 || rec.LatencyMs < minLatency {
				minLatency = rec.LatencyMs
			}
			if rec.LatencyMs > maxLatency {
				maxLatency = rec.LatencyMs
			}
		}
		if rec.SeqNum > 0 {
			if firstSeq == -1 || rec.SeqNum < firstSeq {
				firstSeq = rec.SeqNum
			}
			if rec.SeqNum > lastSeq {
				lastSeq = rec.SeqNum
			}
		}
	}

	report.TotalAcked = ackedCount
	report.FirstSeq = firstSeq
	report.LastSeq = lastSeq

	if latencyCount > 0 {
		report.AvgLatencyMs = totalLatency / float64(latencyCount)
		report.MinLatencyMs = minLatency
		report.MaxLatencyMs = maxLatency
	}

	// Detect missing sequences (gaps)
	if firstSeq > 0 && lastSeq > firstSeq {
		report.ExpectedCount = lastSeq - firstSeq + 1
		for seq := firstSeq; seq <= lastSeq; seq++ {
			if !t.seqMap[seq] {
				report.MissingSeqs = append(report.MissingSeqs, seq)
			}
		}
	}

	return report
}

// MetricsPayload is sent to dashboard
type MetricsPayload struct {
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

var (
	cfg         Config
	metrics     = &Metrics{}
	testTracker *TestTracker
	shutdownCtx context.Context
	cancelFunc  context.CancelFunc
)

func main() {
	// Setup shutdown context
	shutdownCtx, cancelFunc = context.WithCancel(context.Background())
	defer cancelFunc()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("received signal %v, initiating shutdown", sig)
		cancelFunc()
	}()

	cfg = loadConfig()

	epoch := time.Now().Unix()
	filename := fmt.Sprintf("%d_udp_capture.csv", epoch)

	captureDir := cfg.CaptureDir
	outputPath := filepath.Join(captureDir, filename)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		log.Fatalf("failed to create capture directory %q: %v", filepath.Dir(outputPath), err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		log.Fatalf("failed to create capture file %q: %v", outputPath, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("failed to close capture file: %v", err)
		}
	}()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"timestamp", "remote_addr", "size_bytes", "latency_ms", "message"}); err != nil {
		log.Fatalf("failed to write CSV header: %v", err)
	}
	writer.Flush()

	var fileReceiver *FileReceiver
	if cfg.FileReceiveMode {
		dir := cfg.FileReceiveDir
		if dir == "" {
			dir = cfg.CaptureDir
		}
		var err error
		fileReceiver, err = NewFileReceiver(dir)
		if err != nil {
			log.Fatalf("failed to initialize file receiver: %v", err)
		}
		log.Printf("file receive mode enabled: files will be written under %s", dir)
	}

	addr, err := net.ResolveUDPAddr("udp", cfg.ListenAddr)
	if err != nil {
		log.Fatalf("failed to resolve UDP address %q: %v", cfg.ListenAddr, err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("failed to listen on UDP address %q: %v", cfg.ListenAddr, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("failed to close UDP listener: %v", err)
		}
	}()

	log.Printf("listening for UDP packets on %s, writing CSV to %s", cfg.ListenAddr, outputPath)
	if cfg.ReplyMode {
		if cfg.EnhancedAck {
			log.Printf("reply mode enabled: enhanced ACK format (ACK|seq|ts|%s)", cfg.IDName)
		} else {
			log.Printf("reply mode enabled: will respond with %q", cfg.AckMessage)
		}
	}

	// Initialize test tracker if test mode is enabled
	if cfg.TestMode {
		testTracker = NewTestTracker()
		log.Printf("test mode enabled: tracking all messages for drop analysis")
		if cfg.TestReportFile != "" {
			log.Printf("test report will be written to: %s", cfg.TestReportFile)
		}
	}

	// Start dashboard heartbeat if configured
	var wg sync.WaitGroup
	if cfg.DashboardURL != "" && cfg.ClientID != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runHeartbeat(shutdownCtx, addr.Port)
		}()
		log.Printf("dashboard heartbeat enabled: %s (client: %s)", cfg.DashboardURL, cfg.ClientID)
	}

	buf := make([]byte, cfg.BufferSize)

	// Main receive loop with context cancellation
	go func() {
		for {
			select {
			case <-shutdownCtx.Done():
				return
			default:
				// Set read deadline to allow checking shutdown context
				conn.SetReadDeadline(time.Now().Add(1 * time.Second))

				n, remote, err := conn.ReadFromUDP(buf)
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue // Timeout, check shutdown context
					}
					log.Printf("error reading from UDP: %v", err)
					continue
				}

				receiveTime := time.Now()
				rawData := buf[:n]
				plaintext := rawData
				var decryptSuccess bool = true
				var latencyMs float64 = -1

				if cfg.EncryptionPassphrase != "" {
					decrypted, decErr := decryptWithPassphrase(rawData, cfg.EncryptionPassphrase)
					if decErr != nil {
						log.Printf("failed to decrypt packet from %s: %v (storing raw data)", remote, decErr)
						decryptSuccess = false
						atomic.AddInt64(&metrics.DecodeFailureCount, 1)
					} else {
						plaintext = decrypted
						atomic.AddInt64(&metrics.DecodeSuccessCount, 1)
					}
				} else {
					atomic.AddInt64(&metrics.DecodeSuccessCount, 1)
				}

				// Extract timestamp and calculate latency
				originalPayload, senderTimestamp := extractTimestamp(plaintext)
				if senderTimestamp > 0 {
					latencyNs := receiveTime.UnixNano() - senderTimestamp
					latencyMs = float64(latencyNs) / 1e6
					atomic.AddInt64(&metrics.TotalLatencyNs, latencyNs)
					atomic.AddInt64(&metrics.LatencySampleCount, 1)
					plaintext = originalPayload
				}

				// Extract sequence number from TEST| prefix if present
				seqNum := extractSequenceNumber(plaintext)

				// Update metrics
				atomic.AddInt64(&metrics.PacketsReceived, 1)
				atomic.AddInt64(&metrics.BytesReceived, int64(len(rawData)))
				metrics.mu.Lock()
				metrics.LastReceiveTime = receiveTime
				// Track detected source IP (use most recent sender IP)
				if remote != nil {
					metrics.DetectedSourceIP = remote.IP.String()
				}
				metrics.mu.Unlock()

				if fileReceiver != nil {
					if err := fileReceiver.Process(remote.String(), plaintext); err != nil {
						log.Printf("file receive error from %s: %v", remote, err)
					}
				}

				message := string(plaintext)
				latencyStr := "-1"
				if latencyMs >= 0 {
					latencyStr = fmt.Sprintf("%.3f", latencyMs)
				}

				record := []string{
					receiveTime.Format(time.RFC3339Nano),
					remote.String(),
					strconv.Itoa(len(rawData)),
					latencyStr,
					message,
				}

				if err := writer.Write(record); err != nil {
					log.Printf("failed to write CSV record: %v", err)
					continue
				}
				writer.Flush()

				var ackSent bool
				if cfg.ReplyMode && decryptSuccess {
					var ackData []byte
					if cfg.EnhancedAck {
						// Enhanced ACK format: ACK|{seq}|{receive_ts_ns}|{id_name}
						ackData = []byte(fmt.Sprintf("ACK|%d|%d|%s", seqNum, receiveTime.UnixNano(), cfg.IDName))
					} else {
						ackData = []byte(cfg.AckMessage)
					}
					if _, err := conn.WriteToUDP(ackData, remote); err != nil {
						log.Printf("failed to send ACK to %s: %v", remote, err)
					} else {
						ackSent = true
					}
				}

				// Track test record if test mode is enabled
				if testTracker != nil {
					msgPreview := string(plaintext)
					if len(msgPreview) > 100 {
						msgPreview = msgPreview[:100] + "..."
					}
					rec := TestRecord{
						SeqNum:          seqNum,
						ReceiveTime:     receiveTime,
						AckTime:         time.Now(),
						SourceAddr:      remote.String(),
						SenderTimestamp: senderTimestamp,
						LatencyMs:       latencyMs,
						PayloadSize:     len(rawData),
						AckSent:         ackSent,
						Message:         msgPreview,
					}
					testTracker.AddRecord(rec)
				}

				if latencyMs >= 0 {
					log.Printf("received %d bytes from %s (latency: %.2fms)", len(rawData), remote, latencyMs)
				} else {
					log.Printf("received %d bytes from %s", len(rawData), remote)
				}
			}
		}
	}()

	// Wait for shutdown
	<-shutdownCtx.Done()
	log.Println("shutting down...")

	// Write test report if test mode was enabled
	if testTracker != nil {
		writeTestReport()
	}

	// Wait for heartbeat goroutine
	wg.Wait()
	log.Println("shutdown complete")
}

// writeTestReport generates and writes the test report to file
func writeTestReport() {
	report := testTracker.GenerateReport(cfg.IDName, cfg.ListenAddr)

	// Print summary to console
	log.Println("═══════════════════════════════════════════════════════════════")
	log.Println("                    TEST MODE SUMMARY")
	log.Println("═══════════════════════════════════════════════════════════════")
	log.Printf("  Total Received:  %d packets", report.TotalReceived)
	log.Printf("  Total ACKed:     %d packets", report.TotalAcked)
	if report.FirstSeq > 0 {
		log.Printf("  Sequence Range:  %d - %d", report.FirstSeq, report.LastSeq)
		log.Printf("  Expected Count:  %d", report.ExpectedCount)
		if len(report.MissingSeqs) > 0 {
			log.Printf("  Missing Seqs:    %d (DROPS DETECTED)", len(report.MissingSeqs))
			// Show first 10 missing sequences
			showCount := len(report.MissingSeqs)
			if showCount > 10 {
				showCount = 10
			}
			log.Printf("  First Missing:   %v...", report.MissingSeqs[:showCount])
		} else {
			log.Printf("  Missing Seqs:    0 (no drops detected)")
		}
	}
	if report.AvgLatencyMs > 0 {
		log.Printf("  Latency (avg):   %.3f ms", report.AvgLatencyMs)
		log.Printf("  Latency (min):   %.3f ms", report.MinLatencyMs)
		log.Printf("  Latency (max):   %.3f ms", report.MaxLatencyMs)
	}
	log.Println("═══════════════════════════════════════════════════════════════")

	// Write to file if configured
	reportFile := cfg.TestReportFile
	if reportFile == "" {
		// Generate default filename
		reportFile = filepath.Join(cfg.CaptureDir, fmt.Sprintf("%d_test_report.json", time.Now().Unix()))
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Printf("failed to marshal test report: %v", err)
		return
	}

	if err := os.MkdirAll(filepath.Dir(reportFile), 0o755); err != nil {
		log.Printf("failed to create report directory: %v", err)
		return
	}

	if err := os.WriteFile(reportFile, data, 0o644); err != nil {
		log.Printf("failed to write test report: %v", err)
		return
	}

	log.Printf("test report written to: %s", reportFile)
}

func loadConfig() Config {
	configFile := os.Getenv("CONFIG_FILE")

	if configFile != "" {
		if data, err := os.ReadFile(configFile); err == nil {
			var cfg Config
			if err := json.Unmarshal(data, &cfg); err == nil {
				log.Printf("loaded configuration from %s", configFile)
				applyConfigDefaults(&cfg)
				return cfg
			}
			log.Printf("failed to parse config file %q: %v, falling back to environment variables", configFile, err)
		} else {
			log.Printf("failed to read config file %q: %v, falling back to environment variables", configFile, err)
		}
	}

	cfg := Config{
		ListenAddr:            getEnv("UDP_LISTEN_ADDR", ":9000"),
		CaptureDir:            getEnv("CAPTURE_DIR", "captures"),
		BufferSize:            getEnvInt("UDP_BUFFER_SIZE", 2048),
		ReplyMode:             getEnv("REPLY_MODE", "false") == "true",
		IDName:                getEnv("ID_NAME", "UNKNOWN"),
		EncryptionPassphrase:  getEnv("ENCRYPTION_PASSPHRASE", ""),
		FileReceiveMode:       getEnv("FILE_RECEIVE_MODE", "false") == "true",
		FileReceiveDir:        getEnv("FILE_RECEIVE_DIR", ""),
		ClientID:              getEnv("CLIENT_ID", ""),
		DashboardURL:          getEnv("DASHBOARD_URL", ""),
		HeartbeatIntervalSecs: getEnvInt("HEARTBEAT_INTERVAL", 5),
		ListenIP:              getEnv("LISTEN_IP", ""),
		LocalIP:               getEnv("LOCAL_IP", ""),
		ExternalIP:            getEnv("EXTERNAL_IP", ""),
		TestMode:              getEnv("TEST_MODE", "false") == "true",
		TestReportFile:        getEnv("TEST_REPORT_FILE", ""),
		EnhancedAck:           getEnv("ENHANCED_ACK", "false") == "true",
	}

	applyConfigDefaults(&cfg)
	return cfg
}

func applyConfigDefaults(cfg *Config) {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 2048
	}
	if cfg.AckMessage == "" {
		cfg.AckMessage = fmt.Sprintf("ACK-PLTEST_%s", cfg.IDName)
	}
	cfg.AckMessage = processPlaceholders(cfg.AckMessage, cfg.IDName)
	if cfg.CaptureDir == "" {
		cfg.CaptureDir = "captures"
	}
	if cfg.FileReceiveDir == "" {
		cfg.FileReceiveDir = cfg.CaptureDir
	}
	if cfg.HeartbeatIntervalSecs <= 0 {
		cfg.HeartbeatIntervalSecs = 5
	}
}

func processPlaceholders(message, idName string) string {
	message = strings.ReplaceAll(message, "{id_name}", idName)
	return message
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

// extractTimestamp extracts the sender timestamp from the payload
// Returns the original payload (without timestamp prefix) and the timestamp in nanoseconds
func extractTimestamp(data []byte) ([]byte, int64) {
	if !bytes.HasPrefix(data, []byte(TimestampPrefix)) {
		return data, 0
	}

	// Find the second delimiter after the timestamp
	afterPrefix := data[len(TimestampPrefix):]
	delimIdx := bytes.Index(afterPrefix, []byte(TimestampDelimiter))
	if delimIdx < 0 {
		return data, 0
	}

	timestampStr := string(afterPrefix[:delimIdx])
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return data, 0
	}

	// Return the original payload after the timestamp prefix
	originalPayload := afterPrefix[delimIdx+1:]
	return originalPayload, timestamp
}

// extractSequenceNumber extracts sequence number from TEST|{seq}|{payload} format
// Also tries to extract from JSON payloads with "seq" or "sequence" fields
// Returns 0 if no sequence number found
func extractSequenceNumber(data []byte) int64 {
	// Try TEST| prefix format first
	if bytes.HasPrefix(data, []byte(TestMessagePrefix)) {
		afterPrefix := data[len(TestMessagePrefix):]
		delimIdx := bytes.Index(afterPrefix, []byte("|"))
		if delimIdx > 0 {
			seqStr := string(afterPrefix[:delimIdx])
			if seq, err := strconv.ParseInt(seqStr, 10, 64); err == nil {
				return seq
			}
		}
	}

	// Try to extract from JSON payload
	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err == nil {
		// Try common sequence field names
		for _, key := range []string{"seq", "sequence", "seq_num", "seqnum", "packet_num", "id"} {
			if val, ok := jsonData[key]; ok {
				switch v := val.(type) {
				case float64:
					return int64(v)
				case int64:
					return v
				case string:
					if seq, err := strconv.ParseInt(v, 10, 64); err == nil {
						return seq
					}
				}
			}
		}
	}

	return 0
}

func decryptWithPassphrase(ciphertext []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return ciphertext, nil
	}

	if len(ciphertext) == 0 {
		return ciphertext, nil
	}

	key := sha256.Sum256([]byte(passphrase))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// runHeartbeat sends periodic metrics to the dashboard
func runHeartbeat(ctx context.Context, listenPort int) {
	interval := time.Duration(cfg.HeartbeatIntervalSecs) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send initial heartbeat
	sendHeartbeat(listenPort)

	for {
		select {
		case <-ctx.Done():
			log.Println("heartbeat stopped")
			return
		case <-ticker.C:
			sendHeartbeat(listenPort)
		}
	}
}

func sendHeartbeat(listenPort int) {
	metrics.mu.RLock()
	lastReceive := metrics.LastReceiveTime
	detectedSource := metrics.DetectedSourceIP
	metrics.mu.RUnlock()

	// Calculate average latency
	var avgLatencyMs float64
	sampleCount := atomic.LoadInt64(&metrics.LatencySampleCount)
	if sampleCount > 0 {
		totalLatency := atomic.LoadInt64(&metrics.TotalLatencyNs)
		avgLatencyMs = float64(totalLatency) / float64(sampleCount) / 1e6
	}

	payload := MetricsPayload{
		ClientID:           cfg.ClientID,
		ClientType:         "receiver",
		Timestamp:          time.Now(),
		ListenIP:           cfg.ListenIP,
		LocalIP:            cfg.LocalIP,
		ExternalIP:         cfg.ExternalIP,
		ListenPort:         listenPort,
		DetectedSourceIP:   detectedSource,
		PacketsReceived:    atomic.LoadInt64(&metrics.PacketsReceived),
		BytesReceived:      atomic.LoadInt64(&metrics.BytesReceived),
		EncryptionEnabled:  cfg.EncryptionPassphrase != "",
		DecodeSuccessCount: atomic.LoadInt64(&metrics.DecodeSuccessCount),
		DecodeFailureCount: atomic.LoadInt64(&metrics.DecodeFailureCount),
		AvgLatencyMs:       avgLatencyMs,
		LastReceiveTime:    lastReceive,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("failed to marshal metrics: %v", err)
		return
	}

	// Send with retry and exponential backoff
	if err := sendWithRetry(cfg.DashboardURL, data); err != nil {
		log.Printf("failed to send heartbeat: %v", err)
	}
}

// sendWithRetry sends HTTP POST with exponential backoff (per engineering standards)
func sendWithRetry(url string, data []byte) error {
	maxRetries := 3
	baseDelay := 5 * time.Second

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter
			delay := baseDelay * time.Duration(attempt)
			jitter := time.Duration(float64(delay) * 0.2 * (0.5 - float64(time.Now().UnixNano()%1000)/1000))
			time.Sleep(delay + jitter)
		}

		ctx, cancel := context.WithTimeout(shutdownCtx, 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("create request: %w", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("send request: %w", err)
			log.Printf("heartbeat attempt %d failed: %v", attempt+1, err)
			continue
		}

		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		log.Printf("heartbeat attempt %d returned %d", attempt+1, resp.StatusCode)
	}

	return fmt.Errorf("all %d retries failed: %w", maxRetries, lastErr)
}

type FileReceiver struct {
	dir      string
	sessions map[string]*fileSession
}

type fileSession struct {
	file          *os.File
	path          string
	expectedSize  int64
	bytesReceived int64
}

func NewFileReceiver(dir string) (*FileReceiver, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create file receive dir %q: %w", dir, err)
	}

	return &FileReceiver{
		dir:      dir,
		sessions: make(map[string]*fileSession),
	}, nil
}

func (fr *FileReceiver) Process(remote string, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	session, ok := fr.sessions[remote]
	if !ok {
		// Look for a FILE_START header.
		line := strings.TrimSpace(string(data))
		if !strings.HasPrefix(line, "FILE_START ") {
			return nil
		}

		parts := strings.Fields(line)
		if len(parts) < 3 {
			return fmt.Errorf("invalid FILE_START from %s: %q", remote, line)
		}

		filename := parts[1]
		size, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid FILE_START size from %s: %q", remote, line)
		}

		path, err := fr.uniquePath(filename)
		if err != nil {
			return fmt.Errorf("determine unique path: %w", err)
		}

		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create file %q: %w", path, err)
		}

		fr.sessions[remote] = &fileSession{
			file:         f,
			path:         path,
			expectedSize: size,
		}

		log.Printf("file receive: started new file from %s -> %s (expected %d bytes)", remote, path, size)
		return nil
	}

	written, err := session.file.Write(data)
	if err != nil {
		_ = fr.abort(remote)
		return fmt.Errorf("write file %q: %w", session.path, err)
	}

	session.bytesReceived += int64(written)

	if session.bytesReceived >= session.expectedSize {
		if closeErr := session.file.Close(); closeErr != nil {
			log.Printf("file receive: failed to close file %q: %v", session.path, closeErr)
		}
		delete(fr.sessions, remote)
		log.Printf("file receive: completed file from %s -> %s (%d bytes)", remote, session.path, session.bytesReceived)
	}

	return nil
}

func (fr *FileReceiver) abort(remote string) error {
	session, ok := fr.sessions[remote]
	if !ok {
		return nil
	}
	delete(fr.sessions, remote)

	if session.file != nil {
		if err := session.file.Close(); err != nil {
			log.Printf("file receive: failed to close file %q on abort: %v", session.path, err)
		}
	}

	return nil
}

func (fr *FileReceiver) uniquePath(filename string) (string, error) {
	base := filepath.Base(filename)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "received_file"
	}

	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	try := filepath.Join(fr.dir, base)
	if _, err := os.Stat(try); err != nil {
		if os.IsNotExist(err) {
			return try, nil
		}
		return "", err
	}

	for i := 1; ; i++ {
		candidate := filepath.Join(fr.dir, fmt.Sprintf("%s_%d%s", name, i, ext))
		if _, err := os.Stat(candidate); err != nil {
			if os.IsNotExist(err) {
				return candidate, nil
			}
			return "", err
		}
	}
}

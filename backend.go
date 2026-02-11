package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	pb "github.com/GalitskyKK/nekkus-core/pkg/protocol"
	"golang.org/x/sys/windows/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
)

const (
	defaultModuleID    = "com.nekkus.vpn"
	defaultVersion     = "0.1.0"
	defaultListenAddr  = "127.0.0.1:50061"
	defaultHTTPAddr    = "127.0.0.1:8081"
	defaultDataDir     = "data"
	defaultHTTPTimeout = 10 * time.Second
	defaultProxyListen = "127.0.0.1"
	defaultProxyPort   = 7890
)

type BackendOptions struct {
	Mode      string
	GRPCAddr  string
	HTTPAddr  string
	DataDir   string
	HubAddr   string
	ModuleID  string
	Version   string
	StartHTTP bool
}

type vpnConfig struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Content        string `json:"content"`
	SourceURL      string `json:"source_url,omitempty"`
	SubscriptionID string `json:"subscription_id,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

type configStore struct {
	mu       sync.RWMutex
	filePath string
	configs  []vpnConfig
}

func newConfigStore(filePath string) *configStore {
	return &configStore{filePath: filePath, configs: []vpnConfig{}}
}

func (s *configStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var configs []vpnConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return err
	}

	s.mu.Lock()
	s.configs = configs
	s.mu.Unlock()
	return nil
}

type vpnSettings struct {
	DefaultConfigID string `json:"default_config_id"`
	DefaultServer   string `json:"default_server"`
}

type settingsStore struct {
	mu       sync.RWMutex
	filePath string
	settings vpnSettings
}

func newSettingsStore(filePath string) *settingsStore {
	return &settingsStore{
		filePath: filePath,
		settings: vpnSettings{},
	}
}

func (s *settingsStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var settings vpnSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = settings
	s.mu.Unlock()
	return nil
}

func (s *settingsStore) snapshot() vpnSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *settingsStore) setDefaults(configID, server string) error {
	s.mu.Lock()
	s.settings.DefaultConfigID = configID
	s.settings.DefaultServer = server
	updated := s.settings
	s.mu.Unlock()

	data, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0o600)
}

func (s *configStore) save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.configs, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0o600)
}

func (s *configStore) add(config vpnConfig) (vpnConfig, error) {
	s.mu.Lock()
	s.configs = append(s.configs, config)
	s.mu.Unlock()

	return config, s.save()
}

func (s *configStore) updateContent(configID, content string) (vpnConfig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, config := range s.configs {
		if config.ID == configID {
			config.Content = content
			config.UpdatedAt = time.Now().Unix()
			s.configs[index] = config
			return config, true, s.save()
		}
	}

	return vpnConfig{}, false, nil
}

func (s *configStore) list() []vpnConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]vpnConfig, len(s.configs))
	copy(result, s.configs)
	return result
}

func (s *configStore) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.configs)
}

func (s *configStore) getByID(configID string) (vpnConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, config := range s.configs {
		if config.ID == configID {
			return config, true
		}
	}
	return vpnConfig{}, false
}

type subscription struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	ConfigID    string `json:"config_id"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	LastError   string `json:"last_error,omitempty"`
	LastSuccess int64  `json:"last_success,omitempty"`
}

type subscriptionStore struct {
	mu       sync.RWMutex
	filePath string
	items    []subscription
}

func newSubscriptionStore(filePath string) *subscriptionStore {
	return &subscriptionStore{filePath: filePath, items: []subscription{}}
}

func (s *subscriptionStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var items []subscription
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}

	s.mu.Lock()
	s.items = items
	s.mu.Unlock()
	return nil
}

func (s *subscriptionStore) save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.items, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0o600)
}

func (s *subscriptionStore) add(item subscription) (subscription, error) {
	s.mu.Lock()
	s.items = append(s.items, item)
	s.mu.Unlock()

	return item, s.save()
}

func (s *subscriptionStore) list() []subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]subscription, len(s.items))
	copy(result, s.items)
	return result
}

func (s *subscriptionStore) updateStatus(id, lastError string, lastSuccess int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for index, item := range s.items {
		if item.ID == id {
			item.LastError = lastError
			item.LastSuccess = lastSuccess
			item.UpdatedAt = time.Now().Unix()
			s.items[index] = item
			return s.save()
		}
	}
	return nil
}

func (s *subscriptionStore) findByID(id string) (subscription, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, item := range s.items {
		if item.ID == id {
			return item, true
		}
	}
	return subscription{}, false
}

type vpnState struct {
	mu             sync.RWMutex
	connected      bool
	serverName     string
	activeConfigID string
	configCount    int
	engineRunning  bool
	engineError    string
	proxyAddress   string
	downloadSpeed  int64
	uploadSpeed    int64
	totalDownload  int64
	totalUpload    int64
}

func (s *vpnState) snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"connected":      s.connected,
		"server":         s.serverName,
		"activeConfigId": s.activeConfigID,
		"configCount":    s.configCount,
		"engineRunning":  s.engineRunning,
		"engineError":    s.engineError,
		"proxyAddress":   s.proxyAddress,
		"downloadSpeed":  s.downloadSpeed,
		"uploadSpeed":    s.uploadSpeed,
		"totalDownload":  s.totalDownload,
		"totalUpload":    s.totalUpload,
		"lastUpdateUnix": time.Now().Unix(),
	}
}

func (s *vpnState) connect(serverName, configID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if serverName == "" {
		serverName = "auto"
	}

	s.connected = true
	s.serverName = serverName
	s.activeConfigID = configID
	s.downloadSpeed = 12_300_000
	s.uploadSpeed = 1_200_000
}

func (s *vpnState) disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = false
	s.serverName = ""
	s.activeConfigID = ""
	s.downloadSpeed = 0
	s.uploadSpeed = 0
}

func (s *vpnState) setConfigCount(count int) {
	s.mu.Lock()
	s.configCount = count
	s.mu.Unlock()
}

func (s *vpnState) setEngineStatus(running bool, errMsg string, proxyAddr string) {
	s.mu.Lock()
	s.engineRunning = running
	s.engineError = errMsg
	s.proxyAddress = proxyAddr
	s.mu.Unlock()
}

func (s *vpnState) isActive(serverName, configID string) bool {
	normalizedServer := normalizeName(serverName)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected && s.activeConfigID == configID && normalizeName(s.serverName) == normalizedServer
}

func (s *vpnState) isConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

type proxyEngine struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	running   bool
	lastError string
}

func (e *proxyEngine) Start(binaryPath, configPath string, buffer *logBuffer) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running && e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		e.running = false
	}

	cmd := exec.Command(binaryPath, "run", "-c", configPath)
	stdout, stderr, pipeErr := attachSingBoxPipes(cmd)
	if pipeErr != nil {
		e.lastError = pipeErr.Error()
		return pipeErr
	}
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}

	if err := cmd.Start(); err != nil {
		e.lastError = err.Error()
		return err
	}

	e.cmd = cmd
	e.running = true
	e.lastError = ""

	go streamSingBoxLogs(stdout, buffer)
	go streamSingBoxLogs(stderr, buffer)

	go func() {
		err := cmd.Wait()
		e.mu.Lock()
		e.running = false
		if err != nil {
			e.lastError = err.Error()
		}
		e.mu.Unlock()
	}()

	return nil
}

func (e *proxyEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
	e.running = false
}

func (e *proxyEngine) LastError() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastError
}

type moduleServer struct {
	pb.UnimplementedModuleServiceServer
	state    *vpnState
	store    *configStore
	subs     *subscriptionStore
	settings *settingsStore
	engine   *proxyEngine
	dataDir  string
	logs     *logBuffer
}

func (s *moduleServer) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PongResponse, error) {
	status := pb.ModuleStatus_IDLE
	s.state.mu.RLock()
	if s.state.connected {
		status = pb.ModuleStatus_RUNNING
	}
	s.state.mu.RUnlock()

	return &pb.PongResponse{
		Timestamp: time.Now().Unix(),
		Status:    status,
	}, nil
}

func (s *moduleServer) GetWidgetData(ctx context.Context, req *pb.WidgetRequest) (*pb.WidgetDataResponse, error) {
	s.state.setConfigCount(s.store.count())
	payload, err := json.Marshal(s.state.snapshot())
	if err != nil {
		return nil, err
	}

	return &pb.WidgetDataResponse{
		WidgetType: "chart",
		Data:       payload,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (s *moduleServer) ExecuteAction(ctx context.Context, req *pb.ActionRequest) (*pb.ActionResponse, error) {
	switch req.ActionId {
	case "connect":
		serverName := ""
		configID := ""
		if req.Params != nil {
			serverName = req.Params["server"]
			configID = req.Params["config_id"]
		}
		if configID == "" {
			return &pb.ActionResponse{
				Success: false,
				Message: "config_id is required",
			}, nil
		}
		config, ok := s.store.getByID(configID)
		if !ok {
			return &pb.ActionResponse{
				Success: false,
				Message: "config not found",
			}, nil
		}

		if serverName == "" {
			serverName = config.Name
		}
		selectedServer := serverName
		if selectedServer == "" {
			servers := extractServerNames(config.Content)
			if len(servers) > 0 {
				selectedServer = servers[0]
			}
		}

		if s.state.isActive(selectedServer, configID) {
			return &pb.ActionResponse{
				Success: true,
				Message: "already connected",
			}, nil
		}
		if s.state.isConnected() {
			s.engine.Stop()
			s.state.disconnect()
		}

		if err := ensureProxyPortAvailable(); err != nil {
			s.state.setEngineStatus(false, err.Error(), "")
			return &pb.ActionResponse{
				Success: false,
				Message: err.Error(),
			}, nil
		}

		outbound, err := buildOutboundFromConfig(config.Content, selectedServer)
		if err != nil {
			s.state.setEngineStatus(false, err.Error(), "")
			return &pb.ActionResponse{
				Success: false,
				Message: err.Error(),
			}, nil
		}

		configPath := filepath.Join(s.dataDir, "sing-box.json")
		if err := writeSingBoxConfig(configPath, outbound); err != nil {
			s.state.setEngineStatus(false, err.Error(), "")
			return &pb.ActionResponse{
				Success: false,
				Message: err.Error(),
			}, nil
		}

		binaryPath, err := resolveSingBoxPath(s.dataDir)
		if err != nil {
			s.state.setEngineStatus(false, err.Error(), "")
			return &pb.ActionResponse{
				Success: false,
				Message: err.Error(),
			}, nil
		}

		if err := s.engine.Start(binaryPath, configPath, s.logs); err != nil {
			s.state.setEngineStatus(false, err.Error(), "")
			return &pb.ActionResponse{
				Success: false,
				Message: err.Error(),
			}, nil
		}

		s.state.connect(selectedServer, configID)
		_ = setSystemProxy(true, fmt.Sprintf("%s:%d", getProxyListen(), getProxyPort()))
		if s.settings != nil {
			_ = s.settings.setDefaults(configID, selectedServer)
		}
		s.state.setEngineStatus(true, "", fmt.Sprintf("%s:%d", getProxyListen(), getProxyPort()))
		return &pb.ActionResponse{
			Success: true,
			Message: "connected",
		}, nil
	case "disconnect":
		s.engine.Stop()
		_ = setSystemProxy(false, "")
		s.state.disconnect()
		s.state.setEngineStatus(false, s.engine.LastError(), "")
		return &pb.ActionResponse{
			Success: true,
			Message: "disconnected",
		}, nil
	default:
		return &pb.ActionResponse{
			Success: false,
			Message: "unknown action",
		}, nil
	}
}

func (s *moduleServer) StreamUpdates(stream pb.ModuleService_StreamUpdatesServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}

		s.state.setConfigCount(s.store.count())
		payload, _ := json.Marshal(map[string]interface{}{
			"subscription": req.SubscriptionId,
			"status":       s.state.snapshot(),
		})

		if sendErr := stream.Send(&pb.UpdateResponse{
			EventType: "vpn_status",
			Payload:   payload,
			Timestamp: time.Now().Unix(),
		}); sendErr != nil {
			return sendErr
		}
	}
}

func RunBackend(options BackendOptions) (*grpc.Server, error) {
	state := &vpnState{}
	if err := os.MkdirAll(options.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", err)
	}
	store := newConfigStore(filepath.Join(options.DataDir, "configs.json"))
	if err := store.load(); err != nil {
		return nil, fmt.Errorf("failed to load configs: %w", err)
	}
	subscriptions := newSubscriptionStore(filepath.Join(options.DataDir, "subscriptions.json"))
	if err := subscriptions.load(); err != nil {
		return nil, fmt.Errorf("failed to load subscriptions: %w", err)
	}
	settings := newSettingsStore(filepath.Join(options.DataDir, "settings.json"))
	if err := settings.load(); err != nil {
		return nil, fmt.Errorf("failed to load settings: %w", err)
	}
	state.setConfigCount(store.count())

	listener, err := net.Listen("tcp", options.GRPCAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	engine := &proxyEngine{}
	logs := newLogBuffer(300)
	grpcServer := grpc.NewServer()
	pb.RegisterModuleServiceServer(grpcServer, &moduleServer{
		state:    state,
		store:    store,
		subs:     subscriptions,
		settings: settings,
		engine:   engine,
		dataDir:  options.DataDir,
		logs:     logs,
	})

	go func() {
		log.Printf("nekkus-vpn gRPC listening on %s", listener.Addr().String())
		if serveErr := grpcServer.Serve(listener); serveErr != nil {
			log.Fatalf("grpc server error: %v", serveErr)
		}
	}()

	if options.StartHTTP {
		go startHTTPServer(options.HTTPAddr, state, store, subscriptions, settings, engine, options.DataDir, logs)
	}

	if options.Mode == "hub" && options.HubAddr != "" {
		registerWithHub(options.HubAddr, options.ModuleID, options.Version)
	}

	if shouldAutoConnect() {
		tryAutoConnect(state, store, settings, engine, logs, options.DataDir)
	}

	return grpcServer, nil
}

func registerWithHub(hubAddr, moduleID, version string) {
	conn, err := grpc.Dial(hubAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("hub dial failed: %v", err)
		return
	}
	defer conn.Close()

	client := pb.NewHubServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Register(ctx, &pb.RegisterRequest{
		ModuleId: moduleID,
		Version:  version,
		Pid:      int32(os.Getpid()),
	})
	if err != nil {
		log.Printf("hub register failed: %v", err)
	}
}

func waitForShutdown(grpcServer *grpc.Server) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	<-signalChan
	grpcServer.GracefulStop()
}

func startHTTPServer(addr string, state *vpnState, store *configStore, subs *subscriptionStore, settings *settingsStore, engine *proxyEngine, dataDir string, logs *logBuffer) {
	mux := http.NewServeMux()

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if applyCORS(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		state.setConfigCount(store.count())
		writeJSON(w, http.StatusOK, state.snapshot())
	})

	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		if applyCORS(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, store.list())
		case http.MethodPost:
			var input struct {
				Name      string `json:"name"`
				Content   string `json:"content"`
				SourceURL string `json:"source_url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
				return
			}
			if input.Name == "" || input.Content == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and content are required"})
				return
			}
			newConfig := vpnConfig{
				ID:        strconv.FormatInt(time.Now().UnixNano(), 10),
				Name:      input.Name,
				Content:   input.Content,
				SourceURL: input.SourceURL,
				CreatedAt: time.Now().Unix(),
				UpdatedAt: time.Now().Unix(),
			}
			config, err := store.add(newConfig)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save"})
				return
			}
			state.setConfigCount(store.count())
			writeJSON(w, http.StatusCreated, config)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})

	mux.HandleFunc("/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		if applyCORS(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, subs.list())
		case http.MethodPost:
			var input struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
				return
			}
			if input.Name == "" || input.URL == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and url are required"})
				return
			}

			content, fetchErr := fetchSubscription(input.URL)
			if fetchErr != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": fetchErr.Error()})
				return
			}

			config := vpnConfig{
				ID:        strconv.FormatInt(time.Now().UnixNano(), 10),
				Name:      input.Name,
				Content:   content,
				SourceURL: input.URL,
				CreatedAt: time.Now().Unix(),
				UpdatedAt: time.Now().Unix(),
			}
			config, err := store.add(config)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config"})
				return
			}

			item := subscription{
				ID:          strconv.FormatInt(time.Now().UnixNano(), 10),
				Name:        input.Name,
				URL:         input.URL,
				ConfigID:    config.ID,
				CreatedAt:   time.Now().Unix(),
				UpdatedAt:   time.Now().Unix(),
				LastSuccess: time.Now().Unix(),
			}
			item, err = subs.add(item)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save subscription"})
				return
			}

			state.setConfigCount(store.count())
			writeJSON(w, http.StatusCreated, item)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})
	mux.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		if applyCORS(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			if settings == nil {
				writeJSON(w, http.StatusOK, vpnSettings{})
				return
			}
			writeJSON(w, http.StatusOK, settings.snapshot())
		case http.MethodPost:
			var input struct {
				DefaultConfigID string `json:"default_config_id"`
				DefaultServer   string `json:"default_server"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
				return
			}
			if settings == nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "settings not available"})
				return
			}
			if err := settings.setDefaults(input.DefaultConfigID, input.DefaultServer); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save settings"})
				return
			}
			writeJSON(w, http.StatusOK, settings.snapshot())
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})

	mux.HandleFunc("/subscriptions/refresh", func(w http.ResponseWriter, r *http.Request) {
		if applyCORS(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		results := make([]map[string]string, 0)
		for _, item := range subs.list() {
			content, err := fetchSubscription(item.URL)
			if err != nil {
				_ = subs.updateStatus(item.ID, err.Error(), item.LastSuccess)
				results = append(results, map[string]string{"id": item.ID, "status": "error"})
				continue
			}

			_, ok, updateErr := store.updateContent(item.ConfigID, content)
			if updateErr != nil || !ok {
				_ = subs.updateStatus(item.ID, "failed to update config", item.LastSuccess)
				results = append(results, map[string]string{"id": item.ID, "status": "error"})
				continue
			}

			_ = subs.updateStatus(item.ID, "", time.Now().Unix())
			results = append(results, map[string]string{"id": item.ID, "status": "ok"})
		}

		state.setConfigCount(store.count())
		writeJSON(w, http.StatusOK, results)
	})

	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		if applyCORS(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var input struct {
			Server   string `json:"server"`
			ConfigID string `json:"config_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		if input.ConfigID != "" {
			if config, ok := store.getByID(input.ConfigID); ok && input.Server == "" {
				input.Server = config.Name
			}
		}
		if input.ConfigID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "config_id is required"})
			return
		}

		config, ok := store.getByID(input.ConfigID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "config not found"})
			return
		}

		selectedServer := input.Server
		if selectedServer == "" {
			servers := extractServerNames(config.Content)
			if len(servers) > 0 {
				selectedServer = servers[0]
			}
		}
		if state.isActive(selectedServer, input.ConfigID) {
			writeJSON(w, http.StatusOK, state.snapshot())
			return
		}
		if state.isConnected() {
			engine.Stop()
			state.disconnect()
		}

		if err := ensureProxyPortAvailable(); err != nil {
			state.setEngineStatus(false, err.Error(), "")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		outbound, err := buildOutboundFromConfig(config.Content, selectedServer)
		if err != nil {
			state.setEngineStatus(false, err.Error(), "")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		configPath := filepath.Join(dataDir, "sing-box.json")
		if err := writeSingBoxConfig(configPath, outbound); err != nil {
			state.setEngineStatus(false, err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		binaryPath, err := resolveSingBoxPath(dataDir)
		if err != nil {
			state.setEngineStatus(false, err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		if err := engine.Start(binaryPath, configPath, logs); err != nil {
			state.setEngineStatus(false, err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		state.connect(selectedServer, input.ConfigID)
		_ = setSystemProxy(true, fmt.Sprintf("%s:%d", getProxyListen(), getProxyPort()))
		if settings != nil {
			_ = settings.setDefaults(input.ConfigID, selectedServer)
		}
		state.setEngineStatus(true, "", fmt.Sprintf("%s:%d", getProxyListen(), getProxyPort()))
		writeJSON(w, http.StatusOK, state.snapshot())
	})

	mux.HandleFunc("/disconnect", func(w http.ResponseWriter, r *http.Request) {
		if applyCORS(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		engine.Stop()
		_ = setSystemProxy(false, "")
		state.disconnect()
		state.setEngineStatus(false, engine.LastError(), "")
		writeJSON(w, http.StatusOK, state.snapshot())
	})
	mux.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		if applyCORS(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if logs == nil {
			writeJSON(w, http.StatusOK, []string{})
			return
		}
		writeJSON(w, http.StatusOK, logs.Snapshot())
	})

	mux.HandleFunc("/servers", func(w http.ResponseWriter, r *http.Request) {
		if applyCORS(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		configID := r.URL.Query().Get("config_id")
		if configID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "config_id is required"})
			return
		}
		config, ok := store.getByID(configID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "config not found"})
			return
		}
		servers := extractServerNames(config.Content)
		writeJSON(w, http.StatusOK, servers)
	})

	log.Printf("nekkus-vpn HTTP listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("http server error: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func applyCORS(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}

	return false
}

func fetchSubscription(url string) (string, error) {
	client := &http.Client{Timeout: defaultHTTPTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("bad response: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	content := string(body)
	if content == "" {
		return "", fmt.Errorf("empty subscription content")
	}

	return content, nil
}

func buildOutboundFromConfig(content, selectedServer string) (map[string]interface{}, error) {
	proxies := parseProxyURIs(content)
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no supported proxies found in config")
	}

	selectedServer = normalizeName(selectedServer)
	if selectedServer == "" {
		selectedServer = normalizeName(extractNameFromURI(proxies[0]))
	}

	for _, proxy := range proxies {
		name := normalizeName(extractNameFromURI(proxy))
		if name != selectedServer {
			continue
		}
		if strings.HasPrefix(proxy, "vless://") {
			return buildVlessOutbound(proxy, name)
		}
		if strings.HasPrefix(proxy, "ss://") {
			return buildShadowsocksOutbound(proxy, name)
		}
	}

	if len(proxies) > 0 {
		name := normalizeName(extractNameFromURI(proxies[0]))
		if strings.HasPrefix(proxies[0], "vless://") {
			return buildVlessOutbound(proxies[0], name)
		}
		if strings.HasPrefix(proxies[0], "ss://") {
			return buildShadowsocksOutbound(proxies[0], name)
		}
	}

	return nil, fmt.Errorf("selected server not found")
}

func parseProxyURIs(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return []string{}
	}
	if strings.Contains(content, "proxies:") {
		return []string{}
	}

	lines := extractURIList(content)
	if len(lines) > 0 {
		return lines
	}

	decoded, err := tryDecodeBase64(content)
	if err != nil {
		return []string{}
	}

	return extractURIList(decoded)
}

func extractURIList(content string) []string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "vless://") || strings.HasPrefix(line, "ss://") {
			result = append(result, line)
		}
	}
	return result
}

func normalizeName(value string) string {
	return strings.TrimSpace(value)
}

func shouldAutoConnect() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("NEKKUS_AUTO_CONNECT")))
	if value == "" {
		return false
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func tryAutoConnect(state *vpnState, store *configStore, settings *settingsStore, engine *proxyEngine, logs *logBuffer, dataDir string) {
	if settings == nil {
		return
	}
	defaults := settings.snapshot()
	if defaults.DefaultConfigID == "" {
		return
	}

	config, ok := store.getByID(defaults.DefaultConfigID)
	if !ok {
		return
	}

	selectedServer := defaults.DefaultServer
	if selectedServer == "" {
		servers := extractServerNames(config.Content)
		if len(servers) > 0 {
			selectedServer = servers[0]
		}
	}

	outbound, err := buildOutboundFromConfig(config.Content, selectedServer)
	if err != nil {
		state.setEngineStatus(false, err.Error(), "")
		return
	}

	configPath := filepath.Join(dataDir, "sing-box.json")
	if err := writeSingBoxConfig(configPath, outbound); err != nil {
		state.setEngineStatus(false, err.Error(), "")
		return
	}

	binaryPath, err := resolveSingBoxPath(dataDir)
	if err != nil {
		state.setEngineStatus(false, err.Error(), "")
		return
	}

	if err := engine.Start(binaryPath, configPath, logs); err != nil {
		state.setEngineStatus(false, err.Error(), "")
		return
	}

	state.connect(selectedServer, defaults.DefaultConfigID)
	state.setEngineStatus(true, "", fmt.Sprintf("%s:%d", getProxyListen(), getProxyPort()))
	_ = setSystemProxy(true, fmt.Sprintf("%s:%d", getProxyListen(), getProxyPort()))
}

func setSystemProxy(enabled bool, address string) error {
	if runtime.GOOS != "windows" {
		return nil
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if enabled {
		if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
			return err
		}
		if err := key.SetStringValue("ProxyServer", address); err != nil {
			return err
		}
	} else {
		if err := key.SetDWordValue("ProxyEnable", 0); err != nil {
			return err
		}
	}

	wininet := syscall.NewLazyDLL("wininet.dll")
	internetSetOption := wininet.NewProc("InternetSetOptionW")
	internetOptionSettingsChanged := uintptr(39)
	internetOptionRefresh := uintptr(37)
	internetSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
	internetSetOption.Call(0, internetOptionRefresh, 0, 0)
	return nil
}

type logBuffer struct {
	mu   sync.RWMutex
	max  int
	data []string
}

func newLogBuffer(max int) *logBuffer {
	return &logBuffer{max: max, data: make([]string, 0, max)}
}

func (b *logBuffer) Add(line string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if len(b.data) >= b.max {
		b.data = b.data[1:]
	}
	b.data = append(b.data, line)
	b.mu.Unlock()
}

func (b *logBuffer) Snapshot() []string {
	if b == nil {
		return []string{}
	}
	b.mu.RLock()
	out := make([]string, len(b.data))
	copy(out, b.data)
	b.mu.RUnlock()
	return out
}

func attachSingBoxPipes(cmd *exec.Cmd) (io.ReadCloser, io.ReadCloser, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	return stdout, stderr, nil
}

func streamSingBoxLogs(reader io.Reader, buffer *logBuffer) {
	if reader == nil {
		return
	}
	mode := strings.TrimSpace(strings.ToLower(os.Getenv("NEKKUS_SINGBOX_LOG")))
	if mode == "" {
		mode = "memory"
	}
	if mode == "none" || mode == "off" || mode == "false" || mode == "0" {
		_, _ = io.Copy(io.Discard, reader)
		return
	}
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if mode == "stdout" {
			fmt.Println(line)
			continue
		}
		buffer.Add(line)
	}
}

func buildVlessOutbound(raw, tag string) (map[string]interface{}, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	server := parsed.Hostname()
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return nil, fmt.Errorf("invalid port")
	}
	uuid := parsed.User.Username()
	if uuid == "" {
		return nil, fmt.Errorf("missing uuid")
	}

	params := parsed.Query()
	security := params.Get("security")
	flow := params.Get("flow")
	transport := params.Get("type")
	sni := params.Get("sni")
	pbk := params.Get("pbk")
	sid := params.Get("sid")
	fp := params.Get("fp")
	path := params.Get("path")
	hostHeader := params.Get("host")

	outbound := map[string]interface{}{
		"type":        "vless",
		"tag":         tag,
		"server":      server,
		"server_port": port,
		"uuid":        uuid,
		"network":     "tcp",
	}
	if flow != "" {
		outbound["flow"] = flow
	}

	if transport == "ws" {
		ws := map[string]interface{}{"type": "ws"}
		if path != "" {
			ws["path"] = path
		}
		if hostHeader != "" {
			ws["headers"] = map[string]string{"Host": hostHeader}
		}
		outbound["transport"] = ws
	}

	if security != "" && security != "none" {
		tls := map[string]interface{}{
			"enabled":     true,
			"server_name": pickServerName(sni, server),
		}
		if security == "reality" {
			tls["reality"] = map[string]interface{}{
				"enabled":    true,
				"public_key": pbk,
				"short_id":   sid,
			}
		}
		if fp != "" {
			tls["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": fp,
			}
		}
		outbound["tls"] = tls
	}

	return outbound, nil
}

func buildShadowsocksOutbound(raw, tag string) (map[string]interface{}, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	server := parsed.Hostname()
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return nil, fmt.Errorf("invalid port")
	}

	method := ""
	password := ""

	if parsed.User != nil {
		user := parsed.User.Username()
		pass, hasPass := parsed.User.Password()
		if hasPass {
			method = user
			password = pass
		} else {
			decoded, err := tryDecodeBase64(user)
			if err == nil {
				parts := strings.SplitN(decoded, ":", 2)
				if len(parts) == 2 {
					method = parts[0]
					password = parts[1]
				}
			}
		}
	}

	if method == "" || password == "" {
		return nil, fmt.Errorf("invalid shadowsocks credentials")
	}

	return map[string]interface{}{
		"type":        "shadowsocks",
		"tag":         tag,
		"server":      server,
		"server_port": port,
		"method":      method,
		"password":    password,
	}, nil
}

func pickServerName(sni, fallback string) string {
	if sni != "" {
		return sni
	}
	return fallback
}

func writeSingBoxConfig(configPath string, outbound map[string]interface{}) error {
	config := map[string]interface{}{
		"log": map[string]interface{}{
			"level": "info",
		},
		"inbounds": []map[string]interface{}{
			{
				"type":                       "mixed",
				"tag":                        "mixed-in",
				"listen":                     getProxyListen(),
				"listen_port":                getProxyPort(),
				"sniff":                      true,
				"sniff_override_destination": true,
				"set_system_proxy":           shouldSetSystemProxy(),
			},
		},
		"outbounds": []map[string]interface{}{
			outbound,
			{
				"type": "direct",
				"tag":  "direct",
			},
		},
		"route": map[string]interface{}{
			"final": outbound["tag"],
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0o600)
}

func ensureProxyPortAvailable() error {
	address := fmt.Sprintf("%s:%d", getProxyListen(), getProxyPort())
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("proxy port is busy: %s", address)
	}
	_ = listener.Close()
	return nil
}

func shouldSetSystemProxy() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("NEKKUS_SET_SYSTEM_PROXY")))
	if value == "" {
		return true
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func getProxyPort() int {
	value := strings.TrimSpace(os.Getenv("NEKKUS_PROXY_PORT"))
	if value == "" {
		return defaultProxyPort
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return defaultProxyPort
	}
	return port
}

func getProxyListen() string {
	value := strings.TrimSpace(os.Getenv("NEKKUS_PROXY_LISTEN"))
	if value == "" {
		return defaultProxyListen
	}
	return value
}

func resolveSingBoxPath(dataDir string) (string, error) {
	if envPath := os.Getenv("NEKKUS_SINGBOX_PATH"); envPath != "" {
		if fileExists(envPath) {
			return envPath, nil
		}
		return "", fmt.Errorf("sing-box not found at NEKKUS_SINGBOX_PATH")
	}

	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		candidate := filepath.Join(dir, "sing-box.exe")
		if fileExists(candidate) {
			return candidate, nil
		}
	}

	candidate := filepath.Join(dataDir, "sing-box.exe")
	if fileExists(candidate) {
		return candidate, nil
	}

	return "", fmt.Errorf("sing-box binary not found")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
func extractServerNames(content string) []string {
	servers := extractServersFromYAML(content)
	if len(servers) > 0 {
		return servers
	}

	servers = extractServersFromURIList(content)
	if len(servers) > 0 {
		return servers
	}

	decoded, err := tryDecodeBase64(content)
	if err != nil {
		return []string{}
	}

	servers = extractServersFromYAML(decoded)
	if len(servers) > 0 {
		return servers
	}

	return extractServersFromURIList(decoded)
}

func extractServersFromYAML(content string) []string {
	var root map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return []string{}
	}

	rawProxies, ok := root["proxies"].([]interface{})
	if !ok {
		return []string{}
	}

	seen := make(map[string]bool)
	servers := make([]string, 0, len(rawProxies))
	for _, proxy := range rawProxies {
		proxyMap, ok := proxy.(map[string]interface{})
		if !ok {
			continue
		}
		name, ok := proxyMap["name"].(string)
		if !ok || name == "" {
			continue
		}
		if !seen[name] {
			seen[name] = true
			servers = append(servers, name)
		}
	}

	return servers
}

func extractServersFromURIList(content string) []string {
	lines := strings.Split(content, "\n")
	seen := make(map[string]bool)
	servers := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.Contains(line, "://") {
			continue
		}

		name := normalizeName(extractNameFromURI(line))
		if name == "" {
			continue
		}
		if !seen[name] {
			seen[name] = true
			servers = append(servers, name)
		}
	}

	return servers
}

func extractNameFromURI(raw string) string {
	parts := strings.SplitN(raw, "#", 2)
	if len(parts) == 2 && parts[1] != "" {
		if decoded, err := url.QueryUnescape(parts[1]); err == nil {
			return decoded
		}
		return parts[1]
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Host != "" {
		return parsed.Host
	}
	return ""
}

func tryDecodeBase64(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", fmt.Errorf("empty content")
	}

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err == nil {
		return string(decoded), nil
	}

	decoded, err = base64.RawStdEncoding.DecodeString(trimmed)
	if err == nil {
		return string(decoded), nil
	}

	return "", err
}

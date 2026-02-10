package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	pb "github.com/GalitskyKK/nekkus-core/internal/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultModuleID   = "com.nekkus.vpn"
	defaultVersion    = "0.1.0"
	defaultListenAddr = "127.0.0.1:50061"
	defaultHTTPAddr   = "127.0.0.1:8081"
	defaultDataDir    = "data"
)

type vpnConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	SourceURL string `json:"source_url,omitempty"`
	CreatedAt int64  `json:"created_at"`
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

type vpnState struct {
	mu             sync.RWMutex
	connected      bool
	serverName     string
	activeConfigID string
	configCount    int
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

type moduleServer struct {
	pb.UnimplementedModuleServiceServer
	state *vpnState
	store *configStore
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
		if configID != "" {
			if config, ok := s.store.getByID(configID); ok && serverName == "" {
				serverName = config.Name
			}
		}
		s.state.connect(serverName, configID)
		return &pb.ActionResponse{
			Success: true,
			Message: "connected",
		}, nil
	case "disconnect":
		s.state.disconnect()
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

func main() {
	mode := flag.String("mode", "standalone", "Run mode: standalone or hub")
	listenAddr := flag.String("addr", defaultListenAddr, "gRPC listen address")
	httpAddr := flag.String("http-addr", defaultHTTPAddr, "Standalone HTTP address")
	dataDir := flag.String("data-dir", defaultDataDir, "Data directory")
	hubAddr := flag.String("hub-addr", os.Getenv("NEKKUS_HUB_ADDR"), "Hub gRPC address")
	moduleID := flag.String("module-id", defaultModuleID, "Module identifier")
	version := flag.String("version", defaultVersion, "Module version")
	flag.Parse()

	state := &vpnState{}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}
	store := newConfigStore(filepath.Join(*dataDir, "configs.json"))
	if err := store.load(); err != nil {
		log.Fatalf("failed to load configs: %v", err)
	}
	state.setConfigCount(store.count())

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterModuleServiceServer(grpcServer, &moduleServer{state: state, store: store})

	go func() {
		log.Printf("nekkus-vpn gRPC listening on %s", listener.Addr().String())
		if serveErr := grpcServer.Serve(listener); serveErr != nil {
			log.Fatalf("grpc server error: %v", serveErr)
		}
	}()

	if *mode == "standalone" {
		go startHTTPServer(*httpAddr, state, store)
	}

	if *mode == "hub" && *hubAddr != "" {
		registerWithHub(*hubAddr, *moduleID, *version)
	}

	waitForShutdown(grpcServer)
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

func startHTTPServer(addr string, state *vpnState, store *configStore) {
	mux := http.NewServeMux()

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		state.setConfigCount(store.count())
		writeJSON(w, http.StatusOK, state.snapshot())
	})

	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
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
		state.connect(input.Server, input.ConfigID)
		writeJSON(w, http.StatusOK, state.snapshot())
	})

	mux.HandleFunc("/disconnect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		state.disconnect()
		writeJSON(w, http.StatusOK, state.snapshot())
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

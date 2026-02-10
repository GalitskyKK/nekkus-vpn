package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	pb "github.com/GalitskyKK/nekkus-core/internal/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultModuleID  = "com.nekkus.vpn"
	defaultVersion   = "0.1.0"
	defaultListenAddr = "127.0.0.1:0"
)

type vpnState struct {
	mu            sync.RWMutex
	connected     bool
	serverName    string
	downloadSpeed int64
	uploadSpeed   int64
	totalDownload int64
	totalUpload   int64
}

func (s *vpnState) snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"connected":      s.connected,
		"server":         s.serverName,
		"downloadSpeed":  s.downloadSpeed,
		"uploadSpeed":    s.uploadSpeed,
		"totalDownload":  s.totalDownload,
		"totalUpload":    s.totalUpload,
		"lastUpdateUnix": time.Now().Unix(),
	}
}

func (s *vpnState) connect(serverName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if serverName == "" {
		serverName = "auto"
	}

	s.connected = true
	s.serverName = serverName
	s.downloadSpeed = 12_300_000
	s.uploadSpeed = 1_200_000
}

func (s *vpnState) disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = false
	s.serverName = ""
	s.downloadSpeed = 0
	s.uploadSpeed = 0
}

type moduleServer struct {
	pb.UnimplementedModuleServiceServer
	state *vpnState
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
		if req.Params != nil {
			serverName = req.Params["server"]
		}
		s.state.connect(serverName)
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
	hubAddr := flag.String("hub-addr", os.Getenv("NEKKUS_HUB_ADDR"), "Hub gRPC address")
	moduleID := flag.String("module-id", defaultModuleID, "Module identifier")
	version := flag.String("version", defaultVersion, "Module version")
	flag.Parse()

	state := &vpnState{}

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterModuleServiceServer(grpcServer, &moduleServer{state: state})

	go func() {
		log.Printf("nekkus-vpn gRPC listening on %s", listener.Addr().String())
		if serveErr := grpcServer.Serve(listener); serveErr != nil {
			log.Fatalf("grpc server error: %v", serveErr)
		}
	}()

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

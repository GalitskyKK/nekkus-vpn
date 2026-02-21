package module

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pb "github.com/GalitskyKK/nekkus-core/pkg/protocol"
	"github.com/GalitskyKK/nekkus-net/internal/vpn"
	"google.golang.org/grpc"
)

type NetModule struct {
	pb.UnimplementedNekkusModuleServer
	engine   *vpn.Engine
	httpPort int
}

func New(engine *vpn.Engine, httpPort int) *NetModule {
	if httpPort <= 0 {
		httpPort = 9001
	}
	return &NetModule{engine: engine, httpPort: httpPort}
}

func (m *NetModule) GetInfo(ctx context.Context, _ *pb.Empty) (*pb.ModuleInfo, error) {
	return &pb.ModuleInfo{
		Id:           "net",
		Name:         "Nekkus Net",
		Version:      "1.0.0",
		Description:  "VPN + Mesh networking",
		Color:        "#3B82F6",
		HttpPort:     int32(m.httpPort),
		GrpcPort:     19001,
		UiUrl:        fmt.Sprintf("http://127.0.0.1:%d", m.httpPort),
		Capabilities: []string{"vpn.connect", "vpn.disconnect", "vpn.status", "vpn.servers"},
		Provides:     []string{"vpn.status", "vpn.traffic", "vpn.servers"},
		Status:       pb.ModuleStatus_MODULE_RUNNING,
	}, nil
}

func (m *NetModule) Health(ctx context.Context, _ *pb.Empty) (*pb.HealthStatus, error) {
	return &pb.HealthStatus{
		Healthy: true,
		Message: string(m.engine.GetStatus()),
		Details: map[string]string{
			"vpn_status": string(m.engine.GetStatus()),
		},
	}, nil
}

func (m *NetModule) GetWidgets(ctx context.Context, _ *pb.Empty) (*pb.WidgetList, error) {
	return &pb.WidgetList{
		Widgets: []*pb.Widget{
			{
				Id:                "net.status",
				Title:             "VPN Status",
				Size:              pb.WidgetSize_WIDGET_SMALL,
				DataEndpoint:      "/api/status",
				RefreshIntervalMs: 2000,
			},
			{
				Id:                "net.traffic",
				Title:             "Traffic",
				Size:              pb.WidgetSize_WIDGET_MEDIUM,
				DataEndpoint:      "/api/traffic",
				RefreshIntervalMs: 1000,
			},
		},
	}, nil
}

func (m *NetModule) GetActions(ctx context.Context, _ *pb.Empty) (*pb.ActionList, error) {
	return &pb.ActionList{
		Actions: []*pb.Action{
			{
				Id:          "net.connect",
				Label:       "Connect VPN",
				Description: "Connect to VPN server",
				Icon:        "🔌",
				ModuleId:    "net",
				Tags:        []string{"vpn", "connect", "network"},
				Params: []*pb.ActionParam{
					{Name: "server_id", Type: "string", Label: "Server"},
				},
			},
			{
				Id:          "net.disconnect",
				Label:       "Disconnect VPN",
				Description: "Disconnect from VPN",
				Icon:        "🔌",
				ModuleId:    "net",
				Tags:        []string{"vpn", "disconnect"},
			},
			{
				Id:          "net.quick_connect",
				Label:       "Quick Connect",
				Description: "Connect to the best available server",
				Icon:        "⚡",
				ModuleId:    "net",
				Tags:        []string{"vpn", "quick", "connect"},
			},
		},
	}, nil
}

func (m *NetModule) StreamData(req *pb.StreamRequest, _ grpc.ServerStreamingServer[pb.DataEvent]) error {
	return nil
}

func (m *NetModule) Execute(ctx context.Context, req *pb.ExecuteRequest) (*pb.ExecuteResponse, error) {
	switch req.ActionId {
	case "disconnect":
		// Hub при остановке модуля шлёт action "disconnect"
		if err := m.engine.Disconnect(); err != nil {
			return &pb.ExecuteResponse{Success: false, Error: err.Error()}, nil
		}
		return &pb.ExecuteResponse{Success: true, Message: "Disconnected"}, nil
	case "net.connect":
		serverID := req.Params["server_id"]
		if err := m.engine.Connect(serverID); err != nil {
			return &pb.ExecuteResponse{Success: false, Error: err.Error()}, nil
		}
		return &pb.ExecuteResponse{Success: true, Message: "Connected"}, nil
	case "net.disconnect":
		if err := m.engine.Disconnect(); err != nil {
			return &pb.ExecuteResponse{Success: false, Error: err.Error()}, nil
		}
		return &pb.ExecuteResponse{Success: true, Message: "Disconnected"}, nil
	case "net.quick_connect":
		if err := m.engine.QuickConnect(); err != nil {
			return &pb.ExecuteResponse{Success: false, Error: err.Error()}, nil
		}
		return &pb.ExecuteResponse{Success: true, Message: "Quick connected"}, nil
	}
	return &pb.ExecuteResponse{Success: false, Error: "unknown action"}, nil
}

func (m *NetModule) Query(ctx context.Context, req *pb.QueryRequest) (*pb.QueryResponse, error) {
	switch req.QueryType {
	case "servers":
		servers, err := m.engine.GetServers()
		if err != nil {
			return &pb.QueryResponse{Success: false, Error: err.Error()}, nil
		}
		data, _ := json.Marshal(servers)
		return &pb.QueryResponse{Success: true, Data: data}, nil
	case "status":
		data, _ := json.Marshal(map[string]interface{}{
			"status": m.engine.GetStatus(),
			"server": m.engine.GetCurrentServer(),
		})
		return &pb.QueryResponse{Success: true, Data: data}, nil
	}
	return &pb.QueryResponse{Success: false, Error: "unknown query"}, nil
}

func (m *NetModule) GetSnapshot(ctx context.Context, _ *pb.Empty) (*pb.StateSnapshot, error) {
	state := map[string]interface{}{
		"status":       m.engine.GetStatus(),
		"current_node": m.engine.GetCurrentServer(),
	}
	data, _ := json.Marshal(state)
	return &pb.StateSnapshot{
		ModuleId:  "net",
		Timestamp: time.Now().Unix(),
		State:     data,
	}, nil
}

func (m *NetModule) RestoreSnapshot(ctx context.Context, snap *pb.StateSnapshot) (*pb.RestoreResult, error) {
	return &pb.RestoreResult{Success: true, Message: "Restored"}, nil
}

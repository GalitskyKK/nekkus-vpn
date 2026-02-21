package server

import (
	"encoding/json"
	"net/http"
	"time"

	coreserver "github.com/GalitskyKK/nekkus-core/pkg/server"
	"github.com/GalitskyKK/nekkus-net/internal/store"
	"github.com/GalitskyKK/nekkus-net/internal/vpn"
)

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func RegisterRoutes(srv *coreserver.Server, engine *vpn.Engine) {
	srv.Mux.HandleFunc("GET /api/deps/singbox", func(w http.ResponseWriter, _ *http.Request) {
		setCORS(w)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(engine.GetSingBoxStatus())
	})

	srv.Mux.HandleFunc("POST /api/deps/singbox/install", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		status, err := engine.InstallSingBox(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	srv.Mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		w.Header().Set("Content-Type", "application/json")
		server := engine.GetCurrentServer()
		serverName := ""
		if server != nil {
			serverName = server.Name
		}
		connected := engine.GetStatus() == vpn.Connected
		downloadSpeed, uploadSpeed := int64(0), int64(0)
		totalDownload, totalUpload := int64(0), int64(0)
		if stats, err := engine.GetTrafficStats(); err == nil {
			downloadSpeed = stats.DownloadSpeed
			uploadSpeed = stats.UploadSpeed
			totalDownload = stats.Download
			totalUpload = stats.Upload
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected":       connected,
			"server":          serverName,
			"activeConfigId":  "",
			"configCount":     0,
			"downloadSpeed":   downloadSpeed,
			"uploadSpeed":     uploadSpeed,
			"totalDownload":   totalDownload,
			"totalUpload":     totalUpload,
			"lastUpdateUnix":  time.Now().Unix(),
		})
	})

	srv.Mux.HandleFunc("GET /api/servers", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		configID := r.URL.Query().Get("config_id")
		servers, err := engine.GetServersByConfigID(configID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if servers == nil {
			servers = []store.ServerNode{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(servers)
	})

	srv.Mux.HandleFunc("POST /api/connect", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		var req struct {
			ServerID string `json:"server_id"`
			Server   string `json:"server"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		serverID := req.ServerID
		if serverID == "" {
			serverID = req.Server
		}
		if serverID == "" {
			http.Error(w, "server_id or server required", 400)
			return
		}

		if err := engine.Connect(serverID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		srv.Broadcast(map[string]interface{}{
			"type":   "status_changed",
			"status": engine.GetStatus(),
		})

		w.Header().Set("Content-Type", "application/json")
		server := engine.GetCurrentServer()
		serverName := ""
		if server != nil {
			serverName = server.Name
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": true, "server": serverName,
			"activeConfigId": "", "configCount": 0,
			"downloadSpeed": 0, "uploadSpeed": 0,
			"totalDownload": 0, "totalUpload": 0,
			"lastUpdateUnix": 0,
		})
	})

	srv.Mux.HandleFunc("POST /api/disconnect", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if err := engine.Disconnect(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		srv.Broadcast(map[string]interface{}{
			"type":   "status_changed",
			"status": engine.GetStatus(),
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false, "server": "",
			"activeConfigId": "", "configCount": 0,
			"downloadSpeed": 0, "uploadSpeed": 0,
			"totalDownload": 0, "totalUpload": 0,
			"lastUpdateUnix": 0,
		})
	})

	srv.Mux.HandleFunc("POST /api/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		var req struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		url := req.URL
		if url == "" {
			http.Error(w, "url required", 400)
			return
		}
		name := req.Name
		if name == "" {
			name = url
		}

		sub, err := engine.AddSubscription(name, url)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         sub.ID,
			"name":       sub.Name,
			"url":        sub.URL,
			"config_id":  "",
			"created_at": sub.UpdatedAt,
			"updated_at": sub.UpdatedAt,
		})
	})

	srv.Mux.HandleFunc("GET /api/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		subs, err := engine.GetSubscriptions()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(subs)
	})

	srv.Mux.HandleFunc("GET /api/traffic", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		stats, err := engine.GetTrafficStats()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	srv.Mux.HandleFunc("GET /api/configs", func(w http.ResponseWriter, _ *http.Request) {
		setCORS(w)
		subs, err := engine.GetSubscriptions()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		configs := make([]map[string]interface{}, 0, len(subs))
		for _, sub := range subs {
			configs = append(configs, map[string]interface{}{
				"id":               sub.ID,
				"name":             sub.Name,
				"content":          "",
				"source_url":       sub.URL,
				"subscription_id":  sub.ID,
				"created_at":       sub.UpdatedAt,
				"updated_at":       sub.UpdatedAt,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(configs)
	})

	srv.Mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, _ *http.Request) {
		setCORS(w)
		settings, err := engine.GetSettings()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(settings)
	})

	srv.Mux.HandleFunc("POST /api/settings", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		var patch store.Settings
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		settings, err := engine.UpdateSettings(patch)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(settings)
	})

	srv.Mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, _ *http.Request) {
		setCORS(w)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]string{})
	})

	srv.Mux.HandleFunc("POST /api/subscriptions/refresh", func(w http.ResponseWriter, _ *http.Request) {
		setCORS(w)
		results := engine.RefreshAllSubscriptions()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	})
}

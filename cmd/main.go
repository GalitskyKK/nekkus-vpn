package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/GalitskyKK/nekkus-core/pkg/config"
	"github.com/GalitskyKK/nekkus-core/pkg/desktop"
	"github.com/GalitskyKK/nekkus-core/pkg/discovery"
	coreserver "github.com/GalitskyKK/nekkus-core/pkg/server"
	pb "github.com/GalitskyKK/nekkus-core/pkg/protocol"

	"github.com/GalitskyKK/nekkus-net/assets"
	"github.com/GalitskyKK/nekkus-net/internal/module"
	"github.com/GalitskyKK/nekkus-net/internal/server"
	"github.com/GalitskyKK/nekkus-net/internal/store"
	"github.com/GalitskyKK/nekkus-net/internal/vpn"
	"github.com/GalitskyKK/nekkus-net/ui"

	"google.golang.org/grpc"
)

var (
	httpPort  = flag.Int("port", 9001, "HTTP port")
	grpcPort  = flag.Int("grpc-port", 19001, "gRPC port")
	headless  = flag.Bool("headless", false, "Run without GUI")
	trayOnly  = flag.Bool("tray-only", false, "Start minimized to tray")
	mode      = flag.String("mode", "standalone", "Run mode: standalone or hub (Hub passes this)")
	hubAddr   = flag.String("hub-addr", "", "Hub gRPC address when started by Hub")
	addr      = flag.String("addr", "", "gRPC listen address (e.g. 127.0.0.1:19001); overrides -grpc-port")
	dataDirF  = flag.String("data-dir", "", "Data directory (overrides default)")
)

func waitForServer(host string, port int, timeout time.Duration) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Порт gRPC: если Hub передал --addr=127.0.0.1:19001, берём порт оттуда
	grpcPortVal := *grpcPort
	if *addr != "" {
		if _, portStr, err := net.SplitHostPort(*addr); err == nil {
			if p, err := strconv.Atoi(portStr); err == nil {
				grpcPortVal = p
			}
		}
	}

	dataDir := *dataDirF
	if dataDir == "" {
		dataDir = config.GetDataDir("net")
	}
	db, err := store.New(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	engine := vpn.NewEngine(db)

	uiFS, _ := fs.Sub(ui.Assets, "frontend/dist")

	srv := coreserver.New(*httpPort, grpcPortVal, uiFS)

	server.RegisterRoutes(srv, engine)

	go func() {
		if err := srv.Start(ctx); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	mod := module.New(engine)
	go func() {
		if err := srv.StartGRPC(func(s *grpc.Server) {
			pb.RegisterNekkusModuleServer(s, mod)
		}); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	disc, err := discovery.Announce(discovery.ModuleAnnouncement{
		ID:       "net",
		Name:     "Nekkus Net",
		HTTPPort: *httpPort,
		GRPCPort: grpcPortVal,
	})
	if err != nil {
		log.Printf("Discovery error: %v", err)
	} else {
		defer disc.Shutdown()
	}

	log.Printf("Nekkus Net → http://localhost:%d", *httpPort)

	_ = hubAddr // передаётся Hub'ом; при необходимости — регистрация на Hub по gRPC

	// Режим из Hub: NEKKUS_SHOW_UI=1 → открыть окно (Open UI), иначе фоновый (Start)
	showUIFromHub := os.Getenv("NEKKUS_SHOW_UI") == "1"
	autoConnectFromHub := os.Getenv("NEKKUS_AUTO_CONNECT") == "1"

	runHeadless := *headless || (*mode == "hub" && !showUIFromHub)
	if runHeadless {
		// Запуск из Hub по кнопке «Запустить»: авто-подключение к последнему/первому серверу и прокси
		if *mode == "hub" && autoConnectFromHub {
			go func() {
				time.Sleep(1 * time.Second) // дать подняться HTTP/gRPC
				if err := engine.QuickConnect(); err != nil {
					log.Printf("auto-connect: %v", err)
				}
			}()
		}
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		engine.Disconnect()
		cancel()
	} else {
		waitForServer("127.0.0.1", *httpPort, 5*time.Second)
		desktop.Launch(desktop.AppConfig{
			ModuleID:   "net",
			ModuleName: "Nekkus Net",
			HTTPPort:   *httpPort,
			IconBytes:  assets.TrayIcon,
			Headless:   false,
			TrayOnly:   *trayOnly,
			TrayMenuItems: []desktop.TrayMenuItem{
				{Label: "Quick Connect", OnClick: func() { engine.QuickConnect() }},
				{Label: "Disconnect", OnClick: func() { engine.Disconnect() }},
			},
			OnQuit: func() {
				engine.Disconnect()
				cancel()
			},
		})
	}
}

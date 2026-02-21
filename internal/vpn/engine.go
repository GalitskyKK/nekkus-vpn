package vpn

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GalitskyKK/nekkus-net/internal/deps/singbox"
	"github.com/GalitskyKK/nekkus-net/internal/store"
	"github.com/GalitskyKK/nekkus-net/internal/subscription"
)

type Status string

const (
	Disconnected Status = "disconnected"
	Connecting   Status = "connecting"
	Connected    Status = "connected"
	Error        Status = "error"
)

type TrafficStats struct {
	Upload        int64 `json:"upload"`
	Download      int64 `json:"download"`
	DownloadSpeed int64 `json:"download_speed"`
	UploadSpeed   int64 `json:"upload_speed"`
	StartedAt     int64 `json:"started_at"`
}

type Engine struct {
	store       *store.Store
	status      Status
	statusMu    sync.RWMutex
	currentNode *store.ServerNode
	process     *exec.Cmd
	lastConfigPath string
}

func NewEngine(st *store.Store) *Engine {
	return &Engine{
		store:  st,
		status: Disconnected,
	}
}

func (e *Engine) GetStatus() Status {
	e.statusMu.RLock()
	defer e.statusMu.RUnlock()
	return e.status
}

func (e *Engine) setStatus(s Status) {
	e.statusMu.Lock()
	e.status = s
	e.statusMu.Unlock()
}

func (e *Engine) GetCurrentServer() *store.ServerNode {
	return e.currentNode
}

func (e *Engine) GetSettings() (store.Settings, error) {
	return e.store.GetSettings()
}

func (e *Engine) UpdateSettings(patch store.Settings) (store.Settings, error) {
	return e.store.UpdateSettings(patch)
}

func (e *Engine) GetSingBoxStatus() singbox.Status {
	// Order: env override -> settings -> bundled (рядом с exe) -> PATH
	if envPath := os.Getenv("NEKKUS_SINGBOX_PATH"); envPath != "" {
		if _, err := exec.LookPath(envPath); err == nil {
			return singbox.Status{Installed: true, Path: envPath, Source: "env"}
		}
	}

	if settings, err := e.store.GetSettings(); err == nil && settings.SingBoxPath != "" {
		if _, err := exec.LookPath(settings.SingBoxPath); err == nil {
			return singbox.Status{Installed: true, Path: settings.SingBoxPath, Source: "settings"}
		}
	}

	if p, ok := getBundledSingBoxPath(); ok {
		return singbox.Status{Installed: true, Path: p, Source: "bundled"}
	}

	if p, err := exec.LookPath("sing-box"); err == nil {
		return singbox.Status{Installed: true, Path: p, Source: "path"}
	}
	return singbox.Status{Installed: false}
}

// getBundledSingBoxPath возвращает путь к sing-box рядом с исполняемым файлом (папка sing-box/).
// Так можно положить sing-box в архив рядом с nekkus-net.exe — «скачал и включил».
func getBundledSingBoxPath() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	dir := filepath.Dir(exe)
	bin := "sing-box"
	if runtime.GOOS == "windows" {
		bin = "sing-box.exe"
	}
	p := filepath.Join(dir, "sing-box", bin)
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return "", false
	}
	return p, true
}

func (e *Engine) InstallSingBox(ctx context.Context) (singbox.Status, error) {
	status, err := singbox.InstallLatest(ctx, e.store.DataDir())
	if err != nil {
		return singbox.Status{}, err
	}
	if status.Path != "" {
		_, _ = e.store.UpdateSettings(store.Settings{SingBoxPath: status.Path})
	}
	return status, nil
}

func (e *Engine) AddSubscription(name, url string) (*store.Subscription, error) {
	// Пока только сохранение в store; загрузка серверов по URL — отдельно (refresh).
	return e.store.AddSubscription(name, url)
}

func (e *Engine) GetSubscriptions() ([]store.Subscription, error) {
	return e.store.GetSubscriptions()
}

func (e *Engine) GetServers() ([]store.ServerNode, error) {
	return e.store.GetServers()
}

// GetServersByConfigID возвращает серверы подписки с id=configID; если подписка не найдена — все серверы.
// Всегда возвращает не-nil слайс (пустой при отсутствии серверов), чтобы API отдавал JSON [] а не null.
func (e *Engine) GetServersByConfigID(configID string) ([]store.ServerNode, error) {
	if configID != "" {
		if sub, err := e.store.GetSubscription(configID); err == nil {
			if sub.Servers != nil {
				return sub.Servers, nil
			}
			return []store.ServerNode{}, nil
		}
	}
	return e.store.GetServers()
}

// RefreshResult — результат обновления одной подписки.
type RefreshResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// RefreshSubscription загружает по URL подписки и обновляет список серверов.
func (e *Engine) RefreshSubscription(subID string) error {
	sub, err := e.store.GetSubscription(subID)
	if err != nil {
		return err
	}
	body, err := subscription.Fetch(sub.URL)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	servers, err := subscription.ParseContent(body)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	return e.store.UpdateSubscriptionServers(subID, servers)
}

// RefreshAllSubscriptions обновляет серверы для всех подписок.
func (e *Engine) RefreshAllSubscriptions() []RefreshResult {
	subs, err := e.store.GetSubscriptions()
	if err != nil {
		return []RefreshResult{{ID: "", Status: err.Error()}}
	}
	results := make([]RefreshResult, 0, len(subs))
	for _, sub := range subs {
		err := e.RefreshSubscription(sub.ID)
		if err != nil {
			log.Printf("refresh subscription %s: %v", sub.ID, err)
			results = append(results, RefreshResult{ID: sub.ID, Status: err.Error()})
		} else {
			results = append(results, RefreshResult{ID: sub.ID, Status: "ok"})
		}
	}
	return results
}

func (e *Engine) Connect(serverID string) error {
	e.setStatus(Connecting)

	server, err := e.store.GetServer(serverID)
	if err != nil {
		e.setStatus(Error)
		return err
	}
	if server == nil {
		e.setStatus(Error)
		return fmt.Errorf("server not found: %s", serverID)
	}
	if server.URI == "" {
		e.setStatus(Error)
		return fmt.Errorf("server has no uri (refresh subscription to fetch full links)")
	}

	cfg, err := e.generateSingboxConfig(server)
	if err != nil {
		e.setStatus(Error)
		return err
	}
	cfgPath, err := e.writeTempConfig(cfg)
	if err != nil {
		e.setStatus(Error)
		return err
	}

	status := e.GetSingBoxStatus()
	if !status.Installed || status.Path == "" {
		e.setStatus(Error)
		return fmt.Errorf("sing-box not found: install via UI or set NEKKUS_SINGBOX_PATH / settings.sing_box_path")
	}

	e.process = exec.Command(status.Path, "run", "-c", cfgPath)
	setProcessNoWindow(e.process)
	var stderrBuf bytes.Buffer
	e.process.Stderr = &stderrBuf
	if err := e.process.Start(); err != nil {
		e.setStatus(Error)
		return fmt.Errorf("sing-box start error: %w", err)
	}

	// Ждём, пока sing-box поднимет mixed inbound на 127.0.0.1:7890 — только потом включаем системный прокси.
	const proxyPort = 7890
	if err := e.waitForProxyPort("127.0.0.1", proxyPort, 15*time.Second, &stderrBuf); err != nil {
		_ = e.process.Process.Kill()
		_ = e.process.Wait()
		e.process = nil
		e.setStatus(Error)
		return err
	}

	setSystemProxy("127.0.0.1", proxyPort)

	e.currentNode = server
	e.setStatus(Connected)
	log.Printf("Connected to %s", server.Name)
	return nil
}

// waitForProxyPort ждёт, пока на host:port появится слушатель (sing-box mixed inbound).
func (e *Engine) waitForProxyPort(host string, port int, timeout time.Duration, stderr *bytes.Buffer) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	deadline := time.Now().Add(timeout)
	exitCh := make(chan error, 1)
	go func() { exitCh <- e.process.Wait() }()

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 400*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case waitErr := <-exitCh:
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				return fmt.Errorf("sing-box завершился до запуска прокси: %w\nвывод sing-box: %s", waitErr, msg)
			}
			return fmt.Errorf("sing-box завершился до запуска прокси: %w", waitErr)
		default:
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("прокси %s не поднялся за %v (проверь конфиг или логи sing-box)", addr, timeout)
}

func (e *Engine) Disconnect() error {
	// Сразу снимаем системный прокси, чтобы при убийстве процесса из Hub прокси не оставался включённым.
	clearSystemProxy()
	if e.process != nil && e.process.Process != nil {
		_ = e.process.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() {
			e.process.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = e.process.Process.Kill()
			<-done
		}
		e.process = nil
	}
	if e.lastConfigPath != "" {
		_ = os.Remove(e.lastConfigPath)
		e.lastConfigPath = ""
	}
	e.currentNode = nil
	e.setStatus(Disconnected)
	log.Println("Disconnected")
	return nil
}

func (e *Engine) QuickConnect() error {
	servers, _ := e.GetServers()
	if len(servers) > 0 {
		return e.Connect(servers[0].ID)
	}
	return fmt.Errorf("no servers available")
}

// GetTrafficStats реализован в traffic.go (gopsutil + TUN-интерфейс).

func (e *Engine) writeTempConfig(cfg string) (string, error) {
	dir := filepath.Join(e.store.DataDir(), "runtime")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, fmt.Sprintf("sing-box-%d-*.json", time.Now().UnixNano()))
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.WriteString(cfg); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	e.lastConfigPath = path
	return path, nil
}

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const subscriptionsFile = "subscriptions.json"
const settingsFile = "settings.json"

type Settings struct {
	// SingBoxPath — полный путь до sing-box (например, C:\Tools\sing-box\sing-box.exe).
	// Если пусто — будет использован поиск по PATH (и/или NEKKUS_SINGBOX_PATH).
	SingBoxPath string `json:"sing_box_path,omitempty"`

	// Эти поля нужны UI, чтобы запоминать выбор пользователя.
	DefaultConfigID string `json:"default_config_id,omitempty"`
	DefaultServer   string `json:"default_server,omitempty"`
}

// Subscription и ServerNode — типы для VPN (используются engine и API).
type ServerNode struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Country string `json:"country"`
	Ping    int    `json:"ping"`
	URI     string `json:"uri,omitempty"`
}

type Subscription struct {
	ID        string       `json:"id"`
	URL       string       `json:"url"`
	Name      string       `json:"name"`
	Servers   []ServerNode `json:"servers"`
	UpdatedAt int64        `json:"updated_at"`
}

type Store struct {
	mu            sync.RWMutex
	dataDir       string
	subscriptions []Subscription
	servers       []ServerNode
	settings      Settings
}

func New(dataDir string) (*Store, error) {
	s := &Store{
		dataDir:       dataDir,
		subscriptions: []Subscription{},
		servers:       []ServerNode{},
		settings:      Settings{},
	}
	if err := s.loadSubscriptions(); err != nil {
		return nil, err
	}
	if err := s.loadSettings(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) loadSubscriptions() error {
	path := filepath.Join(s.dataDir, subscriptionsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var list []Subscription
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	s.mu.Lock()
	s.subscriptions = list
	s.mu.Unlock()
	return nil
}

func (s *Store) writeSubscriptions(list []Subscription) error {
	path := filepath.Join(s.dataDir, subscriptionsFile)
	if err := os.MkdirAll(s.dataDir, 0750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func (s *Store) saveSubscriptions() error {
	s.mu.RLock()
	list := make([]Subscription, len(s.subscriptions))
	copy(list, s.subscriptions)
	s.mu.RUnlock()
	return s.writeSubscriptions(list)
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) DataDir() string {
	return s.dataDir
}

func (s *Store) loadSettings() error {
	path := filepath.Join(s.dataDir, settingsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var next Settings
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	return nil
}

func (s *Store) saveSettings(next Settings) error {
	path := filepath.Join(s.dataDir, settingsFile)
	if err := os.MkdirAll(s.dataDir, 0750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	return nil
}

func (s *Store) GetSettings() (Settings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings, nil
}

func (s *Store) UpdateSettings(patch Settings) (Settings, error) {
	s.mu.RLock()
	next := s.settings
	s.mu.RUnlock()

	if patch.SingBoxPath != "" {
		next.SingBoxPath = patch.SingBoxPath
	}
	if patch.DefaultConfigID != "" {
		next.DefaultConfigID = patch.DefaultConfigID
	}
	if patch.DefaultServer != "" {
		next.DefaultServer = patch.DefaultServer
	}
	if err := s.saveSettings(next); err != nil {
		return Settings{}, err
	}
	return next, nil
}

func (s *Store) AddSubscription(name, url string) (*Subscription, error) {
	s.mu.Lock()
	now := time.Now().Unix()
	id := "sub-" + strconv.FormatInt(now, 10) + "-" + strconv.FormatInt(int64(len(s.subscriptions)), 10)
	sub := Subscription{
		ID:        id,
		Name:      name,
		URL:       url,
		Servers:   nil,
		UpdatedAt: now,
	}
	s.subscriptions = append(s.subscriptions, sub)
	list := make([]Subscription, len(s.subscriptions))
	copy(list, s.subscriptions)
	s.mu.Unlock()
	if err := s.writeSubscriptions(list); err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *Store) GetSubscription(id string) (*Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			sub := s.subscriptions[i]
			return &sub, nil
		}
	}
	return nil, fmt.Errorf("subscription not found: %s", id)
}

func (s *Store) UpdateSubscriptionServers(id string, servers []ServerNode) error {
	s.mu.Lock()
	var found bool
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			s.subscriptions[i].Servers = servers
			found = true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		return fmt.Errorf("subscription not found: %s", id)
	}
	return s.saveSubscriptions()
}

func (s *Store) GetSubscriptions() ([]Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.subscriptions) == 0 {
		return []Subscription{}, nil
	}
	result := make([]Subscription, len(s.subscriptions))
	copy(result, s.subscriptions)
	return result, nil
}

// GetServers возвращает все серверы: из плоского списка s.servers (если есть) или из всех подписок.
func (s *Store) GetServers() ([]ServerNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.servers) > 0 {
		result := make([]ServerNode, len(s.servers))
		copy(result, s.servers)
		return result, nil
	}
	var result []ServerNode
	seen := make(map[string]bool)
	for _, sub := range s.subscriptions {
		for _, n := range sub.Servers {
			if n.ID != "" && !seen[n.ID] {
				seen[n.ID] = true
				result = append(result, n)
			}
		}
	}
	return result, nil
}

func (s *Store) GetServer(serverID string) (*ServerNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.servers {
		if s.servers[i].ID == serverID {
			n := s.servers[i]
			return &n, nil
		}
	}
	for _, sub := range s.subscriptions {
		for i := range sub.Servers {
			if sub.Servers[i].ID == serverID || sub.Servers[i].Name == serverID {
				n := sub.Servers[i]
				return &n, nil
			}
		}
	}
	return nil, fmt.Errorf("server not found: %s", serverID)
}

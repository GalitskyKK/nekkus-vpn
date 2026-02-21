package vpn

import (
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/net"
)

// Имена интерфейсов TUN/TAP (sing-box, Wintun, utun и т.д.)
// На Windows Wintun может быть "Wintun" или имя из пула; на Linux — tun0, wg0 и т.д.
var tunNamePrefixes = []string{"tun", "wintun", "utun", "tap", "wg-", "wireguard", "vpn", "sing"}

// Исключаем из суммы "все интерфейсы" (loopback и типичные физические)
var excludePrefixes = []string{"loopback", "lo", "bluetooth", "vmware", "vbox", "virtualbox"}

func (e *Engine) getTUNBytes() (recv, sent uint64, ok bool) {
	counters, err := net.IOCounters(true)
	if err != nil {
		return 0, 0, false
	}
	for i := range counters {
		name := strings.ToLower(counters[i].Name)
		for _, prefix := range tunNamePrefixes {
			if strings.Contains(name, prefix) {
				return counters[i].BytesRecv, counters[i].BytesSent, true
			}
		}
	}
	// Запасной вариант: при подключённом VPN суммируем все интерфейсы кроме loopback/виртуалок
	if e.GetStatus() != Connected {
		return 0, 0, false
	}
	var sumRecv, sumSent uint64
	for i := range counters {
		name := strings.ToLower(counters[i].Name)
		skip := false
		for _, ex := range excludePrefixes {
			if strings.Contains(name, ex) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		sumRecv += counters[i].BytesRecv
		sumSent += counters[i].BytesSent
	}
	return sumRecv, sumSent, true
}

var (
	trafficMu     sync.Mutex
	lastRecv      uint64
	lastSent      uint64
	lastTrafficAt time.Time
)

func (e *Engine) GetTrafficStats() (*TrafficStats, error) {
	if e.GetStatus() != Connected {
		trafficMu.Lock()
		lastTrafficAt = time.Time{}
		trafficMu.Unlock()
		return &TrafficStats{}, nil
	}
	recv, sent, ok := e.getTUNBytes()
	now := time.Now()

	trafficMu.Lock()
	defer trafficMu.Unlock()

	out := &TrafficStats{
		Download:  int64(recv),
		Upload:    int64(sent),
		StartedAt: 0,
	}
	if ok && !lastTrafficAt.IsZero() {
		elapsed := now.Sub(lastTrafficAt).Seconds()
		if elapsed > 0 {
			out.DownloadSpeed = int64(float64(int64(recv)-int64(lastRecv)) / elapsed)
			out.UploadSpeed = int64(float64(int64(sent)-int64(lastSent)) / elapsed)
			if out.DownloadSpeed < 0 {
				out.DownloadSpeed = 0
			}
			if out.UploadSpeed < 0 {
				out.UploadSpeed = 0
			}
		}
	}
	lastRecv = recv
	lastSent = sent
	lastTrafficAt = now
	return out, nil
}

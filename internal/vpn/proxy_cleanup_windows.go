//go:build windows

package vpn

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// setSystemProxy включает системный прокси в Windows (HKCU): host:port.
// Вызывается при Connect после старта sing-box.
func setSystemProxy(host string, port int) {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	proxyServer := fmt.Sprintf("%s:%d", host, port)
	_ = k.SetStringValue("ProxyServer", proxyServer)
	_ = k.SetDWordValue("ProxyEnable", 1)
}

// clearSystemProxy отключает системный прокси в Windows (HKCU).
// Вызывается при Disconnect.
func clearSystemProxy() {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetDWordValue("ProxyEnable", 0)
}

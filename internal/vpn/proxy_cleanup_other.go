//go:build !windows

package vpn

// setSystemProxy — no-op на не-Windows; sing-box при set_system_proxy сам выставляет прокси.
func setSystemProxy(host string, port int) {}

// clearSystemProxy — no-op на не-Windows.
func clearSystemProxy() {}

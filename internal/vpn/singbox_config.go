package vpn

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/GalitskyKK/nekkus-net/internal/store"
)

type singBoxConfig struct {
	Log      map[string]any   `json:"log,omitempty"`
	Inbounds []map[string]any `json:"inbounds"`
	Outbounds []map[string]any `json:"outbounds"`
	Route    map[string]any   `json:"route,omitempty"`
}

func (e *Engine) generateSingboxConfig(server *store.ServerNode) (string, error) {
	// mixed inbound умеет сам выставлять системный прокси на Windows/macOS/Linux (set_system_proxy=true)
	inbound := map[string]any{
		"type":             "mixed",
		"tag":              "mixed-in",
		"listen":           "127.0.0.1",
		"listen_port":      7890,
		"set_system_proxy": true,
	}

	outbound, err := outboundFromURI(server.URI)
	if err != nil {
		return "", fmt.Errorf("unsupported/invalid server URI (refresh subscription?): %w", err)
	}
	outbound["tag"] = "proxy"

	cfg := singBoxConfig{
		Log: map[string]any{
			"level": "info",
		},
		Inbounds: []map[string]any{inbound},
		Outbounds: []map[string]any{
			outbound,
			{"type": "direct", "tag": "direct"},
			{"type": "block", "tag": "block"},
		},
		Route: map[string]any{
			"final": "proxy",
		},
	}

	encoded, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func outboundFromURI(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty server uri")
	}

	if strings.HasPrefix(raw, "vmess://") {
		return vmessOutbound(raw)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(parsed.Scheme) {
	case "vless":
		return vlessOutbound(parsed)
	case "trojan":
		return trojanOutbound(parsed)
	case "ss":
		return shadowsocksOutbound(parsed)
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
	}
}

func splitHostPort(hostport string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		// might be missing port
		if hostport != "" && !strings.Contains(hostport, ":") {
			return hostport, 0, nil
		}
		return "", 0, err
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, p, nil
}

func vlessOutbound(u *url.URL) (map[string]any, error) {
	host, port, err := splitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	if port == 0 {
		port = 443
	}

	uuid := ""
	if u.User != nil {
		uuid = u.User.Username()
	}
	if uuid == "" {
		return nil, fmt.Errorf("vless: missing uuid")
	}

	q := u.Query()
	security := strings.ToLower(q.Get("security")) // tls / reality / ""
	transportType := strings.ToLower(q.Get("type")) // ws / grpc / tcp
	flow := q.Get("flow")
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("host")
	}
	if sni == "" && host != "" {
		sni = host
	}

	out := map[string]any{
		"type":        "vless",
		"server":      host,
		"server_port": port,
		"uuid":        uuid,
	}
	if flow != "" {
		out["flow"] = flow
	}

	if security == "tls" || security == "reality" {
		tls := map[string]any{"enabled": true}
		if sni != "" {
			tls["server_name"] = sni
		}
		if alpn := q.Get("alpn"); alpn != "" {
			tls["alpn"] = strings.Split(alpn, ",")
		}
		if security == "reality" {
			pbk := q.Get("pbk")
			sid := q.Get("sid")
			if pbk != "" && sid != "" {
				tls["reality"] = map[string]any{
					"enabled":    true,
					"public_key": pbk,
					"short_id":   sid,
				}
				// Reality client требует uTLS (fingerprint браузера).
				fp := q.Get("fp")
				if fp == "" {
					fp = "chrome"
				}
				tls["utls"] = map[string]any{
					"enabled":    true,
					"fingerprint": fp,
				}
			}
		}
		out["tls"] = tls
	}

	transport, err := v2rayTransport(transportType, q)
	if err != nil {
		return nil, err
	}
	if transport != nil {
		out["transport"] = transport
	}
	return out, nil
}

func trojanOutbound(u *url.URL) (map[string]any, error) {
	host, port, err := splitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	if port == 0 {
		port = 443
	}

	password := ""
	if u.User != nil {
		password = u.User.Username()
	}
	if password == "" {
		return nil, fmt.Errorf("trojan: missing password")
	}
	q := u.Query()
	transportType := strings.ToLower(q.Get("type"))

	tls := map[string]any{"enabled": true}
	if sni := q.Get("sni"); sni != "" {
		tls["server_name"] = sni
	} else if host != "" {
		tls["server_name"] = host
	}
	if alpn := q.Get("alpn"); alpn != "" {
		tls["alpn"] = strings.Split(alpn, ",")
	}
	out := map[string]any{
		"type":        "trojan",
		"server":      host,
		"server_port": port,
		"password":    password,
		"tls":         tls,
	}
	transport, err := v2rayTransport(transportType, q)
	if err != nil {
		return nil, err
	}
	if transport != nil {
		out["transport"] = transport
	}
	return out, nil
}

func shadowsocksOutbound(u *url.URL) (map[string]any, error) {
	// ss:// might be "ss://BASE64(method:pass)@host:port#name" or "ss://method:pass@host:port"
	raw := u.String()
	raw = strings.TrimPrefix(raw, "ss://")
	parts := strings.SplitN(raw, "#", 2)
	raw = parts[0]

	// parse via url to get host
	host, port, err := splitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	if port == 0 {
		port = 8388
	}

	userInfo := ""
	if u.User != nil {
		userInfo = u.User.String()
	}

	method := ""
	password := ""

	if userInfo != "" && strings.Contains(userInfo, ":") {
		// already decoded form
		decoded, _ := url.QueryUnescape(userInfo)
		m, p, ok := strings.Cut(decoded, ":")
		if ok {
			method = m
			password = p
		}
	} else {
		// try base64 from opaque/host prefix before '@'
		beforeAt, _, ok := strings.Cut(raw, "@")
		if ok {
			decodedBytes, err := decodeBase64Compat(beforeAt)
			if err == nil {
				m, p, ok2 := strings.Cut(string(decodedBytes), ":")
				if ok2 {
					method = m
					password = p
				}
			}
		}
	}

	if method == "" || password == "" {
		return nil, fmt.Errorf("shadowsocks: cannot parse method/password")
	}

	return map[string]any{
		"type":        "shadowsocks",
		"server":      host,
		"server_port": port,
		"method":      method,
		"password":    password,
	}, nil
}

func vmessOutbound(raw string) (map[string]any, error) {
	payload := strings.TrimPrefix(strings.TrimSpace(raw), "vmess://")
	decoded, err := decodeBase64Compat(payload)
	if err != nil {
		return nil, err
	}
	var v struct {
		Add  string `json:"add"`
		Port string `json:"port"`
		ID   string `json:"id"`
		Aid  string `json:"aid"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		SNI  string `json:"sni"`
	}
	if err := json.Unmarshal(decoded, &v); err != nil {
		return nil, err
	}
	if v.Add == "" || v.ID == "" || v.Port == "" {
		return nil, fmt.Errorf("vmess: missing add/id/port")
	}
	port, err := strconv.Atoi(v.Port)
	if err != nil {
		return nil, err
	}

	out := map[string]any{
		"type":        "vmess",
		"server":      v.Add,
		"server_port": port,
		"uuid":        v.ID,
		"security":    "auto",
		"alter_id":    0,
	}

	if v.Aid != "" {
		if aid, err := strconv.Atoi(v.Aid); err == nil {
			out["alter_id"] = aid
		}
	}

	if strings.EqualFold(v.TLS, "tls") {
		tls := map[string]any{"enabled": true}
		serverName := v.SNI
		if serverName == "" {
			serverName = v.Host
		}
		if serverName == "" && v.Add != "" {
			serverName = v.Add
		}
		if serverName != "" {
			tls["server_name"] = serverName
		}
		out["tls"] = tls
	}

	q := url.Values{}
	if v.Path != "" {
		q.Set("path", v.Path)
	}
	if v.Host != "" {
		q.Set("host", v.Host)
	}
	transport, err := v2rayTransport(strings.ToLower(v.Net), q)
	if err != nil {
		return nil, err
	}
	if transport != nil {
		out["transport"] = transport
	}

	return out, nil
}

func v2rayTransport(kind string, q url.Values) (map[string]any, error) {
	switch kind {
	case "", "tcp":
		return nil, nil
	case "ws", "websocket":
		ws := map[string]any{
			"type": "ws",
		}
		if path := q.Get("path"); path != "" {
			ws["path"] = path
		}
		if host := q.Get("host"); host != "" {
			ws["headers"] = map[string]any{"Host": host}
		}
		return ws, nil
	case "grpc":
		grpc := map[string]any{
			"type": "grpc",
		}
		if serviceName := q.Get("serviceName"); serviceName != "" {
			grpc["service_name"] = serviceName
		}
		return grpc, nil
	default:
		return nil, fmt.Errorf("unsupported transport: %s", kind)
	}
}

func decodeBase64Compat(input string) ([]byte, error) {
	input = strings.TrimSpace(input)
	input = strings.ReplaceAll(input, "-", "+")
	input = strings.ReplaceAll(input, "_", "/")
	// pad
	if m := len(input) % 4; m != 0 {
		input += strings.Repeat("=", 4-m)
	}
	return base64.StdEncoding.DecodeString(input)
}


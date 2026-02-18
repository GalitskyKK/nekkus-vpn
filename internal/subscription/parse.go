package subscription

import (
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"

	"github.com/GalitskyKK/nekkus-net/internal/store"
)

// ParseContent парсит тело подписки (сырой или base64) и возвращает список серверов.
func ParseContent(body string) ([]store.ServerNode, error) {
	content := strings.TrimSpace(body)
	if content == "" {
		return nil, nil
	}
	// Сначала как plain list URI по строкам
	uris := extractURIList(content)
	if len(uris) == 0 {
		decoded, err := tryDecodeBase64(content)
		if err != nil {
			return nil, nil
		}
		uris = extractURIList(decoded)
	}
	return urisToServerNodes(uris), nil
}

func tryDecodeBase64(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err == nil {
		return string(decoded), nil
	}
	decoded, err = base64.RawStdEncoding.DecodeString(trimmed)
	if err == nil {
		return string(decoded), nil
	}
	return "", err
}

func extractURIList(content string) []string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "://") {
			result = append(result, line)
		}
	}
	return result
}

func urisToServerNodes(uris []string) []store.ServerNode {
	seen := make(map[string]bool)
	out := make([]store.ServerNode, 0, len(uris))
	for i, raw := range uris {
		name := extractNameFromURI(raw)
		name = strings.TrimSpace(name)
		if name == "" {
			name = "server-" + strconv.Itoa(i+1)
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		addr := extractHostFromURI(raw)
		id := name
		if addr != "" {
			id = name + "-" + addr
		}
		out = append(out, store.ServerNode{
			ID:      id,
			Name:    name,
			Address: addr,
			Country: "",
			Ping:    0,
			URI:     raw,
		})
	}
	return out
}

func extractNameFromURI(raw string) string {
	parts := strings.SplitN(raw, "#", 2)
	if len(parts) == 2 && parts[1] != "" {
		if decoded, err := url.QueryUnescape(parts[1]); err == nil {
			return decoded
		}
		return strings.TrimSpace(parts[1])
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Host != "" {
		return parsed.Host
	}
	if parsed.Opaque != "" {
		if at := strings.Index(parsed.Opaque, "@"); at >= 0 && at+1 < len(parsed.Opaque) {
			return parsed.Opaque[at+1:]
		}
		return parsed.Opaque
	}
	return ""
}

func extractHostFromURI(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Host != "" {
		if host, _, _ := strings.Cut(parsed.Host, ":"); host != "" {
			return host
		}
		return parsed.Host
	}
	if parsed.Opaque != "" {
		// uuid@host:port или user@host:port
		afterAt := parsed.Opaque
		if at := strings.Index(afterAt, "@"); at >= 0 {
			afterAt = afterAt[at+1:]
		}
		if host, _, _ := strings.Cut(afterAt, ":"); host != "" {
			return host
		}
		return afterAt
	}
	return ""
}

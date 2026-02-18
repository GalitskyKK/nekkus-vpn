package ui

import "embed"

// Содержимое frontend/dist. Для сборки: соберите фронт в ui/frontend/dist
// (например: mv frontend ui/ && cd ui/frontend && npm run build)
//go:embed all:frontend/dist
var Assets embed.FS

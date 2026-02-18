package assets

import _ "embed"

//go:embed icon.png
var TrayIconPNG []byte

var TrayIcon = TrayIconPNG

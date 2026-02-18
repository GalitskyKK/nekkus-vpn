# Копирует sing-box из папки на рабочем столе (sign-box) в nekkus-net/sing-box/,
# чтобы при запуске nekkus-net.exe рядом был sing-box — «скачал и включил» без кнопки «Установить».
# Запуск: из корня nekkus-net выполнить: .\scripts\link-sing-box.ps1
# Источник по умолчанию: $env:USERPROFILE\Desktop\sign-box

param(
    [string]$Source = (Join-Path $env:USERPROFILE "Desktop\sign-box")
)

$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
$targetDir = Join-Path $root "sing-box"

if (-not (Test-Path $Source)) {
    Write-Error "Source folder not found: $Source. Use -Source path to folder with unpacked sing-box."
}

$winDirs = Get-ChildItem -Path $Source -Directory -Filter "sing-box-*-windows-amd64" -ErrorAction SilentlyContinue
if (-not $winDirs -or $winDirs.Count -eq 0) {
    Write-Error "Folder sing-box-*-windows-amd64 not found in $Source. Unpack sing-box Windows archive into sign-box."
}
$inner = Get-ChildItem -Path $winDirs[0].FullName -Directory -Filter "sing-box-*-windows-amd64" -ErrorAction SilentlyContinue
$sourceDir = if ($inner) { $inner[0].FullName } else { $winDirs[0].FullName }

New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
Copy-Item -Path (Join-Path $sourceDir "*") -Destination $targetDir -Recurse -Force
Write-Host "OK: copied from $sourceDir to $targetDir"
Write-Host "Run nekkus-net.exe from $root - sing-box will be used automatically."

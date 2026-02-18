# Собирает папки для релизов: каждая платформа — nekkus-net + sing-box/ с бинарником для этой ОС.
# Источник sing-box по умолчанию: ../sing-box (папка nekkus/sing-box при запуске из корня nekkus-net).
# Запуск: из корня nekkus-net: .\scripts\pack-releases.ps1
# Результат: папка release\ с подпапками windows-amd64, linux-amd64, darwin-amd64, darwin-arm64.

param(
    [string]$Source = (Join-Path (Split-Path $PSScriptRoot -Parent) "..\sing-box"),
    [string]$OutDir = "release"
)

$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root

if (-not (Test-Path $Source)) {
    Write-Error "Source folder not found: $Source. Use -Source path to folder with unpacked sing-box."
}

$platforms = @(
    @{ Name = "windows-amd64"; GoOs = "windows"; GoArch = "amd64"; Bin = "nekkus-net.exe"; SingBoxPattern = "*-windows-amd64" }
    @{ Name = "linux-amd64";   GoOs = "linux";   GoArch = "amd64"; Bin = "nekkus-net";   SingBoxPattern = "*-linux-amd64" }
    @{ Name = "darwin-amd64";  GoOs = "darwin";  GoArch = "amd64"; Bin = "nekkus-net";   SingBoxPattern = "*-darwin-amd64" }
    @{ Name = "darwin-arm64";  GoOs = "darwin";  GoArch = "arm64"; Bin = "nekkus-net";   SingBoxPattern = "*-darwin-arm64" }
)

$releaseRoot = Join-Path $root $OutDir
New-Item -ItemType Directory -Path $releaseRoot -Force | Out-Null

foreach ($p in $platforms) {
    $dir = Join-Path $releaseRoot $p.Name
    New-Item -ItemType Directory -Path $dir -Force | Out-Null
    $singBoxDir = Join-Path $dir "sing-box"
    New-Item -ItemType Directory -Path $singBoxDir -Force | Out-Null

    # Копируем sing-box из Desktop\sign-box
    $srcDirs = Get-ChildItem -Path $Source -Directory -Filter $p.SingBoxPattern -ErrorAction SilentlyContinue
    if ($srcDirs) {
        $inner = Get-ChildItem -Path $srcDirs[0].FullName -Directory -Filter $p.SingBoxPattern -ErrorAction SilentlyContinue
        $from = if ($inner) { $inner[0].FullName } else { $srcDirs[0].FullName }
        Copy-Item -Path (Join-Path $from "*") -Destination $singBoxDir -Recurse -Force
        Write-Host "[$($p.Name)] sing-box copied from $from"
    } else {
        Write-Warning "[$($p.Name)] folder $($p.SingBoxPattern) not found in $Source"
    }

    # Сборка nekkus-net для платформы (если не Windows — нужен кросс-компилятор)
    $binPath = Join-Path $dir $p.Bin
    if ($p.GoOs -eq "windows" -and $p.GoArch -eq "amd64") {
        go build -o $binPath ./cmd
        if ($LASTEXITCODE -eq 0) { Write-Host "[$($p.Name)] nekkus-net built: $binPath" }
    } else {
        $env:GOOS = $p.GoOs
        $env:GOARCH = $p.GoArch
        go build -o $binPath ./cmd
        $env:GOOS = $null
        $env:GOARCH = $null
        if ($LASTEXITCODE -eq 0) { Write-Host "[$($p.Name)] nekkus-net built: $binPath" }
    }
}

Write-Host "Done. Folders: $releaseRoot"
Write-Host "Next: archive each subfolder (windows-amd64.zip, linux-amd64.tar.gz, etc.) and upload."

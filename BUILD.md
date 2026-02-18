# Сборка и запуск nekkus-net (по fixed-плану)

**Одна точка входа:** `cmd/main.go` — Go‑сервер (nekkus-core) + встроенный UI + tray (desktop.Launch). Wails не используется.

---

## Сборка

```bash
cd nekkus-net
go build -o nekkus-net.exe ./cmd
```

Если менял фронт — собрать UI (сборка пишется сразу в `ui/frontend/dist`, копировать не нужно):

```bash
cd frontend && npm run build && cd ..
go build -o nekkus-net.exe ./cmd
```

---

## Запуск

```bash
./nekkus-net.exe
```

- По умолчанию HTTP на порту **9001**. Открыть в браузере: **http://localhost:9001**
- Другой порт: `./nekkus-net.exe --port 9002` → http://localhost:9002
- Без окна (только сервер): `./nekkus-net.exe --headless`

Данные (подписки): **%APPDATA%\nekkus\net** (Windows).

---

## Портативный релиз («скачал и включил»)

Если рядом с `nekkus-net.exe` лежит папка **sing-box/** с бинарником внутри, приложение подхватит его без установки через UI:

```
nekkus-net.exe
sing-box/
  sing-box.exe   # Windows
  sing-box       # Linux/macOS
```

Порядок поиска sing-box: `NEKKUS_SINGBOX_PATH` → настройки (путь из UI) → **папка sing-box/ рядом с exe** → PATH.

**Если ты скачал sing-box (Windows + Linux + Mac) в одну папку на рабочий стол** (например `Desktop\sign-box` с подпапками `sing-box-1.x.x-windows-amd64`, `sing-box-1.x.x-linux-amd64` и т.д.):

1. Из корня **nekkus-net** выполни один раз:
   ```powershell
   .\scripts\link-sing-box.ps1
   ```
   Скрипт скопирует Windows-бинарник sing-box в `nekkus-net\sing-box\` — после этого запуск `nekkus-net.exe` из папки nekkus-net подхватит sing-box без кнопки «Установить».
2. Чтобы собрать релиз-архивы под все платформы (Windows, Linux, Mac Intel, Mac ARM):
   ```powershell
   .\scripts\pack-releases.ps1
   ```
   В папке `release\` появятся подпапки `windows-amd64`, `linux-amd64`, `darwin-amd64`, `darwin-arm64` — в каждой nekkus-net + sing-box для этой ОС. Остаётся заархивировать каждую и выложить.

---

## Релизы под все платформы (Windows, Linux, macOS)

**Стратегия:** один архив на платформу (и при необходимости архитектуру). Пользователь качает свой вариант — внутри уже nekkus-net + sing-box для этой ОС.

| Артефакт | Содержимое |
|----------|------------|
| `nekkus-net-windows-amd64.zip` | `nekkus-net.exe` + `sing-box/sing-box.exe` (и DLL при необходимости) |
| `nekkus-net-linux-amd64.tar.gz` | `nekkus-net` + `sing-box/sing-box` |
| `nekkus-net-darwin-amd64.zip` | `nekkus-net` + `sing-box/sing-box` (Intel Mac) |
| `nekkus-net-darwin-arm64.zip` | `nekkus-net` + `sing-box/sing-box` (Apple Silicon) |

**Сборка вручную (пример для своей ОС):**

1. Собрать nekkus-net: `go build -o nekkus-net ./cmd` (на Linux/macOS без `.exe`).
2. Скачать [sing-box release](https://github.com/SagerNet/sing-box/releases) под ту же платформу:
   - Windows: `sing-box-*-windows-amd64.zip`
   - Linux: `sing-box-*-linux-amd64.tar.gz`
   - macOS Intel: `sing-box-*-darwin-amd64.tar.gz`
   - macOS Apple Silicon: `sing-box-*-darwin-arm64.tar.gz`
3. Распаковать sing-box в папку **`sing-box/`** рядом с бинарником nekkus-net (внутри архива sing-box часто лежит в подпапке вроде `sing-box-1.x.x-linux-amd64/` — нужен именно бинарник `sing-box` или `sing-box.exe` в каталоге `sing-box/`).
4. Упаковать в zip или tar.gz: корень архива — бинарник nekkus-net и папка `sing-box/`.

**Автоматизация:** в CI (GitHub Actions и т.п.) для каждой пары (GOOS, GOARCH) выполнить: сборка nekkus-net → скачать нужный ассет sing-box по имени (как в `internal/deps/singbox/installer.go`) → распаковать в `sing-box/` → упаковать артефакт. Имена ассетов см. [releases](https://github.com/SagerNet/sing-box/releases) (например `sing-box-1.12.22-windows-amd64.zip`).

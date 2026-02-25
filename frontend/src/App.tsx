import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  AppShell,
  Button,
  Card,
  DataText,
  Input,
  PageLayout,
  Pill,
  Section,
  Select,
  StatusDot,
} from '@nekkus/ui-kit'
import {
  addConfig,
  addSubscription,
  connectVPN,
  disconnectVPN,
  fetchConfigs,
  fetchLogs,
  fetchSingBoxStatus,
  fetchSettings,
  fetchServers,
  fetchStatus,
  fetchSubscriptions,
  installSingBox,
  refreshSubscriptions,
  updateSettings,
} from './api'
import type { SingBoxStatus, Subscription, VpnConfig, VpnSettings, VpnStatus } from './types'

const statusRefreshMs = 2000

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(1)} ${['B', 'KB', 'MB', 'GB', 'TB'][i]}`
}

function formatSpeed(bytesPerSec: number): string {
  return `${formatBytes(bytesPerSec)}/s`
}

function App() {
  const [status, setStatus] = useState<VpnStatus | null>(null)
  const [configs, setConfigs] = useState<VpnConfig[]>([])
  const [subscriptions, setSubscriptions] = useState<Subscription[]>([])
  const [settings, setSettings] = useState<VpnSettings | null>(null)
  const [singBoxStatus, setSingBoxStatus] = useState<SingBoxStatus | null>(null)
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [isBusy, setIsBusy] = useState(false)

  const [configName, setConfigName] = useState('')
  const [configContent, setConfigContent] = useState('')
  const [subscriptionName, setSubscriptionName] = useState('')
  const [subscriptionUrl, setSubscriptionUrl] = useState('')
  const [connectConfigId, setConnectConfigId] = useState('')
  const [connectServer, setConnectServer] = useState('')
  const [availableServers, setAvailableServers] = useState<string[]>([])
  const [selectedServer, setSelectedServer] = useState('')
  const [preferredServer, setPreferredServer] = useState('')
  const [defaultsApplied, setDefaultsApplied] = useState(false)
  const [logs, setLogs] = useState<string[]>([])
  const [logsVisible, setLogsVisible] = useState(false)

  const activeConfig = useMemo(
    () => configs.find((c) => c.id === status?.activeConfigId),
    [configs, status?.activeConfigId],
  )

  const loadAll = useCallback(async () => {
    try {
      setErrorMessage(null)
      const [nextStatus, nextConfigs, nextSubscriptions, nextSettings, nextSingBoxStatus] =
        await Promise.all([
          fetchStatus(),
          fetchConfigs(),
          fetchSubscriptions(),
          fetchSettings(),
          fetchSingBoxStatus(),
        ])
      setStatus(nextStatus)
      setConfigs(Array.isArray(nextConfigs) ? nextConfigs : [])
      setSubscriptions(Array.isArray(nextSubscriptions) ? nextSubscriptions : [])
      setSettings(nextSettings ?? {})
      setSingBoxStatus(nextSingBoxStatus ?? null)
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : 'Failed to load data')
    }
  }, [])

  useEffect(() => {
    void loadAll()
  }, [loadAll])

  useEffect(() => {
    const intervalId = window.setInterval(async () => {
      try {
        const nextStatus = await fetchStatus()
        setStatus(nextStatus)
      } catch (error) {
        setErrorMessage(error instanceof Error ? error.message : 'Failed to refresh status')
      }
    }, statusRefreshMs)
    return () => window.clearInterval(intervalId)
  }, [])

  useEffect(() => {
    if (!logsVisible) return
    let cancelled = false
    const loadLogs = async () => {
      try {
        const nextLogs = await fetchLogs()
        if (!cancelled) setLogs(nextLogs)
      } catch (error) {
        if (!cancelled) {
          setErrorMessage(error instanceof Error ? error.message : 'Не удалось загрузить логи')
        }
      }
    }
    void loadLogs()
    const intervalId = window.setInterval(loadLogs, 2000)
    return () => {
      cancelled = true
      window.clearInterval(intervalId)
    }
  }, [logsVisible])

  useEffect(() => {
    if (!connectConfigId) {
      setAvailableServers([])
      setSelectedServer('')
      return
    }
    let cancelled = false
    const loadServers = async () => {
      try {
        const servers = await fetchServers(connectConfigId)
        if (!cancelled) {
          setAvailableServers(servers)
          if (preferredServer && servers.includes(preferredServer)) {
            setSelectedServer(preferredServer)
          } else {
            setSelectedServer(servers[0] ?? '')
          }
        }
      } catch (error) {
        if (!cancelled) {
          setAvailableServers([])
          setSelectedServer('')
          setErrorMessage(error instanceof Error ? error.message : 'Не удалось загрузить серверы')
        }
      }
    }
    void loadServers()
    return () => {
      cancelled = true
    }
  }, [connectConfigId, preferredServer])

  useEffect(() => {
    if (defaultsApplied || !settings) return
    if (settings.default_config_id && !connectConfigId) {
      setConnectConfigId(settings.default_config_id)
    }
    if (settings.default_server) {
      setPreferredServer(settings.default_server)
      if (!availableServers.length) {
        setConnectServer(settings.default_server)
      }
    }
    setDefaultsApplied(true)
  }, [availableServers.length, connectConfigId, defaultsApplied, settings])

  const handleCreateConfig = useCallback(async () => {
    if (!configName.trim() || !configContent.trim()) {
      setErrorMessage('Укажи имя и контент конфига')
      return
    }
    try {
      setIsBusy(true)
      setErrorMessage(null)
      const created = await addConfig({ name: configName.trim(), content: configContent.trim() })
      setConfigs((prev) => [created, ...prev])
      setConfigName('')
      setConfigContent('')
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : 'Не удалось добавить конфиг')
    } finally {
      setIsBusy(false)
    }
  }, [configContent, configName])

  const handleCreateSubscription = useCallback(async () => {
    if (!subscriptionName.trim() || !subscriptionUrl.trim()) {
      setErrorMessage('Укажи имя и URL подписки')
      return
    }
    try {
      setIsBusy(true)
      setErrorMessage(null)
      const created = await addSubscription({
        name: subscriptionName.trim(),
        url: subscriptionUrl.trim(),
      })
      setSubscriptions((prev) => [created, ...prev])
      await loadAll()
      setSubscriptionName('')
      setSubscriptionUrl('')
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : 'Не удалось добавить подписку')
    } finally {
      setIsBusy(false)
    }
  }, [loadAll, subscriptionName, subscriptionUrl])

  const handleConnect = useCallback(async () => {
    if (!connectConfigId && !connectServer.trim() && !selectedServer) {
      setErrorMessage('Выбери конфиг или задай сервер')
      return
    }
    try {
      setIsBusy(true)
      setErrorMessage(null)
      const nextStatus = await connectVPN({
        config_id: connectConfigId || undefined,
        server: selectedServer || connectServer.trim() || undefined,
      })
      setStatus(nextStatus)
      if (connectConfigId) {
        const nextServer = selectedServer || connectServer.trim()
        const nextSettings = await updateSettings({
          default_config_id: connectConfigId,
          default_server: nextServer,
        })
        setSettings(nextSettings)
        setPreferredServer(nextServer)
      }
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : 'Не удалось подключиться')
    } finally {
      setIsBusy(false)
    }
  }, [connectConfigId, connectServer, selectedServer])

  const handleInstallSingBox = useCallback(async () => {
    try {
      setIsBusy(true)
      setErrorMessage(null)
      const nextStatus = await installSingBox()
      setSingBoxStatus(nextStatus)
      await loadAll()
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : 'Не удалось установить sing-box')
    } finally {
      setIsBusy(false)
    }
  }, [loadAll])

  const handleDisconnect = useCallback(async () => {
    try {
      setIsBusy(true)
      setErrorMessage(null)
      const nextStatus = await disconnectVPN()
      setStatus(nextStatus)
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : 'Не удалось отключиться')
    } finally {
      setIsBusy(false)
    }
  }, [])

  const handleRefreshSubscriptions = useCallback(async () => {
    try {
      setIsBusy(true)
      setErrorMessage(null)
      await refreshSubscriptions()
      await loadAll()
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : 'Не удалось обновить подписки')
    } finally {
      setIsBusy(false)
    }
  }, [loadAll])

  const configOptions = useMemo(
    () => [{ value: '', label: 'Не выбран' }, ...configs.map((c) => ({ value: c.id, label: c.name }))],
    [configs],
  )
  const serverOptions = useMemo(
    () => availableServers.map((s) => ({ value: s, label: s })),
    [availableServers],
  )

  return (
    <PageLayout className="nekkus-glass-root">
      <div className="net">
        <AppShell
          logo="Nekkus"
          title="Net"
          description="VPN, конфиги и подписки."
          meta={
            <div className="net__status">
              <StatusDot
                status={status?.connected ? 'online' : 'offline'}
                label={status?.connected ? 'Подключено' : 'Отключено'}
                pulse={!!status?.connected}
              />
              <div className="net__status-meta">
                <span>Сервер: {status?.server || '—'}</span>
                <span>Конфиг: {activeConfig?.name || '—'}</span>
              </div>
            </div>
          }
        >
          {errorMessage ? (
          <div className="net__error" role="alert">
            {errorMessage}
          </div>
        ) : null}

        <Section title="Скорость и трафик">
          <Card className="net__card net__card--metrics nekkus-glass-card" accentTop={!!status?.connected}>
            <div className="net__metrics">
              <div className="net__metric">
                <span className="net__metric-label">↓ Скачивание</span>
                <DataText size="metric">{formatSpeed(status?.downloadSpeed ?? 0)}</DataText>
              </div>
              <div className="net__metric">
                <span className="net__metric-label">↑ Отдача</span>
                <DataText size="metric">{formatSpeed(status?.uploadSpeed ?? 0)}</DataText>
              </div>
              <div className="net__metric">
                <span className="net__metric-label">Всего ↓</span>
                <DataText size="base">{formatBytes(status?.totalDownload ?? 0)}</DataText>
              </div>
              <div className="net__metric">
                <span className="net__metric-label">Всего ↑</span>
                <DataText size="base">{formatBytes(status?.totalUpload ?? 0)}</DataText>
              </div>
            </div>
          </Card>
        </Section>

        <Section title="Зависимости">
          <Card className="net__card nekkus-glass-card">
          <div className="net__row">
            <div className="net__field net__field--stretch">
              <span className="net__field-label">sing-box</span>
              <span className="net__meta">
                {singBoxStatus?.installed
                  ? `OK${singBoxStatus.path ? `: ${singBoxStatus.path}` : ''}`
                  : 'Не установлен'}
              </span>
            </div>
            {!singBoxStatus?.installed ? (
              <Button variant="primary" onClick={handleInstallSingBox} disabled={isBusy}>
                Установить
              </Button>
            ) : null}
          </div>
          </Card>
        </Section>

        <Section title="Управление подключением">
          <Card className="net__card nekkus-glass-card">
          <div className="net__row">
            <Select
              label="Конфиг"
              options={configOptions}
              value={connectConfigId}
              onChange={(e: React.ChangeEvent<HTMLSelectElement>) => setConnectConfigId(e.target.value)}
              disabled={isBusy}
            />
            {availableServers.length > 0 ? (
              <Select
                label="Сервер"
                options={serverOptions}
                value={selectedServer}
                onChange={(e: React.ChangeEvent<HTMLSelectElement>) => setSelectedServer(e.target.value)}
                disabled={isBusy}
              />
            ) : (
              <Input
                label="Сервер"
                type="text"
                value={connectServer}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => setConnectServer(e.target.value)}
                placeholder="auto / custom"
                disabled={isBusy}
              />
            )}
          </div>
          <div className="net__actions">
            <Button variant="primary" onClick={handleConnect} disabled={isBusy}>
              Подключить
            </Button>
            <Button variant="secondary" onClick={handleDisconnect} disabled={isBusy}>
              Отключить
            </Button>
          </div>
          </Card>
        </Section>

        <Section title={`Конфиги (${configs.length})`}>
          <Card className="net__card nekkus-glass-card">
          <div className="net__header-actions">
            <Button variant="ghost" size="sm" onClick={loadAll} disabled={isBusy}>
              Обновить
            </Button>
          </div>
          <div className="net__row">
            <Input
              label="Имя"
              value={configName}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setConfigName(e.target.value)}
              placeholder="MyConfig"
              disabled={isBusy}
            />
            <div className="net__field net__field--stretch">
              <label className="net__field-label">Контент</label>
              <textarea
                className="net__textarea"
                value={configContent}
                onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => setConfigContent(e.target.value)}
                placeholder="raw config"
                rows={4}
                disabled={isBusy}
              />
            </div>
          </div>
          <Button variant="primary" onClick={handleCreateConfig} disabled={isBusy}>
            Добавить конфиг
          </Button>
          <div className="net__list">
            {configs.map((config) => (
              <div key={config.id} className="net__list-item">
                <div>
                  <div className="net__list-title">{config.name}</div>
                  <div className="net__meta">ID: {config.id}</div>
                </div>
                <Pill variant={config.source_url ? 'info' : 'default'}>
                  {config.source_url ? 'Subscription' : 'Manual'}
                </Pill>
              </div>
            ))}
          </div>
          </Card>
        </Section>

        <Section title={`Подписки (${subscriptions.length})`}>
          <Card className="net__card nekkus-glass-card">
          <div className="net__header-actions">
            <Button variant="ghost" size="sm" onClick={handleRefreshSubscriptions} disabled={isBusy}>
              Обновить все
            </Button>
          </div>
          <div className="net__row">
            <Input
              label="Имя"
              value={subscriptionName}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setSubscriptionName(e.target.value)}
              placeholder="MySubscription"
              disabled={isBusy}
            />
            <Input
              label="URL"
              type="url"
              value={subscriptionUrl}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setSubscriptionUrl(e.target.value)}
              placeholder="https://example.com/sub.txt"
              disabled={isBusy}
            />
          </div>
          <Button variant="primary" onClick={handleCreateSubscription} disabled={isBusy}>
            Добавить подписку
          </Button>
          <div className="net__list">
            {subscriptions.map((sub) => (
              <div key={sub.id} className="net__list-item">
                <div>
                  <div className="net__list-title">{sub.name}</div>
                  <div className="net__meta">{sub.url}</div>
                </div>
                <Pill variant={sub.last_error ? 'error' : 'success'}>
                  {sub.last_error ? `Ошибка: ${sub.last_error}` : 'OK'}
                </Pill>
              </div>
            ))}
          </div>
          </Card>
        </Section>

        <Section title="Логи sing-box">
          <Card className="net__card nekkus-glass-card">
          <div className="net__header-actions">
            <Button variant="ghost" size="sm" onClick={() => setLogsVisible((v) => !v)}>
              {logsVisible ? 'Скрыть' : 'Показать'}
            </Button>
          </div>
          {logsVisible ? (
            <div className="net__logs">{logs.length ? logs.join('\n') : 'Нет логов'}</div>
          ) : (
            <div className="net__logs net__logs--hint">
              Логи показываются только в standalone UI
            </div>
          )}
          </Card>
        </Section>
        </AppShell>
      </div>
      {import.meta.env.VITE_BUILD_ID ? (
        <div className="net__build-id" title="Версия сборки">
          Build: {import.meta.env.VITE_BUILD_ID}
        </div>
      ) : null}
    </PageLayout>
  )
}

export default App

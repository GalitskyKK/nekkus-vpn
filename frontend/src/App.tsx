import { useCallback, useEffect, useMemo, useState } from 'react'
import './App.css'
import {
  addConfig,
  addSubscription,
  connectVPN,
  disconnectVPN,
  fetchConfigs,
  fetchStatus,
  fetchSubscriptions,
  refreshSubscriptions,
} from './api'
import type { Subscription, VpnConfig, VpnStatus } from './types'

const statusRefreshMs = 2000

function App() {
  const [status, setStatus] = useState<VpnStatus | null>(null)
  const [configs, setConfigs] = useState<VpnConfig[]>([])
  const [subscriptions, setSubscriptions] = useState<Subscription[]>([])
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [isBusy, setIsBusy] = useState(false)

  const [configName, setConfigName] = useState('')
  const [configContent, setConfigContent] = useState('')
  const [subscriptionName, setSubscriptionName] = useState('')
  const [subscriptionUrl, setSubscriptionUrl] = useState('')
  const [connectConfigId, setConnectConfigId] = useState('')
  const [connectServer, setConnectServer] = useState('')

  const activeConfig = useMemo(
    () => configs.find((config) => config.id === status?.activeConfigId),
    [configs, status?.activeConfigId],
  )

  const loadAll = useCallback(async () => {
    try {
      setErrorMessage(null)
      const [nextStatus, nextConfigs, nextSubscriptions] = await Promise.all([
        fetchStatus(),
        fetchConfigs(),
        fetchSubscriptions(),
      ])
      setStatus(nextStatus)
      setConfigs(nextConfigs)
      setSubscriptions(nextSubscriptions)
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

  const handleCreateConfig = useCallback(async () => {
    if (!configName.trim() || !configContent.trim()) {
      setErrorMessage('Укажи имя и контент конфига')
      return
    }
    try {
      setIsBusy(true)
      setErrorMessage(null)
      const created = await addConfig({
        name: configName.trim(),
        content: configContent.trim(),
      })
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
    if (!connectConfigId && !connectServer.trim()) {
      setErrorMessage('Выбери конфиг или задай сервер')
      return
    }
    try {
      setIsBusy(true)
      setErrorMessage(null)
      const nextStatus = await connectVPN({
        config_id: connectConfigId || undefined,
        server: connectServer.trim() || undefined,
      })
      setStatus(nextStatus)
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : 'Не удалось подключиться')
    } finally {
      setIsBusy(false)
    }
  }, [connectConfigId, connectServer])

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

  return (
    <div className="app">
      <header className="app__header">
        <div>
          <p className="app__eyebrow">nekkus VPN</p>
          <h1 className="app__title">Standalone панель</h1>
        </div>
        <div className="app__status">
          <div className={`status-pill ${status?.connected ? 'status-pill--on' : 'status-pill--off'}`}>
            {status?.connected ? 'Подключено' : 'Отключено'}
          </div>
          <div className="status-meta">
            <span>Сервер: {status?.server || '—'}</span>
            <span>Конфиг: {activeConfig?.name || '—'}</span>
          </div>
        </div>
      </header>

      {errorMessage ? <div className="app__error">{errorMessage}</div> : null}

      <section className="panel">
        <h2>Управление подключением</h2>
        <div className="panel__row">
          <label className="field">
            <span>Конфиг</span>
            <select
              value={connectConfigId}
              onChange={(event) => setConnectConfigId(event.target.value)}
              disabled={isBusy}
            >
              <option value="">Не выбран</option>
              {configs.map((config) => (
                <option key={config.id} value={config.id}>
                  {config.name}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Сервер</span>
            <input
              type="text"
              value={connectServer}
              onChange={(event) => setConnectServer(event.target.value)}
              placeholder="auto / custom"
              disabled={isBusy}
            />
          </label>
        </div>
        <div className="panel__actions">
          <button className="btn btn--primary" onClick={handleConnect} disabled={isBusy}>
            Подключить
          </button>
          <button className="btn" onClick={handleDisconnect} disabled={isBusy}>
            Отключить
          </button>
        </div>
      </section>

      <section className="panel">
        <div className="panel__header">
          <h2>Конфиги ({configs.length})</h2>
          <button className="btn btn--ghost" onClick={loadAll} disabled={isBusy}>
            Обновить
          </button>
        </div>
        <div className="panel__row">
          <label className="field">
            <span>Имя</span>
            <input
              type="text"
              value={configName}
              onChange={(event) => setConfigName(event.target.value)}
              placeholder="MyConfig"
              disabled={isBusy}
            />
          </label>
          <label className="field field--stretch">
            <span>Контент</span>
            <textarea
              value={configContent}
              onChange={(event) => setConfigContent(event.target.value)}
              placeholder="raw config"
              rows={4}
              disabled={isBusy}
            />
          </label>
        </div>
        <button className="btn btn--primary" onClick={handleCreateConfig} disabled={isBusy}>
          Добавить конфиг
        </button>
        <div className="list">
          {configs.map((config) => (
            <div key={config.id} className="list__item">
              <div>
                <div className="list__title">{config.name}</div>
                <div className="list__meta">ID: {config.id}</div>
              </div>
              <div className="list__meta">{config.source_url ? 'Subscription' : 'Manual'}</div>
            </div>
          ))}
        </div>
      </section>

      <section className="panel">
        <div className="panel__header">
          <h2>Подписки ({subscriptions.length})</h2>
          <button className="btn btn--ghost" onClick={handleRefreshSubscriptions} disabled={isBusy}>
            Обновить все
          </button>
        </div>
        <div className="panel__row">
          <label className="field">
            <span>Имя</span>
            <input
              type="text"
              value={subscriptionName}
              onChange={(event) => setSubscriptionName(event.target.value)}
              placeholder="MySubscription"
              disabled={isBusy}
            />
          </label>
          <label className="field field--stretch">
            <span>URL</span>
            <input
              type="url"
              value={subscriptionUrl}
              onChange={(event) => setSubscriptionUrl(event.target.value)}
              placeholder="https://example.com/sub.txt"
              disabled={isBusy}
            />
          </label>
        </div>
        <button className="btn btn--primary" onClick={handleCreateSubscription} disabled={isBusy}>
          Добавить подписку
        </button>
        <div className="list">
          {subscriptions.map((subscription) => (
            <div key={subscription.id} className="list__item">
              <div>
                <div className="list__title">{subscription.name}</div>
                <div className="list__meta">{subscription.url}</div>
              </div>
              <div className="list__meta">
                {subscription.last_error ? `Ошибка: ${subscription.last_error}` : 'OK'}
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  )
}

export default App

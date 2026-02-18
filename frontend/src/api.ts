import type {
  ConnectPayload,
  CreateConfigPayload,
  CreateSubscriptionPayload,
  SingBoxStatus,
  Subscription,
  VpnConfig,
  VpnSettings,
  VpnStatus,
} from './types'

// Один entry — cmd (порт 9001). UI открывай с http://localhost:9001 (same-origin).
// Для vite dev с другого порта: VITE_API_BASE=http://127.0.0.1:9001
const apiBase =
  import.meta.env.VITE_API_BASE ??
  (typeof window !== 'undefined' ? '' : 'http://127.0.0.1:9001')

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${apiBase}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
  })

  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `Request failed: ${response.status}`)
  }

  return response.json() as Promise<T>
}

export const fetchStatus = () => request<VpnStatus>('/api/status')
export const fetchConfigs = () => request<VpnConfig[]>('/api/configs')
export const fetchSubscriptions = () => request<Subscription[]>('/api/subscriptions')
export const fetchSettings = () => request<VpnSettings>('/api/settings')
export const updateSettings = (patch: VpnSettings) =>
  request<VpnSettings>('/api/settings', {
    method: 'POST',
    body: JSON.stringify(patch),
  })

export const fetchSingBoxStatus = () => request<SingBoxStatus>('/api/deps/singbox')
export const installSingBox = () =>
  request<SingBoxStatus>('/api/deps/singbox/install', {
    method: 'POST',
  })

export const addConfig = (payload: CreateConfigPayload) =>
  request<VpnConfig>('/api/configs', {
    method: 'POST',
    body: JSON.stringify(payload),
  })

export const addSubscription = (payload: CreateSubscriptionPayload) =>
  request<Subscription>('/api/subscriptions', {
    method: 'POST',
    body: JSON.stringify(payload),
  })

export const refreshSubscriptions = () =>
  request<Array<{ id: string; status: string }>>('/api/subscriptions/refresh', {
    method: 'POST',
  })

export const connectVPN = (payload: ConnectPayload) =>
  request<VpnStatus>('/api/connect', {
    method: 'POST',
    body: JSON.stringify(payload),
  })

export const disconnectVPN = () =>
  request<VpnStatus>('/api/disconnect', {
    method: 'POST',
  })

const serverItem = (s: { id?: string; name?: string }) => s?.name ?? s?.id ?? ''

export const fetchServers = (configId: string) =>
  request<Array<{ id?: string; name?: string }>>(
    configId ? `/api/servers?config_id=${encodeURIComponent(configId)}` : '/api/servers',
  ).then((raw) => (Array.isArray(raw) ? raw.map(serverItem) : []))

export const fetchLogs = () => request<string[]>('/api/logs')

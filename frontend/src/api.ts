import type {
  ConnectPayload,
  CreateConfigPayload,
  CreateSubscriptionPayload,
  Subscription,
  VpnConfig,
  VpnSettings,
  VpnStatus,
} from './types'

const apiBase = import.meta.env.VITE_API_BASE ?? 'http://127.0.0.1:8081'

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

export const fetchStatus = () => request<VpnStatus>('/status')
export const fetchConfigs = () => request<VpnConfig[]>('/configs')
export const fetchSubscriptions = () => request<Subscription[]>('/subscriptions')
export const fetchSettings = () => request<VpnSettings>('/settings')

export const addConfig = (payload: CreateConfigPayload) =>
  request<VpnConfig>('/configs', {
    method: 'POST',
    body: JSON.stringify(payload),
  })

export const addSubscription = (payload: CreateSubscriptionPayload) =>
  request<Subscription>('/subscriptions', {
    method: 'POST',
    body: JSON.stringify(payload),
  })

export const refreshSubscriptions = () =>
  request<Array<{ id: string; status: string }>>('/subscriptions/refresh', {
    method: 'POST',
  })

export const connectVPN = (payload: ConnectPayload) =>
  request<VpnStatus>('/connect', {
    method: 'POST',
    body: JSON.stringify(payload),
  })

export const disconnectVPN = () =>
  request<VpnStatus>('/disconnect', {
    method: 'POST',
  })

export const fetchServers = (configId: string) =>
  request<string[]>(`/servers?config_id=${encodeURIComponent(configId)}`)

export const fetchLogs = () => request<string[]>('/logs')

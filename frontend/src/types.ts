export type VpnStatus = {
  connected: boolean
  server: string
  activeConfigId: string
  configCount: number
  downloadSpeed: number
  uploadSpeed: number
  totalDownload: number
  totalUpload: number
  lastUpdateUnix: number
}

export type VpnConfig = {
  id: string
  name: string
  content: string
  source_url?: string
  subscription_id?: string
  created_at: number
  updated_at: number
}

export type Subscription = {
  id: string
  name: string
  url: string
  config_id: string
  created_at: number
  updated_at: number
  last_error?: string
  last_success?: number
}

export type CreateConfigPayload = {
  name: string
  content: string
  source_url?: string
}

export type CreateSubscriptionPayload = {
  name: string
  url: string
}

export type ConnectPayload = {
  config_id?: string
  server?: string
}

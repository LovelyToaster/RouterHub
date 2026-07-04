export interface Provider {
  id: string
  name: string
  type: ProviderType
  base_url: string
  api_key: string
  enabled: boolean
  model_prefix: string
  hide_original_models: boolean
  created_at: string
  updated_at: string
}

export type ProviderType =
  | 'openai-chat-completions'
  | 'openai-responses'
  | 'anthropic-messages'

export interface ModelAlias {
  id: string
  alias: string
  provider_id: string
  target_model: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface GatewayAPIKey {
  id: string
  name: string
  api_key: string
  enabled: boolean
  created_at: string
  updated_at: string
  last_used_at?: string
}

export interface AppSetting {
  key: string
  value: string
  updated_at: string
}

export interface RequestLog {
  id: number
  request_id: string
  provider_name: string
  provider_type: string
  requested_model: string
  actual_model: string
  stream: boolean
  status: string
  error_message?: string
  created_at: string
  finished_at?: string
  time_to_first_token_ms?: number
  total_duration_ms?: number
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  cache_write_tokens: number
  total_tokens: number
  client_ip: string
  gateway_api_key_name: string
}

export interface WindowStats {
  requests: number
  successful_requests: number
  failed_requests: number
  tokens: number
  input_tokens: number
  output_tokens: number
  cached_tokens: number
  avg_duration_ms: number
  avg_ttft_ms: number
  start?: string
  end?: string
}

export type RangeKey = 'all' | 'month' | 'week' | 'day'

export interface StatsSummary {
  range: RangeKey
  bucket_kind: 'hour' | 'day' | 'week' | 'month'
  timezone: string
  has_previous_window: boolean
  active_days: number
  current: WindowStats
  previous: WindowStats
  requests_by_provider: Record<string, number>
  requests_by_model: Record<string, number>
  tokens_by_provider: Record<string, number>
  tokens_by_model: Record<string, number>
  series: { date: string; count: number }[]
  token_series: { date: string; count: number }[]
  model_performance: {
    model: string
    tokens_per_second: number
    avg_ttft_ms: number
    sample_count: number
  }[]
  provider_performance: {
    model: string
    tokens_per_second: number
    avg_ttft_ms: number
    sample_count: number
  }[]
}

export interface AdminMe {
  id: string
  username: string
  timezone: string
}

export interface ProviderModel {
  id: string
  provider_id: string
  model_name: string
  display_name?: string
  enabled: boolean
  source: 'manual' | 'upstream'
  created_at: string
  updated_at: string
}

export interface Appearance {
  language: 'zh-CN' | 'en-US'
  theme: 'light' | 'dark' | 'system'
}

export interface LoginResponse {
  token: string
  expires_at: string
}

export interface SetupStatusResponse {
  initialized: boolean
}

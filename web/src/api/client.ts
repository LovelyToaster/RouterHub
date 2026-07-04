const TOKEN_KEY = 'routerhub_admin_token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function request<T>(
  url: string,
  options: RequestInit = {},
): Promise<T> {
  const token = getToken()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const res = await fetch(url, { ...options, headers })

  if (!res.ok) {
    if (res.status === 401) {
      clearToken()
      // Redirect to login if not already there
      if (window.location.pathname !== '/login' && window.location.pathname !== '/setup') {
        window.location.href = '/login'
      }
    }
    const body = await res.json().catch(() => ({}))
    throw new ApiError(res.status, body.error || res.statusText)
  }

  // Handle 204 No Content
  if (res.status === 204) {
    return undefined as T
  }

  return res.json()
}

// --- Setup ---
export function getSetupStatus(): Promise<{ initialized: boolean }> {
  return request('/api/setup/status')
}

export function initSetup(username: string, password: string): Promise<{ message: string }> {
  return request('/api/setup/init', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

// --- Auth ---
export function login(
  username: string,
  password: string,
): Promise<{ token: string; expires_at: string }> {
  return request('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export function logout(): Promise<{ message: string }> {
  return request('/api/auth/logout', { method: 'POST' })
}

export function getMe(): Promise<import('@/types').AdminMe> {
  return request('/api/auth/me')
}

export function getSystemInfo(): Promise<{
  app_version: string
  go_version: string
  platform: string
  build_date: string
}> {
  return request('/api/system/info')
}

export function updateMe(
  data: { timezone?: string },
): Promise<import('@/types').AdminMe> {
  return request('/api/auth/me', {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

// --- Providers ---
export function listProviders(): Promise<import('@/types').Provider[]> {
  return request('/api/providers')
}

export function getProvider(id: string): Promise<import('@/types').Provider> {
  return request(`/api/providers/${id}`)
}

export function createProvider(
  data: Omit<import('@/types').Provider, 'id' | 'created_at' | 'updated_at'>,
): Promise<import('@/types').Provider> {
  return request('/api/providers', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export function updateProvider(
  id: string,
  data: Partial<import('@/types').Provider>,
): Promise<import('@/types').Provider> {
  return request(`/api/providers/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export function deleteProvider(id: string): Promise<{ message: string }> {
  return request(`/api/providers/${id}`, { method: 'DELETE' })
}

// --- Model Aliases ---
export function listModelAliases(): Promise<import('@/types').ModelAlias[]> {
  return request('/api/model-aliases')
}

export function createModelAlias(
  data: Omit<import('@/types').ModelAlias, 'id' | 'created_at' | 'updated_at'>,
): Promise<import('@/types').ModelAlias> {
  return request('/api/model-aliases', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export function updateModelAlias(
  id: string,
  data: Partial<import('@/types').ModelAlias>,
): Promise<import('@/types').ModelAlias> {
  return request(`/api/model-aliases/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export function deleteModelAlias(id: string): Promise<{ message: string }> {
  return request(`/api/model-aliases/${id}`, { method: 'DELETE' })
}

// --- Gateway API Keys ---
export function listGatewayKeys(): Promise<import('@/types').GatewayAPIKey[]> {
  return request('/api/gateway-keys')
}

export function createGatewayKey(data: {
  name: string
  enabled?: boolean
}): Promise<import('@/types').GatewayAPIKey> {
  return request('/api/gateway-keys', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export function updateGatewayKey(
  id: string,
  data: { name?: string; enabled?: boolean },
): Promise<import('@/types').GatewayAPIKey> {
  return request(`/api/gateway-keys/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export function deleteGatewayKey(id: string): Promise<{ message: string }> {
  return request(`/api/gateway-keys/${id}`, { method: 'DELETE' })
}

// --- Settings ---
export function getSettings(): Promise<import('@/types').AppSetting[]> {
  return request('/api/settings')
}

export function updateSettings(
  data: Record<string, string>,
): Promise<import('@/types').AppSetting[]> {
  return request('/api/settings', {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

// --- Stats ---
export function getStatsSummary(
  range: import('@/types').RangeKey = 'all',
): Promise<import('@/types').StatsSummary> {
  return request(`/api/stats/summary?range=${encodeURIComponent(range)}`)
}

// --- Provider Models ---
export function listProviderModels(
  providerId: string,
): Promise<import('@/types').ProviderModel[]> {
  return request(`/api/providers/${providerId}/models`)
}

export function createProviderModel(
  providerId: string,
  data: { model_name: string },
): Promise<import('@/types').ProviderModel> {
  return request(`/api/providers/${providerId}/models`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export function updateProviderModel(
  providerId: string,
  modelId: string,
  data: Partial<import('@/types').ProviderModel>,
): Promise<import('@/types').ProviderModel> {
  return request(`/api/providers/${providerId}/models/${modelId}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export function deleteProviderModel(
  providerId: string,
  modelId: string,
): Promise<{ message: string }> {
  return request(`/api/providers/${providerId}/models/${modelId}`, {
    method: 'DELETE',
  })
}

export function importProviderModels(
  providerId: string,
  data: { models: { model_name: string; display_name?: string; enabled?: boolean }[] },
): Promise<import('@/types').ProviderModel[]> {
  return request(`/api/providers/${providerId}/models/import`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export function fetchProviderModels(
  providerId: string,
): Promise<string[]> {
  return request(`/api/providers/${providerId}/models/fetch`, {
    method: 'POST',
  })
}

// --- Provider Model Aliases (scoped to a provider) ---
export function listProviderModelAliases(
  providerId: string,
): Promise<import('@/types').ModelAlias[]> {
  return request(`/api/providers/${providerId}/model-aliases`)
}

// --- Provider Models Preview (for creating new providers) ---
export function fetchProviderModelsPreview(
  data: { type: string; base_url: string; api_key: string },
): Promise<string[]> {
  return request('/api/providers/models/preview', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

// --- Request Logs ---
export function listRequestLogs(
  limit = 50,
  offset = 0,
): Promise<import('@/types').RequestLog[]> {
  return request(`/api/request-logs?limit=${limit}&offset=${offset}`)
}

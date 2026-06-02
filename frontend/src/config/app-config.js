let config = null

export async function loadConfig() {
  const configPath = resolvePublicPath('app.json')
  const res = await fetch(configPath)
  if (!res.ok) throw new Error(`${configPath} 返回 ${res.status}`)
  config = normalizeConfig(await res.json())
}

export function getConfig() {
  if (!config) throw new Error('配置未加载')
  return config
}

function resolvePublicPath(path) {
  const basePath = import.meta.env.BASE_URL.endsWith('/') ? import.meta.env.BASE_URL : import.meta.env.BASE_URL + '/'
  return new URL(path, new URL(basePath, window.location.origin)).toString()
}

function resolveBase(value) {
  if (!value) return ''
  try {
    return new URL(value, window.location.origin).toString().replace(/\/$/, '')
  } catch {
    return value
  }
}

function normalizeConfig(raw) {
  return {
    apiBase: resolveBase(raw.api_base ?? raw.apiBase ?? ''),
    staticBase: resolveBase(raw.static_base ?? raw.staticBase ?? ''),
    requestTimeoutMs: raw.request_timeout_ms ?? raw.requestTimeoutMs ?? 15000,
    pollIntervalMs: raw.poll_interval_ms ?? raw.pollIntervalMs ?? 3000,
  }
}

let config = null

export async function loadConfig() {
  const configPath = new URL('app.json', window.location.origin + import.meta.env.BASE_URL).pathname
  const res = await fetch(configPath)
  if (!res.ok) throw new Error(`${configPath} 返回 ${res.status}`)
  config = normalizeConfig(await res.json())
}

export function getConfig() {
  if (!config) throw new Error('配置未加载')
  return config
}

function normalizeConfig(raw) {
  return {
    apiBase: raw.api_base ?? raw.apiBase ?? '',
    staticBase: raw.static_base ?? raw.staticBase ?? '',
    pollIntervalMs: raw.poll_interval_ms ?? raw.pollIntervalMs ?? 3000,
  }
}

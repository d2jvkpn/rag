let config = null

export async function loadConfig() {
  const res = await fetch('/app.json')
  if (!res.ok) throw new Error(`/app.json 返回 ${res.status}`)
  config = await res.json()
}

export function getConfig() {
  if (!config) throw new Error('配置未加载')
  return config
}

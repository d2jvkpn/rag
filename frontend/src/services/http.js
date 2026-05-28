import { getConfig } from '../config/app-config.js'

export class HttpError extends Error {
  constructor(status, code, message, details) {
    super(message)
    this.status = status
    this.code = code
    this.details = details || []
  }
}

async function request(method, path, options = {}) {
  const { apiBase, requestTimeoutMs } = getConfig()
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), requestTimeoutMs)
  const init = { method, credentials: 'include', headers: {}, signal: controller.signal }

  if (options.json !== undefined) {
    init.headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(options.json)
  } else if (options.body !== undefined) {
    init.body = options.body
  }

  let res
  try {
    res = await fetch(apiBase + path, init)
  } catch (err) {
    if (err.name === 'AbortError') {
      throw new HttpError(0, 'timeout', 'Request timed out after ' + requestTimeoutMs + 'ms')
    }
    throw err
  } finally {
    clearTimeout(timeout)
  }

  if (res.status === 204) return null

  const data = await res.json()

  if (!res.ok) {
    const e = data.error || {}
    throw new HttpError(res.status, e.code || 'unknown_error', e.message || 'Request failed', e.details)
  }

  return data.data
}

export const http = {
  get: (path) => request('GET', path),
  post: (path, json) => request('POST', path, { json }),
  put: (path, json) => request('PUT', path, { json }),
  postForm: (path, body) => request('POST', path, { body }),
  delete: (path) => request('DELETE', path),
}

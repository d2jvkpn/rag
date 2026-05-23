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
  const { apiBase } = getConfig()
  const init = { method, credentials: 'include', headers: {} }

  if (options.json !== undefined) {
    init.headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(options.json)
  } else if (options.body !== undefined) {
    init.body = options.body
  }

  const res = await fetch(apiBase + path, init)

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

export const SHELL_CONNECT_INFO = '/api/auth/shellConnectInfo'

export function createShellDialer({ fetch: fetchInfo, location, dial, authenticate, resolveWsUrl }) {
  return async () => {
    const response = await fetchInfo(SHELL_CONNECT_INFO, { cache: 'no-store' })
    if (!response.ok) throw new Error('credential-unavailable')
    const info = await response.json()
    return dial({
      servers: resolveWsUrl(info.wsUrl, location),
      authenticator: authenticate(info.jwt, new TextEncoder().encode(info.nkeySeed)),
      name: 'lab-shell',
      maxReconnectAttempts: -1,
    })
  }
}

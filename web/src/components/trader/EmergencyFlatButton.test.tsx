// m443/N6 — Emergency Flat shows GREEN only for HTTP 2xx + triggered:true.
// Every other shape is red "NOT SENT: <reason>". Driven through the real
// component (click -> Confirm -> fetch), not just the helper.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { EmergencyFlatButton } from './EmergencyFlatButton'

function reply(status: number, body: unknown, rawText?: string) {
  const resp = {
    ok: status >= 200 && status < 300,
    status,
    json: async () => {
      if (rawText !== undefined) throw new SyntaxError('Unexpected token <')
      return body
    },
  }
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(resp))
}

async function confirm() {
  render(<EmergencyFlatButton traderId="t1" />)
  fireEvent.click(screen.getByTestId('emergency-flat-button'))
  fireEvent.click(screen.getByTestId('confirm-flat-button'))
  return await screen.findByTestId('emergency-flat-result')
}

beforeEach(() => vi.restoreAllMocks())
afterEach(() => vi.unstubAllGlobals())

describe('EmergencyFlatButton result truth', () => {
  it('2xx + triggered:true is green and shows the server JSON', async () => {
    reply(200, { triggered: true, trader_id: 't1', positions_flattened: 1 })
    const el = await confirm()
    expect(el.getAttribute('data-sent')).toBe('true')
    expect(el.className).toContain('text-green-300')
    expect(el.className).not.toContain('text-red-400')
    expect(el.textContent).toContain('"triggered": true')
    expect(el.textContent).not.toContain('NOT SENT')
  })
  it('HTTP 200 + triggered:false (NT8 down) is RED with the server reason', async () => {
    reply(200, {
      triggered: false,
      reason: 'flatten command failed: NT8 not connected',
    })
    const el = await confirm()
    expect(el.getAttribute('data-sent')).toBe('false')
    expect(el.className).toContain('text-red-400')
    expect(el.className).not.toContain('text-green-300')
    expect(el.textContent).toBe(
      'NOT SENT: flatten command failed: NT8 not connected'
    )
  })
  it('HTTP 200 + triggered:false with no reason still says NOT SENT', async () => {
    reply(200, { triggered: false })
    const el = await confirm()
    expect(el.textContent).toMatch(/^NOT SENT: /)
    expect(el.getAttribute('data-sent')).toBe('false')
  })
  it('HTTP 401 (expired token) is RED and names the login', async () => {
    reply(401, { error: 'invalid token' })
    const el = await confirm()
    expect(el.getAttribute('data-sent')).toBe('false')
    expect(el.textContent).toContain('NOT SENT: HTTP 401')
    expect(el.textContent).toContain('log in again')
  })
  it('HTTP 500 is RED', async () => {
    reply(500, { error: 'boom' })
    const el = await confirm()
    expect(el.textContent).toBe('NOT SENT: HTTP 500 — boom')
  })
  it('a 2xx that carries triggered:true but non-ok status never happens; non-ok + triggered:true is still RED', async () => {
    reply(502, { triggered: true })
    const el = await confirm()
    expect(el.getAttribute('data-sent')).toBe('false')
    expect(el.textContent).toContain('NOT SENT: HTTP 502')
  })
  it('non-JSON body (proxy error page) is RED', async () => {
    reply(502, null, '<html>Bad Gateway</html>')
    const el = await confirm()
    expect(el.getAttribute('data-sent')).toBe('false')
    expect(el.textContent).toContain('NOT SENT: HTTP 502')
  })
  it('network error is RED', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockRejectedValue(new TypeError('Failed to fetch'))
    )
    render(<EmergencyFlatButton traderId="t1" />)
    fireEvent.click(screen.getByTestId('emergency-flat-button'))
    fireEvent.click(screen.getByTestId('confirm-flat-button'))
    const el = await screen.findByTestId('emergency-flat-result')
    expect(el.getAttribute('data-sent')).toBe('false')
    expect(el.textContent).toBe('NOT SENT: network error — Failed to fetch')
    await waitFor(() =>
      expect(screen.getByTestId('confirm-flat-button')).not.toBeDisabled()
    )
  })
})

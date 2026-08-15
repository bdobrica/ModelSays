import { afterEach, describe, expect, it, vi } from 'vitest'

import { copyText } from './clipboard'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('copyText', () => {
  it('uses the modern clipboard API when available', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    await copyText('https://example.test/join?code=ABC234')
    expect(writeText).toHaveBeenCalledWith('https://example.test/join?code=ABC234')
  })

  it('falls back to execCommand for plain-HTTP LAN contexts', async () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    const execCommand = vi.fn().mockReturnValue(true)
    Object.defineProperty(document, 'execCommand', { configurable: true, value: execCommand })
    await copyText('http://192.168.11.192:5173/join?code=ABC234')
    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(document.querySelector('textarea')).toBeNull()
  })

  it('falls back when the modern API rejects the write', async () => {
    const writeText = vi.fn().mockRejectedValue(new DOMException('Not allowed', 'NotAllowedError'))
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const execCommand = vi.fn().mockReturnValue(true)
    Object.defineProperty(document, 'execCommand', { configurable: true, value: execCommand })
    await copyText('invite')
    expect(writeText).toHaveBeenCalled()
    expect(execCommand).toHaveBeenCalledWith('copy')
  })
})

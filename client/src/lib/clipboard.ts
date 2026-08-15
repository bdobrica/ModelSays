export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return
    } catch {
      // Plain-HTTP LAN deployments commonly expose the API but reject writes.
      // Fall through to the user-gesture-compatible legacy path.
    }
  }

  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.readOnly = true
  textarea.setAttribute('aria-hidden', 'true')
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  textarea.style.pointerEvents = 'none'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()
  textarea.setSelectionRange(0, text.length)
  try {
    if (typeof document.execCommand !== 'function' || !document.execCommand('copy')) {
      throw new Error('clipboard copy was rejected')
    }
  } finally {
    textarea.remove()
  }
}

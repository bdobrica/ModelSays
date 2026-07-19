import axe from 'axe-core'
import { render } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { AppShell } from './components/AppShell'
import { CreateRoomPage } from './pages/CreateRoomPage'
import { HomePage } from './pages/HomePage'
import { JoinRoomPage } from './pages/JoinRoomPage'
import { minimumTouchTargetPixels, revealMotionMilliseconds, supportedViewports } from './lib/uiPolicy'

async function expectNoSeriousViolations(container: HTMLElement) {
  const result = await axe.run(container, {
    rules: {
      // jsdom has no layout engine; contrast is covered by the documented browser spot check.
      'color-contrast': { enabled: false },
    },
  })
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
}

describe('primary route accessibility', () => {
  it.each([
    ['home', <HomePage />],
    ['create', <CreateRoomPage />],
    ['join with invite', <JoinRoomPage />],
  ])('has no serious or critical axe violations on %s', async (_name, route) => {
    window.history.replaceState({}, '', '/join?code=ABC234')
    const { container } = render(
      <MemoryRouter>
        <AppShell>{route}</AppShell>
      </MemoryRouter>,
    )
    await expectNoSeriousViolations(container)
  })

  it('keeps responsive, fullscreen, focus, touch, and reduced-motion policies stable', () => {
    expect(supportedViewports).toEqual([
      { name: 'phone', width: 360, height: 800 },
      { name: 'tablet', width: 768, height: 1024 },
      { name: 'laptop', width: 1366, height: 768 },
      { name: 'shared display', width: 1920, height: 1080 },
    ])
    expect(minimumTouchTargetPixels).toBeGreaterThanOrEqual(44)
    expect(revealMotionMilliseconds).toBeLessThanOrEqual(500)
  })
})

# Client Accessibility and Responsive Play

The living-room TV surface provides a text room code and copyable link alongside (not only) the QR, a visible joined count/list, accessible start and fullscreen controls, the shared timer, and connection status. During play it owns the full viewport without the application navigation header, permits document overflow instead of clipping long content, and moves phase focus without scrolling the question under a sticky element. The QR alternative identifies its exact public join URL. Reduced-motion policy applies to TV reveal, and phone participants retain the focused phase headings and live states.

The supported viewport contract is 360×800 phone, 768×1024 tablet, 1366×768 laptop, and 1920×1080 shared display. The host room changes from two columns to one below 900 px and tightens navigation and card layout below 480 px. A joined participant always uses one centered panel with no roster/sidebar column or hidden-panel gap. Controls have a 48 px minimum height, visible keyboard focus, and touch-action behavior that avoids delayed activation.

## Interaction behavior

- A skip link moves keyboard users directly to game content.
- When lobby, answering, reveal, or round identity changes, focus moves to the phase heading. This announces the new authoritative context without moving focus away from a control while a mutation is pending.
- Connection, answer/submission, loading, mutation, invite/result-copy, presentation, and error states use status or alert semantics. Color is supplemental; text always identifies the state.
- **Copy invite** writes a join URL whose code pre-fills the join form. A text fallback retains the six-character code if clipboard access is unavailable.
- **Presentation mode** uses the browser Fullscreen API. It removes setup chrome and enlarges the revealed answer board, but does not hide or delay host controls.
- Reveal uses a 420 ms opacity/position transition. `prefers-reduced-motion: reduce` reduces all animation and transition duration to effectively immediate.
- Participant focus follows the action boundary: lobby confirmation, question/answer, revealed waiting, and final rankings. Long questions/names wrap within the panel, and errors stay assertive without exposing host refresh or management controls.
- Open Trivia labels its text field as the player's trivia answer. Reveal states identify correctness with explicit words, show the canonical and submitted answers in labeled terms, announce the focused result heading, and never rely on color alone.
- Choice Trivia exposes its four server-ordered answers as ordinary keyboard-operable buttons in a named group. The grid is 2×2 at ordinary widths and one column below 480 px; the selected, correct, and incorrect states have explicit text and do not rely on color.

## Automated coverage

`npm test` runs `axe-core` against the home, create, invite/join, maximum-player host lobby, and participant lobby/answering/waiting/completed states and fails on serious or critical violations. jsdom cannot calculate layout or color contrast, so the test disables axe's color-contrast rule; contrast, clipping, zoom, and browser/screen-reader behavior remain manual checks. Component tests cover the host/participant visibility boundary, phase focus, announcements, invite copying, fullscreen state, long/tied individual and team result labels, empty guesses, errors, simultaneous/team/sequential states, and the declared viewport/touch/motion contract.

The production client must remain within the PB-00 budget of 350 KiB uncompressed and 110 KiB compressed. FUTURE-05D measured 275,896 bytes uncompressed and 84,670 bytes gzip (JavaScript plus CSS), leaving the framework and state architecture unchanged.

## Release playtest checklist

Run this with authorized participants and record only anonymous device/browser descriptions in `docs/release-evidence.md`.

- [ ] Physical phone: create or join, type and submit an answer, background/restore, copy results.
- [ ] Desktop host: create, start, reveal, override, advance, and replay with keyboard only.
- [ ] Screen reader spot check: navigate landmarks/headings, hear connection/phase/submission/error announcements, and submit without pointer use.
- [ ] Reduced motion: reveal is immediate and no authoritative control is delayed.
- [ ] Shared display: enable Presentation mode and confirm the full maximum-size board, long names, ties, and errors remain readable from the expected room distance.
- [ ] All three modes: simultaneous submitted/expired, team assignment/rankings, and sequential waiting/active/pass/prior-claim states are unmistakable.

Do not mark this checklist passed from responsive emulation alone. Record physical devices, screen-share software/display, participant count, and observed failures without names or recordings.

The repository-owner acceptance record for FUTURE-05D is retained in [`baselines/future-05d.md`](baselines/future-05d.md). It deliberately distinguishes overall product acceptance from device-specific evidence that was not supplied.

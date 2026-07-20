# FUTURE-05D Accessibility and Responsive Evidence

FUTURE-05D was accepted on 2026-07-19 after the repository owner ran the actual game and reported that the polished interface looked good. No personal data was recorded. The owner did not provide device, browser, screen-reader, or screen-share details, so this is product acceptance rather than evidence that every physical-device checklist row passed.

The playtest produced two concrete follow-ups rather than a rejection of the completed polish:

1. A joined participant should have a focused play surface instead of seeing host-oriented room configuration and management detail. Implemented by FUTURE-06.
2. A living-room mode should use a non-playing TV host, QR joining, a shared timer, early reveal when every participant answers, a readable reveal pause, and automatic advancement.

Those observations are scheduled separately in `PLAN.md`. The reusable physical phone, keyboard/screen-reader, reduced-motion, and screen-share checklist remains in [`../accessibility.md`](../accessibility.md) for future release evidence.

## Automated evidence

- `make verify` passed Go vet/tests, 21 client tests, and the production build with PostgreSQL integration enabled.
- Primary entry routes and a maximum-player lobby reported no serious or critical axe violations under jsdom-supported rules.
- Component coverage exercises focus, live announcements, copy feedback, fullscreen state, long names, ties, empty guesses, errors, and simultaneous, team, and sequential states.
- Stable policy assertions cover 360×800, 768×1024, 1366×768, and 1920×1080 viewports, a minimum 48 px touch target, and reveal motion no longer than 500 ms.
- Production assets measured 275,896 bytes uncompressed and 84,670 bytes gzip, below the PB-00 limits of 350 KiB and 110 KiB.
- `git diff --check` passed.

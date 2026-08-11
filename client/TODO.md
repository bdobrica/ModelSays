# Model Says — Client TODO

Status note:

- Checked items are implemented in the current codebase.
- Some nearby items are only partially implemented and remain unchecked.
- The controlled simultaneous MVP flow is complete and CI-gated. Unchecked items are known limitations or post-MVP polish, not prerequisites for the current playable scope.
- Current scope changes:
	- the current client uses a single room route that switches between lobby, answer, reveal, and completion states instead of separate screens;
	- live room updates use authenticated SSE invalidations, sequenced authoritative refetches, bounded reconnect/resume, and slower polling recovery; hidden tabs reduce work;
	- expiry copy reflects the PostgreSQL backend's durable automatic reveal; early reveal and next-round advancement remain host controls;
	- same-browser refresh validates the stored token with the server; presence and cross-device transfer remain deferred;
	- loading and error handling still exist as inline page states, not shared reusable components;
	- the create-room flow separates rules from pacing and exposes playable Open and Choice Trivia; shared-TV trivia presentation remains scheduled separately.
	- the creation form and backend now share the MVP bounds of 1–5 rounds, a 15–120 second timer, and bounded names.
	- CI runs the locked client test suite and production build alongside backend and PostgreSQL lifecycle gates.
	- the root baseline command records production bundle size and representative polling volume; browser rendering and physical-device performance remain playtest work.
	- joined participants now use a role/phase-focused surface; the host-only operational room view remains unchanged;
	- living-room creators now receive a non-playing TV lobby/QR, shared question/timer, persisted reveal-pause, and automatic final-ranking surface.

## Phase 0 — Project Setup

- [x] Create React + TypeScript + Vite app.
- [x] Add router.
- [x] Add API client wrapper.
- [x] Add authenticated SSE client wrapper with polling fallback.
- [x] Add environment variable handling.
- [ ] Add linting.
- [ ] Add formatting.
- [x] Add test setup.
- [x] Add basic app shell.
- [x] Add reproducible dependency bootstrap and build/test targets.
- [ ] Add loading and error components.

## Phase 1 — Design Foundation

- [x] Add app layout.
- [ ] Add button component.
- [ ] Add input component.
- [ ] Add card component.
- [ ] Add modal/dialog component.
- [ ] Add toast/notification component.
- [x] Add countdown timer to the room flow.
- [ ] Add scoreboard row component.
- [ ] Add answer board component.
- [ ] Add player badge component.
- [ ] Add team badge component.

## Phase 2 — Home and Room Creation

- [x] Build home screen.
- [x] Add app explanation.
- [x] Add create-room CTA.
- [x] Add join-room CTA.
- [x] Build create-room screen.
- [x] Add host display-name input.
- [x] Add game mode selector for individual, team, and sequential play.
- [x] Select and transmit Model Says, Open Trivia, or Choice Trivia independently from pacing.
- [x] Render four server-ordered Choice Trivia buttons, submit one opaque option ID, and reveal textual choice results responsively.
- [x] Type the phase-safe frozen trivia content and replay contracts without exposing unfinished controls.
- [x] Add round count selector.
- [x] Add answer timer selector.
- [x] Add locale selector.
- [x] Add model selector.
- [x] Add team-building safe mode toggle.
- [x] Call create-room API.
- [x] Store player reconnect token.
- [x] Navigate to lobby.

## Phase 3 — Join Flow

- [x] Build join-room screen.
- [ ] Validate room code.
- [x] Apply server-compatible display-name length bounds.
- [x] Call join-room API.
- [x] Store room code locally.
- [x] Store reconnect token locally.
- [x] Navigate to lobby.
- [x] Handle invalid room.
- [x] Handle duplicate or rejected names.

## Phase 4 — Lobby

- [x] Build lobby screen.
- [x] Show room code.
- [ ] Add copy invite link button.
- [x] Show player list.
- [x] Show host badge.
- [ ] Show team assignments if enabled.
- [x] Show settings summary.
- [x] Connect to the SSE invalidation stream.
- [x] Update lobby on player join/leave.
- [ ] Add ready toggle if backend supports it.
- [x] Show start button for host only.
- [x] Call start-game API.
- [x] Navigate to game screen when game starts.

## Phase 5 — SSE Invalidation State

- [x] Create SSE connection/reconnect manager.
- [x] Authenticate the stream with the room/player token header.
- [x] Subscribe to room events.
- [x] Recover and validate the stored session during same-browser refresh.
- [x] Refetch room state after polling/mutations.
- [x] Coalesce every `room_invalidation` type into an authoritative room refetch.
- [x] Resume by revision and detect duplicate, out-of-order, and gapped events.
- [x] Fall back to polling and show a non-blocking connection indicator.

## Phase 6 — Round Play Screen

- [x] Build round question screen.
- [x] Show round number.
- [x] Show question text.
- [ ] Show model name if known mode is enabled.
- [x] Show countdown timer.
- [x] Build answer input.
- [x] Submit answer on button click.
- [x] Submit answer on Enter.
- [x] Disable input after submit.
- [x] Show submitted confirmation.
- [x] Show player submission progress.
- [x] Keep board, guess, match, and current-round score data hidden until reveal (enforced by the API projection).
- [x] Handle timer expiry locally while preserving server authority.
- [x] Refetch and enter reveal state after the durable automatic transition event.
- [x] Prevent duplicate local submissions.
- [x] Handle backend rejection gracefully.

## Phase 7 — Reveal Screen

- [x] Build answer board reveal screen.
- [x] Show canonical answers.
- [x] Show point values.
- [ ] Animate answer reveals.
- [x] Show which players matched each answer in completed-game replay.
- [x] Show misses.
- [ ] Show score deltas.
- [x] Show board hash.
- [x] Show “board generated before answers” copy.
- [x] Show next-round button for host.
- [x] Handle transition to next round.
- [x] Handle final round transition to scoreboard.

## Phase 8 — Host Review and Overrides

- [x] Support post-reveal correction for deterministic matches and misses.
- [x] Show confidence bands for semantic suggestions.
- [x] Show raw player answer.
- [x] Show suggested match.
- [x] Show judge confidence and rationale category after reveal to the host only.
- [x] Let host accept suggested match.
- [x] Let host override to another board answer.
- [x] Let host mark as miss.
- [x] Call override API.
- [x] Update scores after override.
- [x] Make this usable during reveal.

## Phase 9 — Scoreboard

- [x] Build live scoreboard.
- [x] Build final scoreboard.
- [x] Show player rankings.
- [x] Show team rankings if enabled.
- [ ] Show round-by-round point changes.
- [x] Highlight winner and ties.
- [x] Add host play-again button that enters a clean new lobby.
- [ ] Add back-to-lobby button.
- [x] Add accessible share/copy results feedback and a stable replay route.

## Phase 10 — Team Mode

- [x] Add team display in lobby.
- [x] Let host create teams.
- [x] Let host assign players to teams.
- [x] Show team scores.
- [x] Show individual scores alongside team scores.
- [x] Adjust scoreboard views.
- [x] Add team-building copy.

## Phase 11 — Mobile and Zoom Polish (post-MVP)

- [x] Provide a responsive single-column layout below 900 px for the supported room flow.
- [ ] Validate the join flow on physical mobile devices.
- [ ] Validate answer input on physical mobile devices.
- [ ] Validate the host board on a real screen share.
- [x] Increase reveal-screen text size in fullscreen presentation mode.
- [x] Add fullscreen-friendly layout without hiding host controls.
- [x] Honor `prefers-reduced-motion`.
- [x] Add clear submitted, expired, reconnecting, and host-review text states.
- [x] Add a 420 ms reveal animation that never delays controls.

## Phase 12 — Error Handling

- [x] Expose backend/unavailable refresh errors through an assertive alert.
- [x] Handle room not found.
- [x] Handle game already started.
- [x] Show offline/reconnecting text and preserve polling recovery.
- [x] Handle answer submission failure.
- [x] Surface the backend error when model generation and curated fallback both fail.
- [x] Handle unauthorized host actions.
- [x] Add fallback route.
- [x] Show loading, no-guesses, unavailable replay, and action-error states.

## Phase 13 — Tests

- [x] Test home screen.
- [x] Test create-room form, ruleset selection, and compatibility behavior.
- [ ] Test join-room form.
- [x] Test lobby rendering and host start.
- [x] Test timer display and expiry.
- [x] Test answer submission state.
- [x] Test reveal board and override controls.
- [x] Test final scoreboard rendering.
- [ ] Test WebSocket event reducer.
- [x] Test session recovery and stale/out-of-order refetch behavior.

## Phase 14 — MVP Acceptance Criteria

- [x] Host can create a room.
- [x] Player can join by room code.
- [x] Lobby updates when players join.
- [x] Host can start the game.
- [x] Players see the same question.
- [x] Players can submit answers.
- [x] Players see submitted state.
- [x] Reveal screen displays model answers.
- [x] Scores are visible.
- [x] Final scoreboard works.
- [x] Basic mobile layout is usable.
- [x] Joined players see only lobby confirmation, their current action/waiting state, and final rankings.

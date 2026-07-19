# Model Says — Client TODO

Status note:

- Checked items are implemented in the current codebase.
- Some nearby items are only partially implemented and remain unchecked.
- Current scope changes:
	- the current client uses a single room route that switches between lobby, answer, reveal, and completion states instead of separate screens;
	- live room updates currently use 3-second polling, not WebSockets;
	- loading and error handling still exist as inline page states, not shared reusable components;
	- the create-room flow currently fixes game mode to simultaneous instead of exposing a mode selector.
	- the creation form and backend now share the MVP bounds of 1–5 rounds, a 15–120 second timer, and bounded names.

## Phase 0 — Project Setup

- [x] Create React + TypeScript + Vite app.
- [x] Add router.
- [x] Add API client wrapper.
- [ ] Add WebSocket client wrapper.
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
- [ ] Add countdown timer component.
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
- [ ] Add game mode selector.
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
- [ ] Connect to WebSocket.
- [x] Update lobby on player join/leave.
- [ ] Add ready toggle if backend supports it.
- [x] Show start button for host only.
- [x] Call start-game API.
- [x] Navigate to game screen when game starts.

## Phase 5 — WebSocket State

- [ ] Create WebSocket connection manager.
- [ ] Authenticate socket with room/player token.
- [ ] Subscribe to room events.
- [ ] Handle reconnect.
- [ ] Refetch room state after reconnect.
- [ ] Handle `room.updated`.
- [ ] Handle `game.started`.
- [ ] Handle `round.started`.
- [ ] Handle `guess.submitted`.
- [ ] Handle `round.revealed`.
- [ ] Handle `score.updated`.
- [ ] Handle `game.ended`.
- [ ] Show user-friendly socket errors.

## Phase 6 — Round Play Screen

- [x] Build round question screen.
- [x] Show round number.
- [x] Show question text.
- [ ] Show model name if known mode is enabled.
- [ ] Show countdown timer.
- [x] Build answer input.
- [x] Submit answer on button click.
- [x] Submit answer on Enter.
- [x] Disable input after submit.
- [x] Show submitted confirmation.
- [x] Show player submission progress.
- [x] Keep board, guess, match, and current-round score data hidden until reveal (enforced by the API projection).
- [ ] Handle timer expiry.
- [x] Prevent duplicate local submissions.
- [x] Handle backend rejection gracefully.

## Phase 7 — Reveal Screen

- [x] Build answer board reveal screen.
- [x] Show canonical answers.
- [x] Show point values.
- [ ] Animate answer reveals.
- [ ] Show which players matched each answer.
- [x] Show misses.
- [ ] Show score deltas.
- [x] Show board hash.
- [x] Show “board generated before answers” copy.
- [x] Show next-round button for host.
- [x] Handle transition to next round.
- [x] Handle final round transition to scoreboard.

## Phase 8 — Host Review and Overrides

- [x] Support post-reveal correction for deterministic matches and misses.
- [ ] Show low-confidence matches.
- [x] Show raw player answer.
- [x] Show suggested match.
- [ ] Show judge confidence.
- [x] Let host accept suggested match.
- [x] Let host override to another board answer.
- [x] Let host mark as miss.
- [x] Call override API.
- [x] Update scores after override.
- [x] Make this usable during reveal.

## Phase 9 — Scoreboard

- [x] Build live scoreboard.
- [x] Build final scoreboard.
- [ ] Show player rankings.
- [ ] Show team rankings if enabled.
- [ ] Show round-by-round point changes.
- [ ] Highlight winner.
- [ ] Add play-again button.
- [ ] Add back-to-lobby button.
- [ ] Add share results button if desired.

## Phase 10 — Team Mode

- [ ] Add team display in lobby.
- [ ] Let host create teams.
- [ ] Let host assign players to teams.
- [ ] Show team scores.
- [ ] Show individual scores within teams.
- [ ] Adjust scoreboard views.
- [ ] Add team-building copy.

## Phase 11 — Mobile and Zoom Polish

- [ ] Make join flow mobile-friendly.
- [ ] Make answer input phone-friendly.
- [ ] Make host board readable on screen share.
- [ ] Increase reveal-screen text size.
- [ ] Add fullscreen-friendly layout.
- [ ] Add reduced-motion option.
- [ ] Add clear “submitted” visual state.
- [ ] Add fun but not slow reveal animations.

## Phase 12 — Error Handling

- [ ] Handle backend unavailable.
- [x] Handle room not found.
- [x] Handle game already started.
- [ ] Handle player disconnected.
- [x] Handle answer submission failure.
- [x] Surface the backend error when model generation and curated fallback both fail.
- [x] Handle unauthorized host actions.
- [x] Add fallback route.
- [ ] Add user-friendly empty states.

## Phase 13 — Tests

- [x] Test home screen.
- [ ] Test create-room form.
- [ ] Test join-room form.
- [ ] Test lobby rendering.
- [ ] Test timer display.
- [ ] Test answer submission state.
- [ ] Test reveal board rendering.
- [ ] Test scoreboard rendering.
- [ ] Test WebSocket event reducer.
- [ ] Test reconnect/refetch behavior.

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

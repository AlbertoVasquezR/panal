---
name: panal-ux
description: Design conventions for the `panal` dashboard (Go, Bubble Tea v1 + Lipgloss v1) — palette, status glyphs, cards, pixel-art mascots, animations, sizes and golden screen tests. Use it before changing anything in internal/ui or internal/mascots, or when designing a new view, key, alert or animation in this repo.
---

# `panal` dashboard UX

For general TUI principles (noise audit, minimum sizes, NO_COLOR, keyboard) also load the `tui-design` skill (`.claude/skills/tui-design`, references `ecosystem-go.md` and `visual-patterns.md`). This skill says **how they apply here**; if they conflict, this one wins.

## What it is and who it's for

A read-only, full-screen dashboard to see *at a glance* what the agents orchestrated by Claude Code (claude, agy, codex, opencode) are doing. The user keeps it open on the side while working: it has to make sense in 2 seconds, without reading. Other devs will use it: nothing may depend on knowing the code.

- Live quota (`internal/live`) does launch the CLIs, but only queries that start no turns and spend no quota (codex app-server → account/rateLimits/read). Never redeem resets or touch credentials.
- **It never writes** to the agents' files. Its only write is its own data directory (quota samples), and only from the interactive UI.
- All I/O goes in a `tea.Cmd`; `View()` only draws what is already in the model.
- UI text in English: clear, short, friendly, sentence case, without internal jargon. Domain words: run, task, quota, credits, log, card, dir, report, forecast, alert.

## Statuses: one shape and one color per status

Status is never color alone: always glyph + word. If you add a status, add it here, in the status icon and status color helpers of the cards code in `internal/ui`, and in the help screen (`?`).

| glyph | color | status |
|---|---|---|
| ● | green | working |
| ● | blue | orchestrating (claude only) |
| ✔ | green | done (it closed its turn; it doesn't mean the work is right) |
| ✖ | red | failed / stuck |
| ◐ | amber | out of quota |
| ⊘ | amber | no permission |
| ○ | dim | idle / no data |
| ⏻ | dim | off (config file) |
| ⚠ | red/amber | broke the task / quota forecast |
| ✦ | (name's color) | unseen: its run ended and you haven't opened its detail |
| ▲ / ▼ | green / red | the user rated the run good / bad (History, panal feedback) |

Don't use a new glyph for something that already has one, and don't reuse one with a different meaning.

Glyphs that Cascadia (Windows Terminal's font) doesn't have, like "↻", are drawn with a fallback font wider than one cell and overlap whatever comes next: always put a space after any glyph (`↻ 02 oct`, not `↻02 oct`).

## Color

Every color comes from the palette file in `internal/ui`; don't write a loose `lipgloss.Color("#…")` anywhere else.

- Per-agent accents: claude `#D97757`, agy `#9575CD`, codex `#69F0AE`, opencode `#FFD54F`, cursor `#38BDF8`. They are the pixel art's colors: if you change one, change the mascot.
- Themes (`-theme`, `ApplyTheme` in `internal/ui`): if you add a dim, border or background color, decide what value it takes in "contrast". Never put a style with a fixed color in a package variable without resetting it in `ApplyTheme`.
- Adaptive semantic colors (`AdaptiveColor` light/dark): green, amber, red, blue, dim, border, empty and brand.
- Text on colored banners or badges: the fixed black "text on color" color, never the palette's "0" nor grey.
- Quotas: green below 60 %, amber from 60 %, red from 85 %.
- Green = good, red = bad, amber = attention, blue = orchestrating, dim = secondary. Don't invent another meaning for those colors.

## Cards and layout

- A single border level: the card. No boxes inside boxes. Normal rounded; thick (`┏━┓`) in the agent's color = selected.
- Order inside the card: banner (NAME + status badge) → mascot → data → quotas. The banner and badge are read first.
- Stable positions: card order doesn't change with status (spatial memory). A disabled agent moves to the "⏻ off" line; it doesn't vanish silently.
- Long paths are trimmed in the middle (the truncate-middle helper in `internal/ui`), text at the end with "…". Always measure with `lipgloss.Width`, never `len`.
- Header: brand + "live" + time; below it, the sentence that sums up what is happening. Footer: 3–6 keys of the current view with `Footer(...)`, `?` always.

### Sizes (floor)

Check every change at 160, 132, 100, 80 and 60 columns:

- Room for 4 cards across → one row of 4; otherwise 2×2; very narrow → one column.
- If it doesn't fit vertically: first the mascots go, then it switches to the compact table.
- The last things to go are: name, status, task.

## Mascots (`internal/mascots`)

- 12×10 pixel sprites with half blocks (`▀`): 12×5 characters. **The size never changes** between frames or modes, so the card doesn't jump.
- Each letter in a row is a color from the sprite's `Palette`; `.` is transparent. A hand-drawn `Base` drawing + `Work` cycle; everything else (blinking, sleeping, trembling…) comes from transforming `Base` (close eyes, shift X/Y, swap colors). Prefer a transformation to drawing new frames.
- Floating effects (the sleeping z, hearts, confetti) are drawn on top and in their own color, never grey.
- Animation **communicates status**: working moves, idle is almost still (blinking), out of quota/failed sleeps in grey, stuck trembles. A reaction to an event (done, failed, delegation) is short (1–3 s) and returns to the status mode.
- Reactions (`Celebrate`, `Scared`, `WakeUp`, `Greet`, `Pet`): started through the UI's react helper (animation code in `internal/ui`), they last `mascots.Duration(mode)` ticks and their frame counts from when they started. Which status change triggers which is in the reaction-by-change table next to it. What the mascot says lives in its phrase list and is drawn on the free line under it: never add a new line for that.
- Effects that depend on status and are not a mode (sweating at high quota) go in `mascots.Effects`.
- One clock: the animation tick (`internal/ui`, every 450 ms) and the model's frame counter. Each mascot is offset (`frame+i*3` in the cards code) so they don't move in unison. Never `time.Sleep` or goroutines of their own to animate.
- `panal pet` (`internal/pet`) reuses the same sprites, `ui.StatusMode`, `ui.ReactionForChange` and `mascots.Cells` (the half-block encoding); it animates at 4 fps from the wall clock and its reactions last `Duration(mode)` × 450 ms. A change to the status → mode or reaction rules shows in both; check its golden views (`go test ./internal/pet -update`) and its JSON contract (`docs/reference.md#pet-frames`), which another program depends on.
- If you raise the rate for a reaction, do it only while it lasts.
- With `NoAnimation` (`-no-animation`) there is no tick: nothing may depend on the frame counter advancing (that's why reacting does nothing then). With `NO_COLOR` no mascots are drawn.
- To review sprites without a terminal: `go run ./cmd/preview docs/animations.png` (regenerate it if you change frames).

## Keys

- Numbers for tabs (1 dashboard, 2 history, 3 report, 4 timeline; the bar abbreviates itself if it doesn't fit), ←→ between agents, ↑↓ in lists, enter/tab = detail, esc = back one level, `?` help, `q` quit.
- Every new key goes in the help (`?`) and, if it belongs to the current view and is frequent, in its footer.
- Don't steal terminal keys (ctrl+c quits, ctrl+z/ctrl+s are not used).

## Tests and verification

- The golden screens live in `internal/ui/testdata/` (80/100/132/160). If a visual change is intentional: `go test ./internal/ui -run TestScreens -update` and **review the diff** of the .txt files before calling it good.
- To see a screen without opening the UI: `go run ./cmd/panal -preview 80` (and `-screen history|detail|table|report`), `go run ./cmd/panal -mascots`.
- Before finishing: `go vet ./...`, `go test ./...`, and look at the preview at 60 and 80 columns.
- Reader tests use real samples in `testdata/`; don't invent formats.

## Checklist before handing in a UI change

1. Is the status readable without color (glyph + word)?
2. Does it look right at 60, 80 and 132 columns? What hides first?
3. Did you add any border, label or marker that carries no data? Remove it.
4. Is the new key or icon in the help?
5. Does the animation say something about the status, without making the layout jump?
6. Golden screens regenerated and diff reviewed? Does `go test ./...` pass?

# Changelog

All notable project changes are tracked here.

## Unreleased

- Navigation now follows vim conventions and arrow keys work everywhere: `j`/`k` or `↑`/`↓` move the cursor in the main form, history list and every overlay; `h`/`l` or `←`/`→` switch tab groups (also cycling select options while editing). The bottom help bar and each overlay's hint line spell out both key sets.

- The "相关链接" overlay now also lists Doubao resources (official docs and the new speech console). The Qianwen ASR model catalog link (qwen3-asr-flash-realtime 等) moved under the DashScope channel where it belongs — the OpenAI channel keeps no links since that catalog is not OpenAI-compatible.

- Engine credentials are now stored per engine (`[voice.engine_configs.<engine>]`) instead of one flat `engine_config`, so you can configure and keep keys for several ASR channels at once and switch between them without re-entering credentials. The TUI "engine config" overlay (renamed from "channel config") edits and persists the current engine's own settings; the legacy flat `engine_config` and `app_key`/`access_key`/`resource_id` fields remain as migration-read compatibility.
- Moved documentation links out of the channel picker (which previously rendered them as long underlined lines that broke layout) into a "相关链接" entry on the Engine tab: one overlay lists links for all engines (not just the currently selected one), j/k to select and Enter opens the chosen link in the system browser (`xdg-open` / `open` / `rundll32`). Links live in a separate `engineLinks` table so adding one never touches picker layout.

- Added a DashScope (Qianwen AI platform) Fun-ASR realtime streaming engine (`dashscope`) over the official WebSocket API (`wss://dashscope.aliyuncs.com/api-ws/v1/inference/`): live partial results while recording, Bearer-token auth via `api_key`, configurable `model` / `sample_rate` / `base_url` / `language` in `[voice.engine_config]`. 8k models (e.g. `fun-asr-flash-8k-realtime`) only accept 8000 Hz audio, so the engine auto-downsamples the recorder's 16 kHz PCM 2:1 and defaults `sample_rate` to 8000 for them. Docs: https://platform.qianwenai.com/docs/developer-guides/speech/asr-realtime
- Reworked the TUI main view onto the charmbracelet component library: settings are grouped into tabs (General / Engine / Microphone / Output / History) rendered by `list` with title/description rows, string fields edit via `textinput`, and the key help bar is generated from bindings (`help`). The history view is now a tab whose entries can be copied with Enter. Vertical height only depends on the largest tab, so adding settings no longer stretches the screen.

- Added trailing-punctuation omission processed uniformly before output: the master toggle `omit_trailing_punct` strips a trailing Chinese period "。", with two sub-options (`omit_trailing_all` to strip all trailing punctuation, `omit_trailing_ascii` to additionally strip ASCII `.` `!` `?`) shown indented under it in the TUI.

- Added a microphone debug panel in the TUI: the same recording hotkey triggers a test recording (no ASR), with live peak/RMS level bars, clipping warning, fine-grained gain adjustment (`+`/`-`, persisted), and an adaptive-gain mode that auto-lowers gain on clipping and recovers slowly when quiet (applies to normal recordings too, toggleable via `[voice].auto_gain`).
- Added an in-memory recognition history (last 10 submitted/copied results) with a TUI panel to re-copy any entry to the clipboard.

- Added copyright headers to source files (new files © conglinyizhi, modified files carry a modified notice), plus `NOTICE` and `THIRD_PARTY_NOTICES` documenting project ownership, maintenance history, and third-party dependency licenses. Maintenance of Just Talk moved from `whoamihappyhacking` to `conglinyizhi`; the project remains GPL-3.0.
- Renamed the application to Just Talk Plus (in-app TUI title, README, doctor output, and install paths); the binary and config paths keep the `just-talk` name.

- Added a pluggable ASR engine layer (`internal/speech`) with a registry, ported from the go-human-handler speech module: `doubao` (Volcano Engine, streaming + batch), `auralwise` (async task transcription), and a new OpenAI-compatible Whisper engine (`openai`). `[voice].engine` selects the engine and `[voice.engine_config]` holds engine-specific settings; `app_key`/`access_key`/`resource_id` remain as Doubao legacy shortcuts.
- Batch-only engines are wrapped into streaming sessions via a buffering adapter (`speech.BatchSession`) so the recording pipeline stays unchanged; batch engines get a longer final-result wait.
- The TUI gained an engine selector, and `--doctor` now reports the configured engine's availability instead of assuming Doubao credentials.
- Reworked the TUI into an engine-aware dynamic form: switching the Engine field shows only that engine's settings (Doubao model version/stream mode/two credential sets/hotwords; AuralWise API key/base URL/optimize; OpenAI API key/base URL/model) and saves them into `engine_config`; legacy flat Doubao fields are migrated into the form.
- The Engine field now opens a channel picker overlay (j/k navigation, per-channel description subtitles) instead of a cycling select; selecting a channel rebuilds the form underneath.
- Renamed the Doubao engine type from `doubao` to `doubao-stream` to make the streaming nature explicit; the picker description notes that the 1.0 / 2.0 model version is chosen after selecting the channel. The OpenAI channel description points to the QianwenAI model catalog for available ASR models.
- Removed engine type aliases (`volc-asr`, `doubao-asr`, `aural-wise`); the canonical types are `doubao-stream` / `auralwise` / `openai`, and `null` is now an internal fallback only. Doubao's 1.0 (bigasr) vs 2.0 (seedasr) access modes are documented in the README with a comparison table.
- `--doctor` ASR guidance now asks users to configure any one of the supported engines instead of showing a raw Doubao credential error.

## v0.0.3 - 2026-07-29

- Reworked Windows modifier-only hotkeys so the low-level hook only observes key edges and never consumes or replays modifiers. `Alt+Super` works through exact physical-state matching, stale hook state is cleared without combining separate single-key presses, and system shortcuts such as `Alt+Tab` remain untouched.

## v0.0.2 - 2026-07-21

- Fixed intermittent Windows `Alt+Super` false activations where stale low-level hook state could make a later Alt-only press look like the full shortcut; Windows modifier combinations now require an exact modifier set and reset stale suppressed state after physical release.

## v0.0.1 - 2026-07-17

- Added a tag-triggered GoReleaser v2 pipeline using the official GitHub Action to build Linux, macOS, and Windows amd64/arm64 archives on native runners and publish SHA256 checksums.
- Added Windows 10/11 support with global key-state monitoring, native WinMM microphone capture, Unicode clipboard access, SendInput auto-submit, a click-through Win32 status overlay, and Windows environment checks.
- Added Windows-standard config, state, log, and per-user installation paths. Windows builds no longer require external recording or clipboard commands.
- Added an opt-in Windows microphone integration test using `JUST_TALK_TEST_WINDOWS_AUDIO=1`.
- Added a low-level Windows keyboard-hook fallback so modifier-only shortcuts such as `Alt+Super` still work when `GetAsyncKeyState` misses the physical Windows key.
- Consume Windows modifier-only voice shortcuts such as `Alt+Super` so they do not activate menus or toolbars in the focused application, while replaying unrelated modifier shortcuts normally.
- Fixed recording shutdown getting stuck while ASR audio writes were still in flight, and accept empty final ASR responses without waiting for a false timeout.
- Redesigned the Windows status overlay with per-pixel transparency, antialiased animated waveform bars, smoother capsule edges, and a soft shadow.
- Clarified README build and install setup steps for the repository directory and `~/.local/bin` PATH.
- Restricted voice hotkeys to non-text global shortcut keys, rejecting letters, digits, punctuation, Space, and similar text-producing keys.
- Avoid duplicate auto-submit on KDE Plasma by using uinput directly and not writing the Wayland primary selection there.
- TUI is now the default startup mode.
- Added persistent usage statistics for total sessions, recognized characters, average speed, and recent speed.
- Added configurable ASR hotwords.
- Added TUI help toggle with `h`.
- Improved Wayland clipboard and auto-submit behavior with `wl-copy` and `wtype`.
- Added Linux recording status overlay for X11 and Wayland.
- Added macOS support for global hotkeys, native recording, clipboard, auto-submit, recording status overlay, and environment checks.
- Removed non-cgo macOS fallback builds; Just Talk now requires cgo for native platform integration.
- Replaced the old Claude-specific agent guide with `AGENTS.md` and clarified build documentation.
- Improved toggle and hold hotkey behavior for fast repeated key presses.
- Show ASR connection and final-result timeout errors in the status UI/overlay instead of immediately falling back to idle.
- Added transient `Esc` cancel and `R` retry hotkeys while recording or showing retryable errors.
- Improved X11 overlay placement on multi-monitor setups and switched X11 rendering to an ARGB window for smoother rounded corners.
- Fixed a Wayland overlay shutdown race that could crash while closing the app, and surfaced Linux `arecord` microphone/device failures in the UI.
- Made `Esc` cancel active overlay states, including the final ASR wait state, and suppress output from canceled pending sessions.
- Improved Wayland overlay rounded-corner antialiasing, especially on KDE Plasma.
- Added `just-talk --install` and `make install` to install the binary into `~/.local/bin`.

## 2026-05-30

- Initial Linux-focused development snapshot.
- Supported Linux Wayland hotkeys via evdev.
- Supported Linux X11 hotkeys via native X11 grabs.
- Added Doubao streaming ASR integration.
- Added TUI configuration interface.
- Added automatic clipboard copy and auto-submit.

# AGENTS.md

Coding-agent guidance for working in this repository.

## License And Copyright — read first

This project is **GPL-3.0-only** (`LICENSE`). These rules are not optional:

- Never modify `LICENSE` or change the project license.
- Every Go source file carries a copyright header (`Copyright` + `SPDX-License-Identifier: GPL-3.0-only`). Never remove or alter an existing header.
- New files must include a header signed by the current maintainer:
  ```go
  // Copyright (C) 2026 conglinyizhi
  // SPDX-License-Identifier: GPL-3.0-only
  ```
- Files modified from the original Just Talk carry a "modified from the original Just Talk by whoamihappyhacking" notice; keep it intact (GPL §5 preserves upstream legal notices and marks changes).
- `NOTICE` and `THIRD_PARTY_NOTICES` document ownership and third-party licenses: do not delete them, and update `THIRD_PARTY_NOTICES` when adding dependencies. Keep third-party license headers intact (e.g. the xdg-shell / wlr-layer-shell protocol headers in `plugins/overlay/`).

## Project

Just Talk Plus is a desktop voice input tool: a global hotkey records audio, streaming ASR recognizes it, and the text is copied to the clipboard or submitted into the focused input field. Targets: Linux, macOS, Windows. (Successor of Just Talk; the `just-talk` binary and config paths keep the old name.)

## Build And Test

Linux/macOS need cgo; Windows builds with `CGO_ENABLED=0`.

```bash
go test ./...            # tests
go test ./... -tags no_x11
CGO_ENABLED=1 go build -o build/just-talk ./cmd/just-talk
go build -o build/just-talk.exe ./cmd/just-talk   # Windows
```

Runtime: `just-talk` (TUI default), `--no-tui` (daemon), `--doctor`, `--backend x11|wayland|darwin` (or `JUST_TALK_BACKEND`); `mock` is provider-testing only.

Never add or keep non-cgo macOS fallback builds: a build that compiles but lacks native hotkeys/recording/clipboard/auto-submit/overlay is worse than a failed build.

## Architecture

Startup: `cmd/just-talk/main.go` → `config.Load` → `doctor.Run` → `hotkey.NewProvider` → `engine.New` → load voice + overlay plugins → TUI or daemon.

- `hotkey/`: global hotkey providers + combo/event types.
- `engine/`: plugin lifecycle, config reload.
- `plugins/voice/`: recorder, ASR sessions, hotkey behavior, clipboard/auto-submit dispatch, stats.
- `plugins/overlay/`: recording status capsule.
- `internal/speech/`: ASR engine abstraction (Engine + registry), streaming sessions, batch adapter, engines `doubao-stream` / `auralwise` / `openai` / `dashscope`.
- `internal/asrcfg/`: builds an engine from `config.VoiceConfig`; the single place importing engine packages (registers them).
- `internal/autotype/`, `internal/clipboard/`, `internal/doctor/`, `internal/tui/` (Bubble Tea UI on the charmbracelet component library: tab-grouped settings via `list`, `textinput` editing, `help` key bar; channel picker/config and mic-debug are modal overlays).

## ASR Engines

The voice plugin works against `speech.StreamSession`, never a concrete vendor.

- Selection: `[voice].engine` (default `doubao-stream`) + `[voice].engine_config`; legacy `app_key`/`access_key`/`resource_id` are Doubao-only shortcuts.
- Canonical types: `doubao-stream`, `auralwise`, `openai`, `dashscope`. Do not add aliases (removed on purpose).
- `null` is the internal fallback only; never list it in the TUI channel picker.
- Streaming engines implement `speech.StreamingEngine`; batch-only engines are wrapped by `speech.BatchSession` (buffers PCM, `Transcribe` at stop; the plugin waits longer for them).
- Adding an engine: `speech.Register` in the package init → blank import in `internal/asrcfg` → add to `asrcfg.Types()` (`userEngines`) and the TUI `engineChannels` → update the bilingual README. Match the recorder's 16 kHz 16-bit mono PCM.
- Doubao: `model_version` `"1.0"` (bigasr, `volc.bigasr.sauc.*`, `app_id`+`access_key`) or `"2.0"` (seedasr, `volc.seedasr.sauc.*`, `api_key`). The resource ID derives from `model_version` — never ship a default `resource_id` in `config.Default()` (it overrides the derivation).

## Platform Constraints

- Wayland: evdev hotkeys (`input` group), `wl-clipboard`/`wtype` for clipboard/auto-submit.
- X11: native X11 via cgo; auto-submit uses XTest.
- macOS: CGEventTap hotkeys, CoreAudio/AudioQueue recording, NSPasteboard, AppKit NSPanel overlay; users grant Accessibility/Microphone to the launching terminal, not a `.app` bundle.
- Windows: `GetAsyncKeyState` polling + observational `WH_KEYBOARD_LL` fallback that always passes events through and never consumes/replays modifiers (`Alt`, `Super`, `Alt+Tab` stay untouched; combinations need an exact modifier set). winmm recording (16 kHz 16-bit mono), Win32 clipboard, `SendInput` Ctrl+V, click-through topmost overlay. No ffmpeg/SoX/cgo/admin needed.

## Behavioural Rules

- Hotkeys: register in `Plugin.Init()`, never `Start()` (late registration drops events). Keep provider event loops and hotkey handlers free of blocking work.
- Recording stop is split from hotkey handling: handlers update state fast; recorder stop, final audio send, ASR final wait, clipboard, auto-submit run in background finish work. Debug logs say which stage a stop is waiting on.
- Dispatch recognized text exactly once per user-stopped session; when changing ASR result handling check both `Final()` and `Done()` paths.
- The microphone debug panel routes the recording hotkey into a test recording (no ASR) while active; the shared `AutoGain` (adaptive gain) applies to both test and normal recordings and analyzes post-gain PCM. Successful text output is recorded into the in-memory history (`history.go`).
- TUI mode must not write logs to stdout/stderr (corrupts the Bubble Tea layout); `voice.SetOutput(io.Discard)` is intentional.
- Prefer existing package boundaries and platform-specific build tags. Keep doctor output short and actionable.
- README is bilingual (`README.md` + `README.en.md`); update `CHANGELOG.md` for user-visible changes.

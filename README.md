# Just Talk Plus

[English](README.en.md)

减少用键盘的次数，改用口喷吧。

Just Talk Plus 是一个面向桌面环境的语音输入工具。它通过全局快捷键录音，把语音识别结果复制到剪贴板，或直接上屏到当前输入框，适合写代码、聊天、记笔记和处理长文本输入。

## 功能

- 全局热键录音（`toggle` / `hold`），流式识别后自动上屏或复制到剪贴板。
- 多引擎可选：豆包流式 ASR（默认）、千问 Fun-ASR、AuralWise、OpenAI 兼容端点、小米 MiMo、硅基流动。
- 中文输入优化：句尾标点省略、中文间空格清理、中英文间自动补空格。
- Android 手机可作为录音麦克风（内嵌 AndroidMic 服务端，手机端装官方 APK）。
- 麦克风调试面板：测试录音、电平与增益调整、自适应增益。
- 识别历史与可选录音保留，便于回放与复查。
- 顶层录音状态胶囊；TUI 采用 tab 分组配置。

## 测试状态

目前主要在 Linux Wayland 后端 + 豆包流式 ASR（单向/双向）环境下开发与测试；其他平台、后端与引擎未充分验证，可能存在兼容性问题。

## 平台状态

当前支持 Linux、macOS 和 Windows 桌面：

| 平台          | 状态   | 说明                                                                                                |
| ------------- | ------ | --------------------------------------------------------------------------------------------------- |
| Linux Wayland | 已支持 | 已支持 Sway / wlroots 场景；快捷键基于 evdev，需要 input 权限                                       |
| Linux X11     | 已支持 | 使用 X11 原生全局热键                                                                               |
| macOS         | 已支持 | 全局快捷键基于 CGEventTap，录音使用 CoreAudio，剪贴板使用 NSPasteboard，胶囊显示使用 AppKit NSPanel |
| Windows 10/11 | 已支持 | 全局按键轮询及低级键盘钩子边沿回退、WinMM 录音、Unicode 剪贴板、SendInput 自动上屏和 Win32 状态胶囊 |

## 构建

Just Talk Plus 依赖平台原生能力。Linux 和 macOS 构建需要启用 cgo；Windows 使用纯 Go 的 Win32 调用，不需要 cgo。

Linux 构建依赖：

Linux 构建会**同时编译 X11 与 Wayland 两个后端**（cgo），运行时按会话自动选择：`XDG_SESSION_TYPE=wayland`（或设置了 `WAYLAND_DISPLAY`）时优先 Wayland 后端，否则用 X11 后端；也可用 `JUST_TALK_PLUS_BACKEND=x11|wayland` 显式指定。因此编译期依赖需一次装齐两套：

```bash
# Arch Linux
sudo pacman -S --needed go gcc cairo libx11 libxtst libxext libxinerama libxrender wayland

# Debian / Ubuntu
sudo apt install golang-go build-essential libcairo2-dev libx11-dev libxtst-dev libxext-dev libxinerama-dev libxrender-dev libwayland-dev
```

运行期工具按实际使用的后端安装：

```bash
# Wayland 会话：剪贴板（wl-copy/wl-paste）与自动上屏（wtype）
# Arch Linux
sudo pacman -S --needed wl-clipboard wtype
# Debian / Ubuntu
sudo apt install wl-clipboard wtype

# X11 会话：剪贴板
# Arch Linux
sudo pacman -S --needed xclip
# Debian / Ubuntu
sudo apt install xclip
```

macOS 构建依赖：

```bash
# 需要 Apple Command Line Tools 提供 clang 和 macOS SDK；不需要安装完整 Xcode。
xcode-select --install
```

Windows 构建依赖：

```powershell
# 安装 Go 1.25 或更高版本；不需要额外安装 ffmpeg、SoX 或 C 编译器。
winget install --id GoLang.Go --exact
```

构建当前平台二进制：

```bash
cd just-talk-plus
CGO_ENABLED=1 go build -o build/just-talk-plus ./cmd/just-talk-plus
```

Windows PowerShell：

```powershell
cd just-talk-plus
go build -o build\just-talk-plus.exe .\cmd\just-talk-plus
```

安装到 `~/.local/bin/just-talk-plus`：

```bash
# 确保 ~/.local/bin 在 PATH 中（如未配置，将下面这行加入 ~/.bashrc 或 ~/.zshrc）
# export PATH="$HOME/.local/bin:$PATH"
build/just-talk-plus --install
# 或
make install
```

macOS 需要在本机 macOS 上构建；项目不提供非 cgo 版本。

Windows 安装到 `%LOCALAPPDATA%\Programs\Just Talk Plus\just-talk-plus.exe`：

```powershell
.\build\just-talk-plus.exe --install
# 如果安装目录尚未在 PATH 中，按命令输出提示添加即可。
```

## Release 下载

GitHub Release 提供以下预编译归档：

- Linux amd64 / arm64
- macOS Intel / Apple Silicon
- Windows amd64 / arm64
- `SHA256SUMS.txt` 文件校验

发布流程使用 GoReleaser v2 和官方 `goreleaser/goreleaser-action`。Linux、macOS 和 Windows 二进制分别在对应的 GitHub 托管 runner 上原生构建；维护者推送 `v*` 标签时会自动构建并发布，例如：

```bash
git tag v0.2
git push origin v0.2
```

## 使用

默认启动 TUI（完整控制面板）：

```bash
just-talk-plus
```

后台 daemon（挂系统托盘）：

```bash
just-talk-plus --gui
```

TUI 模式（等价默认，显式指定）：

```bash
just-talk-plus --tui
```

配置模式（仅编辑配置，不录音/不开服务，跳过医生检查，可与后台 daemon 并行）：

```bash
just-talk-plus --tui-config
```

> `--gui` = 后台 daemon + 托盘；`--tui` = 完整 TUI（默认，会注册录音热键，勿与 daemon 并行）；`--tui-config` = 纯配置编辑器（保存后 daemon 自动热重载）。

后台模式默认挂系统托盘（见下方「系统托盘」），切换引擎 / 设备、开关 push-to-talk、查看或打开日志、重启当前程序、退出都可从托盘图标完成。

指定后端：

```bash
just-talk-plus --backend wayland
just-talk-plus --backend x11
```

Windows 不需要指定后端。首次使用前可检查麦克风和配置：

```powershell
.\build\just-talk-plus.exe --doctor
```

### systemd 开机自启（Linux）

后台服务可由 systemd --user 托管，实现开机自启与日志接管（journald）：

```bash
just-talk-plus --install-systemd    # 生成并启用 unit（运行 --gui，日志走 journald）
systemctl --user start just-talk-plus   # 立即启动
journalctl --user -u just-talk-plus -f  # 查看日志
```

- 生成的服务运行 `just-talk-plus --gui`，并在安装时将当前检测到的后端写入 `--backend wayland` 或 `--backend x11`，避免启动时图形环境变量尚未就绪导致 `unknown backend`；`StandardOutput/Error=journal`，daemon 识别到 systemd 环境（`INVOCATION_ID`）后日志只走 stderr 交给 journald，不再写 `/tmp` 文件。
- 系统未登录也开机自启：`loginctl enable-linger $USER`。

### 使用 Android 手机作为麦克风

PC 端（本项目）只提供内嵌的 TCP 服务端；手机端需要安装 AndroidMic 官方 APK 才能连接。

- 到 [AndroidMic 官方 Release](https://github.com/teamclouday/AndroidMic/releases) 下载并安装手机端 APK
- 本项目**不维护、不分发**手机端 APK，请直接使用官方原版
- 手机与电脑需在同一局域网：先在 TUI「输入 → Android 虚拟麦克风」启动服务端，再在手机端填入电脑 IP 与端口（默认 `54345`）连接

开发调试可使用 Air 热重载（需先安装 `air`）：

```bash
go install github.com/air-verse/air@latest
make dev
make dev ARGS="--gui --verbose"
```

代码或配置变更后会自动重新构建并重启调试 CLI，临时二进制位于 `tmp/`。
在 Linux/macOS 上，Air 会通过终端入口程序将前台终端临时交给 TUI，并在退出或热重载时交还给 Air；这样 Bubble Tea 可正常接收按键。如果需要观察 Air 自身日志，可临时执行 `air -c .air.toml -log.main_only false`。

使用 `doubao-stream` 时，历史页会显示本次识别 WebSocket 握手响应中的 `X-Tt-Logid`，可用于向豆包支持团队追溯上游日志。

## 配置

默认配置路径：

```text
# Linux / macOS
~/.config/just-talk-plus/config.toml

# Windows
%APPDATA%\just-talk-plus\config.toml
```

推荐热键配置：

```toml
[voice]
mode = "toggle"
push_to_talk = "Alt+Super"
gain = 1
auto_gain = true   # 自适应增益：仅在检测到人声时缓慢抬升弱输入；响亮块整体预缩放防削波，限幅时渐进回撤
# auto_gain_target_peak = 0.42  # 目标输出峰值（默认 0.42）
# auto_gain_max_peak = 0.65     # 输出峰值超过此值开始回撤增益（默认 0.65）
# auto_gain_max_rise = 1.15     # 每块（约 200ms）最大升高倍数（默认 1.15）
# auto_gain_max_gain = 25       # 增益上限（倍，默认 25；上限 100）
# 以上 4 个参数也可在 TUI 的「输入 → 自适应增益」下直接调整：留空即回内置默认，保存后重启生效。
stop_delay_ms = 3000  # 停止后的静音等待时长
stop_after_silence = true  # 停止请求后，持续收到人声就重置等待；静音满上述时长才截断
# 底噪判定（TUI「输入」tab 可调）：
# noise_threshold = 0.015  # 底噪阈值（RMS）：开头超过它开始录音，结束持续低于它满停止延迟即停止；低于它的音频始终原样录制
# noise_deadband = 0.1     # 停止判定容差：高于底噪阈值多少才算人声；别调太大，否则正常说话会被判成噪音，提前截断丢字
# noise_floor_auto = false # 自动底噪：开启后停止判定阈值随环境自动漂移，无需手动校准
# 延迟麦克风建议：停止延迟应不小于设备的最大传输延迟；采集会在 ASR 拨号期间持续缓存，连接后补发。
omit_trailing_punct = false  # 输出前省略句尾句号「。」（主设置）
# omit_trailing_all = false  # 副设置：切断句尾所有符号（含英文）
# omit_trailing_ascii = false  # 副设置：额外省略英文 . ! ?
# remove_cjk_space = false  # 删除中文与中文之间的空格（思考停顿产生的空隙）；中文与英文/数字间的空格保留
# auto_space = true  # 中文排版自动空格：中英/中数间补空格，全角标点两侧去空格，全角数字转半角
# 上屏与复制（归在 auto_submit 下）：
# auto_submit = true          # 自动上屏：模拟粘贴到当前输入框
# write_clipboard = true      # 子项：复制到剪贴板（识别结果写入剪贴板）
# restore_clipboard = true    # 子项：上屏后还原剪贴板（成功后还原原内容）
# no_clipboard_on_fail = true # 子项：上屏失败不写入（失败时也还原原内容）
# 剪贴板恢复仅对支持的纯文本、HTML、PNG 快照生效；Linux 遇到多 MIME、文件列表或未知格式会跳过恢复，避免把图片二进制误写成文本。自动上屏期间用户复制了新内容时也会跳过恢复。

# 诊断收集默认关闭；在 TUI 选择“本次运行”即时启用；3 = 后续 3 次启动，-1 = 持续开启
# [diagnostics]
# startup_runs = 3
```

`Alt+Super` 配合 `toggle` 模式是推荐用法。按一次开始录音，再按一次停止录音，避免按住模式下和桌面环境或输入框发生按键冲突。在 Windows 上，低级键盘钩子只观察按键边沿，不会消费或回放修饰键，因此单独使用 `Alt`、`Super` 或 `Alt+Tab` 时会保持系统原有行为。组合键必须精确匹配配置的修饰键集合；钩子回退状态会通过未拦截的物理按键状态校验，避免把两次独立的单键按下拼成组合键。

语音热键只支持适合作为全局快捷键的按键：

- 支持：纯修饰键组合，如 `Alt+Super`、`Ctrl+Alt+Shift`。
- 支持：功能键 `F1` 到 `F24`，如 `F9`、`Alt+F8`。
- 支持：非文本控制键和导航键，如 `Tab`、`Enter`、`Escape`、`Backspace`、`CapsLock`、`Up`、`Down`、`Left`、`Right`、`Home`、`End`、`PageUp`、`PageDown`、`Insert`、`Delete`。
- 不支持：字母、数字、标点、空格、数字小键盘数字和符号等会输入文本的按键，如 `Alt+G`、`G`、`Alt+1`、`Alt+Space`。

> 提示：若与本项目的上游 just-talk-go（Go 版）同时安装使用，两边默认录音热键相同（`Alt+Super`），请把两边的热键配置成不同的按键，避免同时触发。

热词示例：

```toml
[voice]
hotwords = ["Wayland", "Sway", "wl-copy", "wtype", "just-talk-plus"]
```

macOS 热键写法：

```toml
[voice]
# Option 等价于 Alt，Command/Cmd 等价于 Super
push_to_talk = "Option+Command"
```

Windows 使用 `Win` 或 `Super` 表示 Windows 徽标键。如果麦克风不可用，请在“Windows 设置 → 隐私和安全性 → 麦克风”中允许桌面应用访问麦克风。

### 系统托盘（后台控制面板）

`--gui` 零窗口后台 daemon 默认挂系统托盘/菜单栏图标，用于不占终端窗口的常驻控制：切换识别引擎、切换麦克风设备、开关 push-to-talk、控制 Android 虚拟麦克风服务端、查看或打开日志、重启当前程序，以及退出。Android 虚拟麦克风子菜单中的「复制诊断信息」会把最近 10 次连接的收包、转换和丢帧统计复制到剪贴板，不包含音频、识别文本或密钥，也不持续写入日志文件。托盘「重启」会在当前进程内重新加载程序，不调用 `systemctl --user restart`，适合开发构建完成后直接让后台 daemon 使用新版本。录音状态用托盘图标颜色提示（待机=灰、录音中=红、停止中=黄、错误=深红），悬停 tooltip 显示当前引擎与模式。历史记录与麦克风频谱等重度交互托盘装不下，保留 `just-talk-plus --tui` 完整终端面板兜底。识别结果确认后历史会立即刷新；配置文件热加载后，托盘「配置状态」会显示应用成功或失败，并弹出桌面提示。

```toml
[tray]
enabled = true                   # 关掉则后台不挂托盘
# cycle_engine_hotkey = "Alt+F9"     # 可选：全局热键循环切换识别引擎
# toggle_push_to_talk_hotkey = "Alt+F8"  # 可选：全局热键切换 push-to-talk / toggle
```

- 托盘仅在后台模式（`just-talk-plus --gui` / 默认后台）生效；完整 TUI（默认/`--tui`）会注册录音热键，勿与后台 daemon 并行；`--tui-config` 是配置模式，可与 daemon 并行。
- 全局热键受与语音热键相同的按键约束（见上方「推荐热键配置」），留空即禁用；如与录音热键冲突，注册会失败并记录日志，不影响 daemon。
- `Android 虚拟麦克风` 子菜单可启动/停止内嵌服务端、开关自动启用（`androidmic_auto_start`）、或一键把当前输入设为手机（`device = "androidmic"`）。

### 语音引擎

`[voice]` 下的 `engine` 选择识别引擎，每个引擎的私有配置单独存放于 `[voice.engine_configs.<engine>]`（如 `[voice.engine_configs.openai]`），可同时保存多个渠道的凭据、互不覆盖。默认 `engine = "doubao-stream"`。TUI 中「引擎」字段按回车会打开渠道选择弹层，每个渠道附特点说明；「引擎配置」二级弹层（按回车进入、`s` 保存、`esc` 取消）编辑当前引擎的凭据与专属设置，保存后写入 `engine_configs.<engine>`；「相关链接」弹层汇总全部引擎的文档链接，回车用浏览器打开。旧版平铺 `engine_config` 与 `app_key` / `access_key` / `resource_id` 字段仍作为迁移读取的兼容写法保留。

#### 豆包（火山引擎）流式 ASR

边录边识别，支持热词。豆包有两种接入方式：

| 接入方式               | 模型版本 | 凭据                                                           | 资源 ID（默认）       |
| ---------------------- | -------- | -------------------------------------------------------------- | --------------------- |
| 豆包 1.0（旧版控制台） | `1.0`    | `app_id` + `access_key`（或旧版平铺 `app_key` / `access_key`） | `volc.bigasr.sauc.*`  |
| 豆包 2.0（新版控制台） | `2.0`    | `api_key`                                                      | `volc.seedasr.sauc.*` |

新版凭据二选一：新版控制台创建应用后拿到 `X-Api-Key`，用 `api_key`；旧版控制台的 App ID + Access Token 用 `app_id` + `access_key`。示例：

```toml
[voice]
engine = "doubao-stream"
language = "zh-CN"

[voice.engine_configs.doubao-stream]
# 新版控制台凭据
api_key = "…"
# 或用旧版凭据：
# app_id = "…"
# access_key = "…"
# 接入方式：
model_version = "1.0"      # 1.0=bigasr（默认） | 2.0=seedasr
stream_mode = "async"      # async=双向流式（低延迟、实时结果） | nostream=单向流式（完整音频后给最终结果）
# —— 识别质量选项（均在 TUI「引擎配置」弹层中，仅豆包引擎显示）——
# enable_lid = true         # 中英文及方言识别（中英混说建议开；语言需为自动或 zh-CN）
# enable_nonstream = true   # 二遍识别，最终更准（仅 async 双向流式生效）
# enable_itn = true         # 数字规范化（一九七零年 → 1970年）
# enable_punc = true        # 标点
# enable_ddc = false        # 语义顺滑（删语气词/重复词）
# boosting_table_name = ""  # 热词词表名（控制台自学习平台）
# boosting_table_id = ""
# correct_table_name = ""   # 替换词词表名
# correct_table_id = ""
# regex_correct_table_name = ""  # 正则替换词表名
# regex_correct_table_id = ""
# resource_id = "volc.bigasr.sauc.duration"  # 显式覆盖资源 ID
# base_host = "openspeech.bytedance.com"
```

AuralWise 异步任务转写（整段录音识别，无需公网 URL）：

```toml
[voice]
engine = "auralwise"

[voice.engine_configs.auralwise]
api_key = "asr_…"
# base_url = "https://api.auralwise.cn/v1"
# optimize = "auto"          # auto | true | false
# poll_timeout = "10m"
```

OpenAI 兼容 Whisper 接口（官方 API、Groq、本地 whisper 服务等）：

```toml
[voice]
engine = "openai"

[voice.engine_configs.openai]
api_key = "sk-…"
# base_url = "https://api.openai.com/v1"   # 可指向兼容端点
# model = "whisper-1"
```

千问（DashScope）Fun-ASR 实时流式识别（WebSocket，边录边出字）。文档见 [实时语音识别](https://platform.qianwenai.com/docs/developer-guides/speech/asr-realtime)，可用的 ASR 模型可参考 [千问实时 ASR 模型库（搜索 asr）](https://www.qianwenai.com/models?search=asr)：

```toml
[voice]
engine = "dashscope"

[voice.engine_configs.dashscope]
api_key = "sk-…"        # DASHSCOPE_API_KEY
# model = "fun-asr-realtime"            # 8k 模型（如 fun-asr-flash-8k-realtime）自动降采样到 8000 Hz
# sample_rate = "16000"                 # 8000 | 16000
# base_url = "wss://dashscope.aliyuncs.com/api-ws/v1/inference/"
# language = "zh-CN"
```

小米 MiMo 语音识别（整段录音识别，走 OpenAI Chat Completions + input_audio 形态）。文档见 [MiMo 语音识别文档](https://mimo.mi.com/docs/zh-CN/api/audio/Speech-Recognition)，价格见 [MiMo 按量付费](https://mimo.mi.com/docs/zh-CN/price/pay-as-you-go)：

```toml
[voice]
engine = "mimo-asr"

[voice.engine_configs.mimo-asr]
api_key = "…"                          # 小米 MiMo API Key
# base_url = "https://api.xiaomimimo.com/v1"   # 默认即可
# model = "mimo-v2.5-asr"                     # 默认即可
# language = "auto"                            # auto / zh / en；空 = 自动
```

硅基流动（SiliconFlow）语音转写（整段录音识别，非流式）。文档见 [硅基流动语音转写文档](https://docs.siliconflow.cn/docs/api/audio-transcriptions-post)，可选模型以官网模型列表为准：

```toml
[voice]
engine = "siliconflow"

[voice.engine_configs.siliconflow]
api_key = "sk-…"                              # 账户 API Key
# base_url = "https://api.siliconflow.cn/v1"  # 默认即可
# model = "FunAudioLLM/SenseVoiceSmall"       # 默认；可换 TeleAI/TeleSpeechASR 等
```

与豆包的边录边出字不同，硅基流动是整段识别：录音结束后把音频（≤50MB、≤1 小时）一次性提交到 `/v1/audio/transcriptions`，只返回一条完整文本，识别过程中没有中间结果；默认模型 `FunAudioLLM/SenseVoiceSmall`，模型名可在引擎配置里自行更换（以官网模型列表为准）。

批处理引擎（auralwise / openai / mimo-asr / siliconflow）在录音结束后一次性转写，需要更长的等待时间；流式引擎（doubao-stream / dashscope）在识别过程中即可看到部分结果。`engine = "null"` 用于禁用识别。

### 麦克风调试与识别历史

TUI 主界面选择「麦克风调试」回车进入调试面板：

- 按**同一录音热键**触发/停止测试录音（只测麦克风，不触发识别与上屏）
- `[` / `]`（或 `h`/`l`、`k`/`j`）循环切换录音设备（含「自动 = 系统默认」），切换即停止测试并持久化到 `[voice].device`（设备列表自动探测）
- 实时显示原始输入的峰值/真实电平条、应用增益后的真实电平条与“已限幅”警告；各电平条会暂留峰值标记
- `+` / `-` 细粒度调整增益（1–100，实时生效并持久化）
- 空格切换自适应增益：检测到有效人声后缓慢抬升弱输入（增益上限 25 倍）；静音不抬增益，响亮块整体预缩放防削波，限幅时渐进回撤
- 需要排查卡顿、上屏或识别异常时，在通用设置选择「[高级] 诊断收集」的本次、后续 3 次或持续启动；诊断启用后按 `K` 导出本机内存日志到 Kate。也可用 `--diagnostics` 仅为本次启动开启

「历史记录」面板展示最近 50 条成功上屏/复制的识别结果，`j`/`k` 选择、回车重新复制到剪贴板，`esc` 退出。历史滚动快照保存在系统运行时目录，程序重启后可恢复，系统重启后自动清理。

### 与剪贴板管理器（Klipper / CopyQ）配合

自动上屏在机制上需要「写剪贴板 → 模拟粘贴」，会在剪贴板历史（Klipper、CopyQ 等）留下一条识别文本记录。为了不污染剪贴板历史，Just Talk Plus 在 Linux/Wayland 上以**敏感内容**方式写入临时剪贴板（`wl-copy --sensitive`，即 `x-kde-passwordManagerHint: secret`）：

- **Klipper** 与 **CopyQ 8.0+** 识别该标记后**静默忽略**：不存储、不显示、不进入历史；
- 上屏仍可正常粘贴（文本本身不受影响），只是剪贴板管理器不记录。

启用方式：

1. 安装 CopyQ（Arch：`sudo pacman -S copyq`，其他发行版同理），并勾选「启动时自动运行」（或复制 `copyq.desktop` 到 `~/.config/autostart/`）；
2. CopyQ 启动后接管剪贴板历史；Klipper 无需卸载（它很小且可能被桌面元包依赖），若想避免双份历史可在「系统设置 → 开机与关机 → 自动启动」里取消 Klipper；
3. 剪贴板历史快捷键由 Klipper 的 `Ctrl+Alt+V` 换成 CopyQ 的（默认 `Ctrl+Alt+C`，可在 CopyQ 设置中自定义）。

> 说明：X11 会话下 `xclip` 不支持该敏感标记，X11 上屏仍通过「保存→粘贴→恢复」尽量还原剪贴板内容，但剪贴板管理器可能仍短暂记录一条；`wl-copy --sensitive` 需要 wl-clipboard ≥ 2.2（当前主流发行版均已包含）。

## 更新日志

见 [CHANGELOG.md](CHANGELOG.md)。

## 维护与贡献

Just Talk Plus 由 `conglinyizhi` 维护（基于 `whoamihappyhacking` 的原作继续开发，版权声明见 [NOTICE](NOTICE)）。

本项目不接受 Pull Request。欢迎通过 Issue 反馈 bug、使用体验和功能建议。

## 许可证

Just Talk Plus 使用 GNU General Public License v3.0 开源。

项目参考或移植了以下开源项目，版权与许可声明见 [NOTICE](NOTICE) 与 [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES)：

- [just-talk-go](https://github.com/whoamihappyhacking/just-talk-go)（GPL-3.0，作者 whoamihappyhacking）——本项目的前身与直接上游（Go 版），代码在其基础上继续开发。
- [AndroidMic](https://github.com/teamclouday/AndroidMic)（GPL-3.0，作者 wiiznokes / teamclouday）——内嵌 TCP 服务端按其协议实现（Go 重写），把 Android 手机用作录音麦克风。

其余依赖、协议头与移植来源见 THIRD_PARTY_NOTICES。

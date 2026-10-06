# One-KVM CLI

为 [One-KVM](https://github.com/mofeng-git/One-KVM) 提供的独立 Go 命令行工具和 Agent Skill。One-KVM 服务器、固件及硬件使用说明由上游项目维护；本仓库提供连接和操作该设备的客户端。

Go 原生 CLI，附带可安装的 `onekvm` Skill。支持 One-KVM 状态、截图和 USB 键鼠控制，适配 One-KVM 0.2.6（上游标签 `v260802`）。支持持续采集的 `stream start/stop/status`；不提供 `ai` 或 `config get` 命令。

## 使用

从 [GitHub Releases](https://github.com/LxFee/onekvm-cli/releases) 下载对应平台的 Skill 压缩包，核对同一版本的 `SHA256SUMS` 后解压得到 `onekvm/`。Windows 程序位于 `scripts/onekvm.exe`，Linux 位于 `scripts/onekvm`；无需安装 Go。以下以程序已在 PATH 中为例，也可使用完整路径执行。

```sh
onekvm target add home --url http://HOST:8080 --user USER
onekvm target use home
onekvm login
onekvm capabilities
onekvm status
onekvm stream start
# 等待 HDMI 建立信号后再截图
onekvm stream status
onekvm snapshot --out screen.jpg
onekvm mouse move --x 960 --y 540 --width 1920 --height 1080
onekvm mouse click --button left
onekvm mouse click --x 960 --y 540 --width 1920 --height 1080 --double
onekvm mouse scroll --delta -3
onekvm key Ctrl+Shift+Esc
onekvm type "Hello world"
onekvm type --file text.txt
onekvm stream stop
onekvm logout
```

`login` 隐藏输入密码，有双因素认证时继续输入验证码。`login --remember-password` 经用户选择后把密码放入 Windows DPAPI 或 Linux 系统凭据库；本地会话到期或被服务端撤销时，CLI 用它重新登录一次并更新会话。没有该选项时只保存会话。自动化也可以设置 `ONEKVM_PASSWORD`、`ONEKVM_TOTP`；除 `login` 命令外，环境变量登录仅用于当前进程，不保存新会话。密码没有命令行参数，也不写入配置。避免把密码直接写进命令历史。启用双因素认证时，自动重新登录仍需交互输入验证码或提供 `ONEKVM_TOTP`。

`target add/list/use/remove` 管理连接；`--target NAME` 为一次调用选择目标。替换或删除目标会删除对应本机会话和已记住的密码；如需撤销服务器会话，先执行 `logout`。

地址和用户名优先级：显式 `--url/--user` > `ONEKVM_URL/ONEKVM_USER` > 默认目标。显式 `--target` 则使用该目标并忽略地址/用户名环境变量，仍可用 `--url/--user` 覆盖。会话绑定实际地址和用户名，不会把其他目标的凭据发给新地址。只接受 HTTP(S) origin，不接受 URL 中的密码、路径、查询参数；不跟随 HTTP 重定向。HTTP 用于可信局域网，跨不可信网络应在服务端配置 HTTPS。

输出为 JSON；`--json` 使用紧凑 JSON，并使错误也以 JSON 写到 stderr。正常退出为 0；一般失败为 1，参数/输入校验失败通常为 2，认证失败为 3，HID 不可用为 4。Cobra 解析错误为 1。

## 持久化

| 内容 | Windows | Linux |
|---|---|---|
| 目标和默认项 | `%LOCALAPPDATA%/onekvm/config.json` | `${XDG_CONFIG_HOME:-~/.config}/onekvm/config.json` |
| 登录会话 | 同目录 `sessions/*.dpapi`，当前 Windows 用户 DPAPI 加密 | 系统凭据库，服务名包含配置目录标识 |
| 可选的记住密码 | 同目录 `credentials/*.dpapi`，当前 Windows 用户 DPAPI 加密 | 系统凭据库，和目标会话分开存储 |
| 截图 | 必须通过 `--out` 指定 | 必须通过 `--out` 指定 |

`ONEKVM_HOME` 覆盖配置根目录。Linux 凭据库不可用时，`login` 报错且不落地明文凭据，可用环境变量完成单次调用。`logout` 删除本地会话及可选的记住密码。程序不建立持久日志或截图缓存，不在 Skill 目录写运行数据。截图父目录需要存在，已有文件不会覆盖。

## 输入与截图语义

- `stream start` 保持采集开启，即使没有观看客户端也不会因空闲自动关闭。需要本次修订的服务器，且当前为 MJPEG 模式。`stream status` 显示 `keep_alive` 和实际采集状态。保持状态持续到 `stream stop` 或服务重启，不修改开机配置。
- `stream stop` 停止采集，也会打断其他观看客户端；新的观看请求可能再次开启采集。
- 截图在采集未启动时自动启动采集；不停止已有观看会话。首次启动后需要等待 HDMI 建立信号，否则可能收到黑色过渡帧。连续操作建议先 `stream start`，稍候再截图。
- 鼠标位置是最新截图的像素坐标，必须同时提供该截图的宽高。
- 修订服务器通过 `absolute_mouse_buttons` 表示绝对鼠标接口可以处理点击和滚轮，通过 `preserves_pointer_on_reset` 表示断开连接后保持指针位置。这样可以只启用绝对鼠标，为 USB 串口腾出端点。
- 未修改的上游 0.2.6 仍需要相对鼠标接口处理点击和滚轮，并会在连接断开后把绝对鼠标移回原点。CLI 根据实际能力标记判断，不根据版本号放宽检查；旧服务器使用带坐标的单条 `mouse click`，不要依赖上一次命令的位置。
- `capabilities` 分别报告移动、定位点击、滚轮能力，发送前检查完整操作所需的接口。内部仅读取 `/config/hid` 中的功能选择字段，不输出完整配置，也没有配置读取命令。
- `key` 支持字母、数字、F1–F12、Enter/Esc/Tab、方向键及常用导航键；修饰键支持 Ctrl/Shift/Alt/Win（Meta/Cmd）。特殊符号键可用 `Equal`、`Minus` 等物理键名。
- `type` 支持美式键盘布局 ASCII、换行和 Tab；最多 4096 字节。中文等字符会整段拒绝，不会输入到一半才报错。目标电脑需要处于英文/美式输入状态，Caps Lock 等状态会影响文字。`--stdin` 可避免把文本放进进程命令行；`--delay-ms` 默认 20，每次按下/释放之间等待。
- 发送输入前检查 `hid.available` 和 `hid.online`。OTG 数据线未连接受控电脑时拒绝发送。
- `sent: true` 表示完成 WebSocket 发送和传输屏障。需要观察目标状态时可另行截图；失败后不自动重发输入，以免重复点击/打字。
- 控制期间避免其他浏览器或客户端同时发送输入。连接关闭时上游释放 HID 状态。

## Skill

把压缩包中的整个 `onekvm/` 文件夹安装到使用者的 Skill 目录。源模板位于 `skill/onekvm/`；发布包额外包含编译后的程序及依赖许可证。Skill 内没有固定目标、用户名或凭据，读取上述用户配置。

## 开发和发布

```sh
go test ./...
go vet ./...
go build -o bin/onekvm ./cmd/onekvm
```

在 PowerShell 中执行 `./scripts/release.ps1 -Version v0.1.0`，生成 Windows amd64、Linux amd64/arm64 Skill 包及 SHA256SUMS。构建使用 `CGO_ENABLED=0`、`-trimpath`，模块版本固定在 go.mod/go.sum。Linux 凭据存储依赖用户桌面会话的系统密钥服务；无桌面的环境可使用环境变量认证。

测试包括 HTTP 登录与 TOTP、拒绝跨源重定向、会话绑定与过期、DPAPI 加密读写（Windows）、按需截图、二进制 HID WebSocket 交互、离线输入拒绝和输入编码。Linux 发布包为交叉编译，凭据库行为需要在目标 Linux 会话实测。

接口依据：[One-KVM v260802](https://github.com/mofeng-git/One-KVM/tree/v260802)，尤其 `src/hid/datachannel.rs`、`src/hid/websocket.rs` 和 API 路由实现。

## 许可证与上游

Copyright (c) 2026 LxFee。项目采用 **GNU GPL v3.0**，与 [One-KVM 上游许可证](https://github.com/mofeng-git/One-KVM/blob/v260802/LICENSE) 保持一致，完整文本见 [LICENSE](LICENSE)。发行包包含本项目许可证和依赖的第三方许可声明。

上游项目：[mofeng-git/One-KVM](https://github.com/mofeng-git/One-KVM)。本客户端通过 One-KVM 的 HTTP/WebSocket 接口工作；服务器版本和能力差异见上文，服务端问题请查阅上游文档。

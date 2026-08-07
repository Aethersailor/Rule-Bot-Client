# Rule-Bot Client

Rule-Bot Client 是一个小巧、无第三方运行时依赖的 Linux 守护进程。它持续监听一个或多个兼容 Clash API 的外部控制器日志流，将最终命中 `MATCH` 规则且此前未记录过的域名追加到纯文本文件中；还可选择将这些域名可靠地发送给 Rule-Bot 自动校验和添加直连规则。

Rule-Bot Client 核心进程只主动建立出站连接，不监听端口，也不需要数据库。原生 Linux、Debian、Docker 和 Docker Compose 继续使用 JSON 配置；OpenWrt 单包另含由本机 LuCI/rpcd 鉴权保护的 Web 管理界面。

## 与 Rule-Bot 联动

`rule_bot` 是可选的可靠投递模块，默认关闭。关闭时 Rule-Bot Client 只把捕获结果写入本地 `output`；开启后，新增域名会先持久化到本地文件，再经过隐私处理发送给 Rule-Bot，由 Rule-Bot 统一判断是否需要加入 GitHub 直连规则。

```text
Mihomo 最终命中 MATCH 的连接
    ↓
Rule-Bot Client 提取并规范化主机名（IDN 转为 Punycode）
    ↓
按 domain_mode 投影，并在投影后全局去重
    ↓
追加到本地 output，刷新并 fsync
    ↓
本地执行 .cn/排除项过滤和发送去重
    ↓
使用 Bearer Token 投递到 Rule-Bot
    ↓
Rule-Bot 执行规则/GeoSite/DNS/NS/GeoIP/频率限制检查
    ↓
收到终态后，Rule-Bot Client 原子推进 state_file 游标
```

Rule-Bot 提供两类互相隔离的入口；下列端口属于 Rule-Bot，不是 Rule-Bot Client 的监听端口：

| 入口 | Rule-Bot 默认端口 | 凭据 | 适用场景 |
| --- | ---: | --- | --- |
| 私用入口 | `8765` | 部署者生成的静态 Token | 同一部署者管理自己的 Rule-Bot Client 与 Rule-Bot |
| 社区入口 | `7654` | 每位 Telegram 用户独立签发、可过期和吊销的 Token | 群成员使用社区 Rule-Bot 服务 |

私用入口的完整 `endpoint` 和 Token 由 Rule-Bot 部署者提供。社区用户应在 Rule-Bot 私聊主菜单进入“Rule-Bot Client 接入”，阅读并同意当前隐私说明，通过实时群成员验证后签发 Token；重新签发会使旧 Token 立即失效。社区 Token 只携带不透明随机主体，不包含 Telegram 用户 ID；身份映射只保存在 Rule-Bot 服务端。两种入口必须使用不同端口、路径和凭据，不能把私用静态 Token 当作社区共享 Token。

Rule-Bot Client 固定发送以下请求，不能要求 Rule-Bot 改写其他仓库或绕过策略：

```http
POST /部署者提供的随机路径 HTTP/1.1
Authorization: Bearer <token>
Content-Type: application/json

{"version":1,"domain":"example.com"}
```

客户端不能指定目标仓库、规则文件、提交标题、来源身份或强制添加。Rule-Bot 会把请求送入与 Telegram 相同的服务端检查与串行写入路径；成功提交会返回 GitHub commit URL。

可靠投递以本地 `output` 为事实来源：

- 首次启用且没有状态文件时，`send_existing=false` 默认从文件末尾开始，避免意外批量发送历史记录；只有明确设为 `true` 才会回放已有内容。
- `added`、规则或 GeoSite 已存在、`.cn`、策略拒绝和无效域名属于终态，收到匹配的响应后才推进游标。
- 限流、服务异常、网络错误、非预期响应及 `401/403` 不推进游标，并按退避策略重试；每次重试都会重新读取 `token_file`，可以原地轮换凭据。
- 如果 Rule-Bot 已经提交，但 Rule-Bot Client 在保存游标前退出，重启后可能重发；Rule-Bot 的重复检查会把请求作为已存在处理，再安全推进游标。
- 私用和社区 API 提交都不会触发 Rule-Bot 的 Telegram 群组播报；成功结果只会进入部署者指定的 GitHub 规则文件和提交历史。

## 隐私边界与必要警告

> [!WARNING]
> Rule-Bot Client 观察的是设备或整个网络经过 Mihomo 的连接域名。启用者必须确认自己有权收集和外发这些数据；Rule-Bot 社区入口记录的是 Token 申请人的同意，不代表网络中其他用户或设备也已同意。

| 阶段 | 能看到或保存的内容 | 明确不会包含的内容 |
| --- | --- | --- |
| Rule-Bot Client 本地 | `output` 长期保存 `domain_mode` 选择的完整主机名或可注册域名；`state_file` 只保存文件字节游标 | 不保存 URL 路径、查询参数、网页内容或来源应用 |
| Rule-Bot Client 外发 | 默认发送缩减后的可注册域名和 Bearer Token；同一主域只发送一次 | 默认不发送完整子域、`.cn`、本地排除项、访问次数或时间戳 |
| Rule-Bot | 接收一个域名并进行规则判断；社区数据库保存 Telegram 用户 ID 与随机 Token 主体的映射、签发/到期/启用和隐私同意状态 | 不把原始 Token、请求域名、请求次数、客户端 IP 或最后使用时间写入数据库 |
| GitHub 规则仓库 | 成功添加的规则域名、Rule-Bot Client 来源标识和提交时间进入规则文件与 commit 历史 | 不写入 Telegram 用户 ID、Token、客户端 IP 或 Rule-Bot Client 实例名 |

必须同时理解以下边界：

- 本地 `output` 不会自动过期；即使启用可注册域名聚合，它仍可能反映访问行为。应使用受限目录和 `0600` 文件权限，禁止公开共享、备份到不可信位置或交给无关用户。
- `privacy.reduce_to_registrable_domain=true` 只是 Rule-Bot Client 的默认客户端策略。将其关闭会发送完整主机名并显著增加可识别性；第三方客户端也可能不遵守该默认值，Rule-Bot 服务方无法仅凭协议保证客户端已经缩减。
- `privacy.exclude_suffixes` 和 `privacy.exclude_file` 应用于永不外发的内部域名、家庭设备域名或其他敏感后缀。排除文件不可读、过大或包含无效条目时，投递会失败关闭，而不是绕过排除策略。
- `send_existing=true` 可能一次性外发历史文件中的大量域名。除非已经审查输出文件并明确接受该后果，否则保持默认 `false`。
- Token 是 Bearer 凭据，持有者可在吊销或到期前代表该入口提交域名。优先使用 `token_file` 并限制为 `0600`；不要把 Token 放进仓库、命令行、URL 查询参数或可公开读取的环境与配置文件。
- 经公网投递必须使用 HTTPS，但 HTTPS 只保护传输，不代替 Token 鉴权。随机路径只能减少扫描噪声，也不是认证手段。
- Cloudflare、反向代理或其他终止 TLS 的中间服务在技术上可以处理出口 IP、时间、请求路径、Authorization 头和域名正文。使用代理只会把入口看到的地址换成代理出口；代理提供方会成为新的信任方。Rule-Bot Client 不承诺匿名性。
- Rule-Bot Client 和 Rule-Bot 的业务日志不记录原始域名，只使用每次进程启动时随机密钥生成的短引用；但日志仍可能包含入口地址、HTTP 状态或网络错误等运维信息，仍应限制访问和轮转。
- 撤回社区隐私同意或吊销 Token 只会阻止后续投递，不能删除 Rule-Bot Client 本地输出，也不能撤回已经公开进入 GitHub 历史的规则。

更完整的客户端数据边界与用户控制方式见 [PRIVACY.md](PRIVACY.md)，服务端边界见 [Rule-Bot 隐私说明](https://github.com/Aethersailor/Rule-Bot/blob/master/PRIVACY.md)。

## 配置

最小配置如下：

```json
{
  "version": 1,
  "output": "/var/lib/rule-bot-client/domains.txt",
  "domain_mode": "registrable_domain",
  "instances": [
    {
      "name": "home",
      "url": "http://127.0.0.1:9090",
      "secret_file": "/etc/rule-bot-client/home.secret"
    }
  ]
}
```

将 Mihomo 的 `secret` 单独写入 `home.secret`，文件中只保留密钥本身。若控制器没有设置密钥，可以删掉 `secret_file` 这一行。

`domain_mode` 推荐设为 `registrable_domain`，让本地输出和后续 Rule-Bot 投递都在写盘前按可注册域名聚合。省略该字段时使用 `hostname`，保留完整主机名。`rule_bot` 完全可选；不配置或设为 `{"enabled":false}` 时只写本地域名文件。启用示例：

```json
"rule_bot": {
  "enabled": true,
  "endpoint": "https://rule-bot.example.com/api/v1/rule-bot-client/your-hidden-path",
  "token_file": "/etc/rule-bot-client/rulebot.token",
  "state_file": "/var/lib/rule-bot-client/rulebot-state.json",
  "send_existing": false,
  "privacy": {
    "reduce_to_registrable_domain": true,
    "exclude_suffixes": ["internal.example"],
    "exclude_file": "/etc/rule-bot-client/rulebot-exclude.txt"
  },
  "retry": {
    "initial_delay": "1s",
    "max_delay": "5m"
  }
}
```

推荐使用 Rule-Bot 的私用入口和静态 Token；社区用户则在 Rule-Bot 私聊菜单确认隐私说明并申请独立 Token。入口可使用 HTTP 或 HTTPS，但经公网发送时必须使用 HTTPS。默认隐私策略会在本地把完整子域缩减为可注册域名、合并重复主域并排除 `.cn`；完整流程和警告见上方“与 Rule-Bot 联动”及“隐私边界与必要警告”。

Mihomo 必须满足以下条件：

- 外部控制器地址可由 Rule-Bot Client 访问；
- 日志级别为 `info` 或更详细；
- 如果跨越不可信网络，应使用 HTTPS 或受保护的专用网络；
- 不要向不可信网络暴露未启用身份验证的控制器。

成功的最终规则判定属于 `info` 日志。如果 Mihomo 过滤了这些日志，Rule-Bot Client 无法通过控制器 API 重新构造它们。

## 部署

### 原生二进制

从 [Releases](https://github.com/Aethersailor/Rule-Bot-Client/releases) 下载与设备架构匹配的压缩包，解压后安装：

```sh
sudo install -m 0755 rule-bot-client /usr/local/bin/rule-bot-client
sudo install -d -m 0755 /etc/rule-bot-client
sudo install -d -m 0750 /var/lib/rule-bot-client
sudo install -m 0600 /dev/null /etc/rule-bot-client/config.json
sudo install -m 0600 /dev/null /etc/rule-bot-client/home.secret
sudo install -m 0600 /dev/null /etc/rule-bot-client/rulebot.token
sudo nano /etc/rule-bot-client/config.json
sudo nano /etc/rule-bot-client/home.secret
sudo nano /etc/rule-bot-client/rulebot.token
```

先检查配置，再启动：

```sh
sudo rule-bot-client --config /etc/rule-bot-client/config.json --check
sudo rule-bot-client --config /etc/rule-bot-client/config.json
```

Debian 软件包和 systemd 的后台运行方式见 [`deploy/`](deploy/)；OpenWrt 请使用下方的架构相关单包。

### OpenWrt 单包

OpenWrt 不发布手工复制的 tar.gz。`OpenWrt Packages` GitHub Actions 使用对应版本的官方 SDK 生成两类真正的包：OpenWrt 24.10 的 IPK，以及 OpenWrt 25.12 的 APK。每个架构只需安装一个 `luci-app-rule-bot-client` 包，包内同时包含核心二进制、procd 服务、UCI 配置、配置适配后端和 LuCI 页面。

普通用户推荐只下载统一安装入口。该脚本会识别本机使用 apk 还是 opkg，再从同一正式版本的 manifest 中选择本机支持的包架构；下载后同时核对文件大小和 SHA-256，最终仍由原生包管理器完成安装：

```sh
wget -O /tmp/install-rule-bot-client-openwrt.sh \
  https://github.com/Aethersailor/Rule-Bot-Client/releases/latest/download/install-rule-bot-client-openwrt.sh
less /tmp/install-rule-bot-client-openwrt.sh
sh /tmp/install-rule-bot-client-openwrt.sh
```

需要离线安装或自行审核包身份时，也可以从同一 Release 下载与本机管理器和架构匹配的单包，并对照 `openwrt-manifest.tsv` 或 `openwrt-checksums.txt` 校验：

```sh
# OpenWrt 24.10（opkg）
opkg install ./luci-app-rule-bot-client_*.ipk

# OpenWrt 25.12+（apk）；目前 Actions 产物没有仓库签名
apk add --allow-untrusted ./luci-app-rule-bot-client-*.apk
```

安装后从“服务 → Rule-Bot Client”完成设置。OpenClash、Nikki 和多个手工 Mihomo Controller 可任意组合启用；每个目标独立连接和重连，但结果仍按全局 `domain_mode` 去重，写入单一输出，并共享单一可选 Rule-Bot 投递状态。Rule-Bot 使用完整 endpoint 与 Token 两个字段，`send_existing` 默认关闭。

默认持久化目录为 `/etc/rule-bot-client/data/`。也可选择 `/tmp` 临时存储，或选择 `/mnt/...` 外部挂载；外部挂载缺失时服务会失败关闭，不会静默写回 Overlay。配置、凭据、证书、排除清单、输出和 Rule-Bot 状态均列入 sysupgrade keep 清单；“备份恢复”页面可导出或导入同一组数据，`/etc/rule-bot-client/recover.sh` 可在固件升级后按包 URL 和 SHA256 重装单包。自动发现的 Controller secret、生成配置和状态只写入 `/var/run/rule-bot-client/`，不会进入备份。

### Docker

先准备固定的数据目录和配置文件：

```sh
sudo install -d -m 0755 /opt/rule-bot-client
sudo install -d -o 10001 -g 10001 -m 0750 /opt/rule-bot-client/data
sudo install -o root -g 10001 -m 0640 /dev/null /opt/rule-bot-client/config.json
sudo install -o root -g 10001 -m 0640 /dev/null /opt/rule-bot-client/home.secret
sudo install -o root -g 10001 -m 0640 /dev/null /opt/rule-bot-client/rulebot.token
```

将以下内容保存到 `/opt/rule-bot-client/config.json`：

```json
{
  "version": 1,
  "output": "/data/data/domains.txt",
  "domain_mode": "registrable_domain",
  "flush_interval": "5s",
  "include_failed_connections": true,
  "include_single_label_hosts": false,
  "rule_bot": {
    "enabled": true,
    "endpoint": "https://rule-bot.example.com/api/v1/rule-bot-client/your-hidden-path",
    "token_file": "/data/rulebot.token",
    "state_file": "/data/data/rulebot-state.json",
    "send_existing": false
  },
  "instances": [
    {
      "name": "home",
      "url": "http://127.0.0.1:9090",
      "secret_file": "/data/home.secret"
    }
  ]
}
```

编辑配置，将 Mihomo 密钥写入 `home.secret`，并将 Rule-Bot Token 写入 `rulebot.token`：

```sh
sudo nano /opt/rule-bot-client/config.json
sudo nano /opt/rule-bot-client/home.secret
sudo nano /opt/rule-bot-client/rulebot.token
```

直接启动容器，不需要端口映射或环境变量：

```sh
docker run -d \
  --name rule-bot-client \
  --restart unless-stopped \
  --network host \
  -v /opt/rule-bot-client:/data \
  ghcr.io/aethersailor/rule-bot-client:latest
```

查看运行状态和已收集的域名：

```sh
docker logs rule-bot-client
cat /opt/rule-bot-client/data/domains.txt
```

### Docker Compose

先按上一节准备 `/opt/rule-bot-client/config.json`、`home.secret` 和 `data` 目录，然后将仓库中的 [`compose.yaml`](compose.yaml) 放到 `/opt/rule-bot-client/compose.yaml`：

```sh
sudo curl -fsSL https://raw.githubusercontent.com/Aethersailor/Rule-Bot-Client/master/compose.yaml -o /opt/rule-bot-client/compose.yaml
cd /opt/rule-bot-client
docker compose up -d
docker compose logs -f
```

Docker 与 Docker Compose 示例使用宿主机网络，因此可以直接连接监听在宿主机 `127.0.0.1:9090` 的 Mihomo。控制器在其他设备上时，只需修改 `url`。

Rule-Bot Client 本身不提供网络服务，因此不需要映射任何端口。

## 配置说明

`version`、`output` 和至少一个 `instances` 实例是必填项。程序会拒绝未知 JSON 字段，让拼写错误直接暴露，而不是被静默忽略。`domain_mode` 可取 `hostname` 或 `registrable_domain`；省略时使用 `hostname`，推荐显式选择 `registrable_domain`。一个 Rule-Bot Client 进程可以同时监听多个控制器：

```json
{
  "version": 1,
  "output": "/var/lib/rule-bot-client/domains.txt",
  "domain_mode": "registrable_domain",
  "flush_interval": "5s",
  "include_failed_connections": true,
  "include_single_label_hosts": false,
  "instances": [
    {
      "name": "home",
      "url": "http://127.0.0.1:9090",
      "secret_file": "/etc/rule-bot-client/home.secret"
    },
    {
      "name": "remote",
      "url": "https://controller.example:9090",
      "secret_file": "/etc/rule-bot-client/remote.secret",
      "tls": {
        "ca_file": "/etc/rule-bot-client/private-ca.pem",
        "server_name": "controller.example"
      },
      "reconnect": {
        "initial_delay": "500ms",
        "max_delay": "30s"
      }
    }
  ]
}
```

每个实例只能使用 `secret_file`、`secret_env` 或 `secret` 中的一种密钥来源。推荐使用 `secret_file`，避免将凭据直接写入配置文件或启动参数。

TLS 证书校验默认开启。`insecure_skip_verify` 仅作为紧急情况下的显式逃生选项；启用后，程序会在启动日志中发出警告。

Mihomo 未启动、重启、拒绝连接或控制器暂时返回错误时，实例会按 `reconnect.initial_delay` 至 `reconnect.max_delay` 的指数退避持续重连；默认范围为 `500ms` 至 `30s`，无需手工干预。成功恢复后日志会报告离线时长和重试次数。为兼容 Mihomo 在首条日志出现前可能不发送响应头的行为，Rule-Bot Client 不会仅因日志流长时间安静而主动断开；内核退出或重启使连接关闭后仍会正常重连。断线期间已结束的短连接可能因为上游没有历史回放而丢失。

### Rule-Bot 投递配置

`rule_bot.enabled=true` 时需要配置：

- `endpoint`：Rule-Bot 给出的完整 URL，必须包含其随机隐藏路径，不允许 URL 查询参数、片段或内嵌用户名密码。
- `token_file`、`token_env`、`token`：三选一。推荐 `token_file`，文件中只放 Token 本身；发送器每次重试都会重新读取文件，因此可以原地轮换 Token。
- `state_file`：可靠投递游标；省略时默认为 `<output>.rulebot-state.json`，不可与 `output` 相同。
- `send_existing`：首次启用且尚无状态文件时，是否发送输出文件中的历史域名；默认 `false`，即从文件末尾开始，只发送启用后新捕获的域名。
- `privacy.reduce_to_registrable_domain`：默认 `true`，在本地按公共后缀规则将完整主机名缩减为可注册域名；不建议关闭。若 `domain_mode=registrable_domain` 已在写盘前完成缩减，此处设为 `false` 也无法恢复原始子域。
- `privacy.exclude_suffixes`：永不发送的域名后缀数组；同时匹配该域名本身和所有子域。
- `privacy.exclude_file`：可选的本地排除文件，每行一个域名后缀，允许空行、`#` 注释和开头的点；文件会在每次处理前重新读取，修改无需重启。文件缺失、过大或含无效条目时投递会失败关闭，不会绕过排除策略。
- `proxy_url`：可选的 `http://`、`https://`、`socks5://` 或 `socks5h://` 代理地址，可包含代理认证；不要把含凭据的配置文件公开。
- `proxy_from_environment`：设为 `true` 时使用标准代理环境变量；与 `proxy_url` 互斥。两者都不配置时直接连接，入口服务方仍可看到本机网络的出口 IP。
- `retry.initial_delay/max_delay`：临时错误的指数退避范围，默认 `1s` 至 `5m`。

Rule-Bot Client 固定发送 `{"version":1,"domain":"example.com"}`，客户端不能指定 Rule-Bot 仓库、规则文件、提交标题、强制添加或来源。`.cn`、本地排除项、无效域名和重复可注册域名会在本地推进游标且不发起请求；`added`、规则/GeoSite 已存在和策略拒绝属于服务端终态并推进游标。限流、服务异常、网络错误和鉴权失败不会推进游标。`401/403` 后会持续保留当前域名并重新读取 Token 文件。

投递顺序为：主机名规范化 → 按 `domain_mode` 投影 → 投影后全局去重 → 写入本地 `output` → 缓冲刷新并 `fsync` → 本地排除与发送去重 → 发送 Rule-Bot → 收到终态 → 原子更新 `state_file`。如果 Rule-Bot 已提交但 Rule-Bot Client 在保存游标前退出，重启后可能重发；Rule-Bot 的重复检查会把它作为已存在处理，然后安全推进游标。启动时还会从已持久化游标之前的本地输出重建发送去重集合。

只检查配置和引用的密钥、TLS 文件，不启动守护进程：

```sh
rule-bot-client --config /etc/rule-bot-client/config.json --check
```

打印版本、源码提交和构建时间：

```sh
rule-bot-client --version
```

## 输出格式

输出文件是只追加的 UTF-8 文本，每行一个规范化后的 ASCII 域名。Rule-Bot Client 会转换小写、移除末尾 DNS 根点、把 Unicode IDN 转为 Punycode，并拒绝 IP 地址。`domain_mode=hostname` 保留完整主机名；`domain_mode=registrable_domain` 使用内置 Public Suffix List 在本地计算 eTLD+1，绝不按“最后两段”截取，因此 `service.example.co.uk` 会得到 `example.co.uk`。PSL 的 PRIVATE 区段也参与计算，避免把 `alice.github.io` 与 `bob.github.io` 等不同租户错误合并；不会发起远程查询。公共后缀本身、私有后缀本身、`localhost`、无可注册域名和无效输入会被跳过。去重发生在该投影之后。

更改 `domain_mode` 不会改写已有的只追加输出。若要让文件只包含新模式的结果，请保留旧文件作受限备份，并使用新的空输出文件和新的 Rule-Bot 状态文件；保持 `send_existing=false` 可避免历史记录被批量重发。

程序启动时会加载已有条目，因此重启后不会再次写入同一个域名。Linux 上会独占锁定输出文件，防止多个进程同时写入。

## 完整性边界

外部控制器日志 API 是一个尽力而为的实时流，不提供序号、历史回放或持久化队列。在极端负载或连接中断期间，上游可能丢失事件。

Rule-Bot Client 会控制自身性能开销，并在重新连接后读取一次当前连接快照，以补充仍然存活的连接；但如果上游不提供可靠队列，它无法保证审计级别的绝对零丢失。

实现原理和可靠性约定参见 [DESIGN.md](DESIGN.md)。

## 许可证

[GNU General Public License v3.0](LICENSE)

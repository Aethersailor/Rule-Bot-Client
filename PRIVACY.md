# 隐私边界

Rule-Bot Client 会观察 Mihomo / Clash 控制器提供的连接日志，因此本地输出文件本身可以反映设备或网络访问过的域名。是否把其中任何域名发送给 Rule-Bot 完全由用户控制；`rule_bot` 默认关闭。推荐显式设置 `domain_mode=registrable_domain`；省略该字段时保存完整主机名。

## 默认外发最小化

启用 Rule-Bot 投递时，Rule-Bot Client 默认在本地完成以下处理后才建立网络请求：

- 使用公共后缀规则把完整子域名缩减为可注册域名，例如 `img.account.example.com` 只发送 `example.com`；
- 同一可注册域名仅发送一次，多个子域不会形成访问次数；
- `.cn` 域名不会发送；
- `privacy.exclude_suffixes` 和 `privacy.exclude_file` 中的域名及其子域不会发送；
- 只发送 `{"version":1,"domain":"example.com"}`，不发送 URL 路径、查询参数、网页内容、来源应用或时间戳。

`domain_mode=registrable_domain` 时，本地 `output` 也只保存聚合后的可注册域名；处理使用程序内置 PSL（包含 PRIVATE 区段），全程不发起远程查询。`domain_mode=hostname` 则保留完整主机名供原有调试或审计用途，隐私暴露更大。两种输出都应位于权限受控的持久化目录中，不应公开共享。

仅当 `domain_mode=hostname` 保留了完整主机名时，才可将 `privacy.reduce_to_registrable_domain` 显式设为 `false` 发送完整主机名；这会显著增加可识别性，不建议社区用户启用。若写盘前已经聚合，则发送器无法恢复原始子域。

## 出口 IP

不配置代理时，Rule-Bot、Cloudflare 或其他入口服务提供方看到的是 Rule-Bot Client 所在网络的出口 IP。Rule-Bot Client 支持两种互斥方式：

- `proxy_url`：显式配置 `http`、`https`、`socks5` 或 `socks5h` 代理；
- `proxy_from_environment`：遵循标准代理环境变量。

使用代理后，入口通常只能看到代理出口 IP，但代理提供方能够观察连接目标与时间。Rule-Bot Client 不承诺匿名性；用户需要自行选择可信代理并保护代理凭据。

## OpenWrt 更新检查

OpenWrt 用户手工检查更新，或显式启用自动更新后，设备会访问本项目的 GitHub Releases。GitHub 及其下载服务能够看到设备的出口 IP；下载的软件包名称还会反映包管理器和设备架构。更新请求不包含 Mihomo 地址、访问密钥、Rule-Bot Token、域名清单或设备名称。自动更新默认关闭。

## 本地日志与状态

Rule-Bot 投递日志不写入原始域名，只记录使用进程启动时随机密钥计算的 12 位短引用。引用在同一次运行中可用于排查重试，重启后会变化。Docker Compose 示例限制日志为两个 1 MiB 文件。

可靠投递状态只记录输出文件游标，不记录域名。为了避免升级或重启后重复外发，启动时会读取游标之前的本地输出并只在内存中重建已发送可注册域名集合。

## 用户选择

不希望外发时，请保持 `rule_bot.enabled=false` 或删除整个 `rule_bot` 配置。希望进一步减少信息时，可以：

- 在排除文件中逐行添加不希望发送的域名后缀；
- 配置可信代理；
- 定期审查和保护本地输出文件；
- 在 Rule-Bot 私聊中吊销社区 Token 或撤回隐私同意。

成功添加的域名会写入 Rule-Bot 服务方配置的目标 GitHub 规则仓库及提交历史。目标仓库公开时，域名也会公开；私有仓库是否公开取决于仓库权限。使用项目公共入口时，目标仓库是公开的 Custom_OpenClash_Rules。启用发送前，请同时阅读 [Rule-Bot 服务端隐私说明](https://github.com/Aethersailor/Rule-Bot/blob/master/PRIVACY.md)。

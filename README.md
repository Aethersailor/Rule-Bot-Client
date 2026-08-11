<div align="center">

<h1>Rule-Bot Client</h1>

<p><strong>发现 Mihomo 最终规则命中的域名，把值得检查的目标整理成清单。</strong></p>

<p>本地保存优先 · 可选接入 Rule-Bot · 一个客户端可监听多个 Mihomo</p>

<p>
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/releases/latest"><img alt="最新版本" src="https://img.shields.io/github/v/release/Aethersailor/Rule-Bot-Client?display_name=tag&amp;sort=semver&amp;style=flat-square"></a>
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/actions/workflows/ci.yml"><img alt="CI 状态" src="https://img.shields.io/github/actions/workflow/status/Aethersailor/Rule-Bot-Client/ci.yml?branch=master&amp;label=CI&amp;style=flat-square"></a>
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/actions/workflows/openwrt-packages.yml"><img alt="OpenWrt 构建" src="https://img.shields.io/github/actions/workflow/status/Aethersailor/Rule-Bot-Client/openwrt-packages.yml?branch=master&amp;label=OpenWrt&amp;style=flat-square"></a>
  <a href="LICENSE"><img alt="许可证" src="https://img.shields.io/github/license/Aethersailor/Rule-Bot-Client?style=flat-square"></a>
</p>

<p>
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/releases/latest">⬇️ 下载</a> ·
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/wiki">📖 用户 Wiki</a> ·
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/issues">💬 问题反馈</a>
</p>

</div>

---

Rule-Bot Client 用来发现可能需要补充规则的域名。它读取 Mihomo 的连接信息，找出最终由兜底规则 `MATCH` 处理的域名，并保存为本地清单。需要时，还可以把域名发送给 [Rule-Bot](https://github.com/Aethersailor/Rule-Bot) 做进一步检查。

> [!TIP]
> **第一次使用？** 先了解下面四个概念，再根据设备选择一种部署方式。安装后先验证本地收集，确认正常后再决定是否启用 Rule-Bot。

<a id="先了解几个关键概念"></a>
## 🧭 先了解几个关键概念

- **Mihomo**：实际处理代理连接和规则匹配的内核。OpenClash、Nikki 等工具可以管理 Mihomo 内核。
- **`MATCH`**：Mihomo 规则列表末尾的兜底规则。连接没有命中前面的规则时，才会交给 `MATCH` 处理。命中 `MATCH` 不一定代表规则错误，但这些域名通常值得检查。
- **Mihomo 控制接口**：Mihomo 自带的 HTTP 管理接口，配置项通常叫 `external-controller`。管理面板和其他工具通过它读取 Mihomo 的运行状态。Rule-Bot Client 也通过这个接口读取日志和当前连接；它不是需要另行安装的程序。英文资料和部分界面也把它称为 Controller。
- **Rule-Bot**：可选的配套服务。它会检查域名是否已有规则、是否已被 GeoSite 域名库覆盖，以及是否符合服务端策略。本地收集不需要 Rule-Bot。

<a id="它能做什么"></a>
## ✨ 它能做什么

- 把最终由 `MATCH` 处理的域名整理成去重后的本地清单。
- 一个客户端同时连接多个 Mihomo 控制接口，采集多个正在运行的 Mihomo 内核，包括由 OpenClash 或 Nikki 管理的内核。
- 可选把域名发送给 Rule-Bot，由服务端决定是否补充直连规则。

Rule-Bot Client 不是代理客户端，也不会修改 Mihomo 配置。它不会记录 URL 路径、查询参数、网页内容或应用名称，也不能代替完整的流量审计工具。

<a id="它怎样工作"></a>
## 🔄 它怎样工作

| ① 数据来源 | ② 客户端处理 | ③ 默认结果 |
|:---:|:---:|:---:|
| Mihomo 日志和当前连接 | Rule-Bot Client 筛选最终由 `MATCH` 处理的域名 | 保存到本地域名清单 |

> [!NOTE]
> 启用可选的 Rule-Bot 后，客户端还会把域名发送给 Rule-Bot 检查；通过检查的域名才会进入 GitHub 规则仓库。

Rule-Bot 发送功能默认关闭。只使用本地收集时，域名不会发送给 Rule-Bot。

<a id="安装前需要确认"></a>
## ✅ 安装前需要确认

- 已有正在运行的 Mihomo 内核。使用 OpenClash 或 Nikki 时，需要确认它们当前使用的是 Mihomo。
- 安装 Rule-Bot Client 的设备能够访问 Mihomo 控制接口。控制接口设置了密钥时，还需要准备对应密钥。
- Mihomo 日志级别为 `info` 或更详细。日志级别只保留警告或错误时，客户端看不到用于判断 `MATCH` 的连接日志。
- 已确认有权收集设备或网络中的域名。共享网络尤其需要先了解下方的隐私边界。

OpenWrt 软件包可以自动发现本机的 OpenClash 或 Nikki。其他部署方式通常需要填写 Mihomo 控制接口的地址和密钥。

具体位置和示例见 Wiki 的[配置说明](https://github.com/Aethersailor/Rule-Bot-Client/wiki/%E9%85%8D%E7%BD%AE%E8%AF%B4%E6%98%8E)。

<a id="只选择一个部署位置"></a>
## 🎯 只选择一个部署位置

> [!IMPORTANT]
> 同一套网络通常只需部署一个 Rule-Bot Client，不需要在 OpenWrt 和 Linux 上各安装一份。只要部署位置能够访问各个 Mihomo 控制接口，一个客户端就能同时采集多个正在运行的 Mihomo 内核，并统一去重。

优先选择能够长期运行、且可以访问各个 Mihomo 控制接口的 Linux、NAS 或其他能够运行 Docker 的设备。只有 OpenWrt 是唯一合适的常驻设备时，再把客户端安装到 OpenWrt。仅当网络相互隔离，没有任何一个部署位置能够访问全部控制接口时，才需要部署多个客户端。

<a id="选择安装方式"></a>
## 🚀 选择安装方式

| 使用环境 | 推荐方式 | 说明 |
| --- | --- | --- |
| 🐳 已安装 Docker 的 Linux 或 NAS | Docker Compose | 镜像支持 `linux/amd64` 和 `linux/arm64` |
| 📦 Debian 或 Ubuntu | `.deb` 软件包 | 支持 `amd64`、`arm64` 和 `armhf`，作为系统服务运行 |
| 🧰 不使用 Docker 的 Linux | 原生二进制 | 提供 AMD64、386、ARM、MIPS、MIPS64、RISC-V 等构建，需要自行管理服务 |
| 📡 只有 OpenWrt 常驻设备 | LuCI 软件包 | 支持 OpenWrt 24.10 和 25.12 的四种常见架构，通过 OpenWrt Web 界面配置 |

当前没有 Windows 或 macOS 正式构建。OpenWrt 必须使用专用的 IPK 或 APK 软件包，不要安装通用 Linux 压缩包。

### 推荐：Linux、NAS 或 Docker

Debian 软件包、Docker Compose 和原生二进制的完整步骤见 [Linux 部署方式](https://github.com/Aethersailor/Rule-Bot-Client/wiki/Linux-%E4%B8%8E-Docker)。

Linux 版本没有 Web 管理页面，需要编辑 JSON 文本配置文件，并通过日志和输出文件确认状态。

### 只有 OpenWrt 设备时

> [!WARNING]
> OpenWrt 包由本项目通过 GitHub Releases 发布，不属于 OpenWrt 官方软件源。固件升级不能保证保留或自动重装该包。
>
> 如果新固件没有包含它，Rule-Bot Client 程序、后台服务和 LuCI 管理页面会消失。选择保留设置且保留清单完整时，配置和默认持久化数据可以继续保留；软件包丢失后仍需重新安装。详见 [OpenWrt 使用指南](https://github.com/Aethersailor/Rule-Bot-Client/wiki/OpenWrt-%E4%BD%BF%E7%94%A8%E6%8C%87%E5%8D%97#%E5%A4%87%E4%BB%BD%E4%B8%8E%E5%9B%BA%E4%BB%B6%E5%8D%87%E7%BA%A7)。

软件包安装后会启用后台服务并尝试启动。默认设置会自动发现 OpenClash，并且只在本地保存域名；Rule-Bot 发送功能默认关闭。运行安装命令前，需要确认允许在这台路由器上收集域名。

在 OpenWrt 的 SSH 终端中运行：

```sh
wget -O /tmp/install-rule-bot-client-openwrt.sh \
  https://github.com/Aethersailor/Rule-Bot-Client/releases/latest/download/install-rule-bot-client-openwrt.sh
cat /tmp/install-rule-bot-client-openwrt.sh
sh /tmp/install-rule-bot-client-openwrt.sh
```

脚本会识别 OpenWrt 版本、软件包格式和设备架构，并在安装前校验下载文件。

安装完成后，打开「服务 → Rule-Bot Client」。首次连接和验证步骤见 [OpenWrt 使用指南](https://github.com/Aethersailor/Rule-Bot-Client/wiki/OpenWrt-%E4%BD%BF%E7%94%A8%E6%8C%87%E5%8D%97)。

<a id="怎样确认已经正常工作"></a>
## 🔎 怎样确认已经正常工作

安装成功不代表已经连接 Mihomo。按下面的顺序检查：

| 检查内容 | 正常表现 |
| --- | --- |
| 🟢 后台服务 | OpenWrt「概览」显示正在运行，或 Linux、Docker 显示进程正在运行 |
| 🔗 Mihomo 控制接口 | OpenWrt「测试连接」成功，或 Linux 日志出现 `instance=... connected` |
| 📝 本地收集 | 访问一个此前未收集、最终由 `MATCH` 处理的域名后，「本地结果」或输出文件出现新行 |
| 🤖 Rule-Bot 处理 | 启用后，日志显示已添加、规则已存在、GeoSite 已覆盖或策略拒绝等最终结果 |

本地输出默认每 5 秒刷新一次。`--check` 只检查 JSON 格式、凭据和 HTTPS 证书文件，不会连接 Mihomo。因此，出现 `configuration is valid` 只代表配置可以读取，不代表 Mihomo 已经连通。

如果不确定某次连接是否由 `MATCH` 处理，可以先在 Mihomo 管理面板、OpenClash 日志或连接详情中查看命中的规则。所有连接都命中前面的其他规则时，没有新增域名是正常现象。

<a id="隐私与使用边界"></a>
## 🔐 隐私与使用边界

> [!CAUTION]
> 域名清单可能反映个人、家庭或组织网络的访问行为。本地清单不会自动过期。成功添加的域名会进入目标仓库的提交历史；目标仓库公开时，域名也会公开。之后吊销访问令牌或关闭客户端，无法撤回已有提交。

- 本地域名清单每行只包含一个域名，不为每个域名记录访问次数或时间戳；运行日志和可选状态文件仍会包含运维时间与计数。
- 默认也会收集连接失败、但最终由 `MATCH` 处理的域名；不需要时可以在配置中关闭。
- OpenWrt 和随附示例默认只保存可注册部分。例如，`service.example.co.uk` 保存为 `example.co.uk`，从而减少完整子域带来的识别风险。
- Mihomo 的实时日志没有历史队列。客户端断线期间已经结束的短连接可能无法补回，因此本项目不保证审计级的零丢失。
- 使用公网 Rule-Bot 提交地址时必须使用 HTTPS。不要把访问令牌、Mihomo 控制接口密钥或完整配置发布到 Issue、聊天记录或公开仓库。

启用 Rule-Bot 前，请阅读完整的 [客户端隐私说明](PRIVACY.md) 和对应服务方的隐私说明。

<a id="可选发送给-rule-bot"></a>
## 🤖 可选：发送给 Rule-Bot

本地收集正常后，可以填写服务方提供的 Rule-Bot 提交地址和访问令牌（Token）：

- 私用地址由 Rule-Bot 部署者提供。
- 社区用户需要私聊对应的 Rule-Bot，进入「Rule-Bot Client 接入」，阅读并同意隐私说明，通过群成员验证后领取个人访问令牌。
- 群聊只用于社区资格验证。客户端不会读取群消息，也不会把处理结果发送到群内。
- 首次启用且没有发送进度文件时，默认只发送之后新增的域名，不发送本地清单中的历史内容。已有发送进度文件时，重新启用会从原进度继续。

具体字段见 Wiki 的[配置说明](https://github.com/Aethersailor/Rule-Bot-Client/wiki/%E9%85%8D%E7%BD%AE%E8%AF%B4%E6%98%8E)。

自行部署 Rule-Bot 时，另见 [Rule-Bot Client 接入说明](https://github.com/Aethersailor/Rule-Bot/wiki/Rule-Bot-Client-%E6%8E%A5%E5%85%A5)。

<a id="用户文档"></a>
## 📚 用户文档

| 文档 | 内容 |
| --- | --- |
| 🐧 [Linux 部署方式](https://github.com/Aethersailor/Rule-Bot-Client/wiki/Linux-%E4%B8%8E-Docker) | 根据系统选择 Debian 软件包、Docker Compose 或原生二进制 |
| 📡 [OpenWrt 使用指南](https://github.com/Aethersailor/Rule-Bot-Client/wiki/OpenWrt-%E4%BD%BF%E7%94%A8%E6%8C%87%E5%8D%97) | 安装、首次配置、升级、备份和卸载 |
| ⚙️ [配置说明](https://github.com/Aethersailor/Rule-Bot-Client/wiki/%E9%85%8D%E7%BD%AE%E8%AF%B4%E6%98%8E) | Mihomo 控制接口、域名保存方式、Rule-Bot、HTTPS 和代理设置 |
| 🩺 [故障排查](https://github.com/Aethersailor/Rule-Bot-Client/wiki/%E6%95%85%E9%9A%9C%E6%8E%92%E6%9F%A5) | 无法启动、无法连接、没有结果和 Rule-Bot 发送失败 |
| 🔐 [隐私说明](PRIVACY.md) | 本地保存、发送到外部的数据、出口 IP 和用户控制 |
| 🛡️ [安全策略](SECURITY.md) | 私密报告安全问题，以及公开反馈前需要移除的信息 |

<a id="反馈与许可"></a>
## 💬 反馈与许可

使用问题或功能建议可以提交到 [GitHub Issues](https://github.com/Aethersailor/Rule-Bot-Client/issues)。公开反馈前，请移除访问令牌、Mihomo 控制接口密钥、私有地址和真实域名清单。

本项目采用 [GNU General Public License v3.0](LICENSE)。

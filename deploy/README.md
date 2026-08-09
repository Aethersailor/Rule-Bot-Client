# 部署资产

本目录保存与源码版本配套的部署配置，不再重复用户安装步骤。普通用户请直接阅读：

- [Linux 与 Docker](https://github.com/Aethersailor/Rule-Bot-Client/wiki/Linux-%E4%B8%8E-Docker)
- [OpenWrt 使用指南](https://github.com/Aethersailor/Rule-Bot-Client/wiki/OpenWrt-%E4%BD%BF%E7%94%A8%E6%8C%87%E5%8D%97)

| 路径 | 用途 |
| --- | --- |
| [`docker/config.json`](docker/config.json) | 与顶层 [`compose.yaml`](../compose.yaml) 配套的容器配置示例 |
| [`systemd/config.json`](systemd/config.json) | Debian / systemd 配置示例 |
| [`systemd/rule-bot-client.service`](systemd/rule-bot-client.service) | Debian 软件包使用的 systemd 单元 |

这些文件跟随当前分支演进。部署正式版本时，应使用对应 Release 标签下的同版本文件，不要混用 `master` 配置与旧版程序或镜像。

OpenWrt 的 LuCI、procd 和 UCI 打包源码位于 [`openwrt/package/luci-app-rule-bot-client`](../openwrt/package/luci-app-rule-bot-client)，正式用户只需安装 Release 中与系统系列和架构匹配的单包。

# 部署资产

本目录保存与源码版本配套的部署文件，主要供软件包、容器和系统服务使用。这里只列出文件用途，不提供完整安装步骤。

如果只是安装 Rule-Bot Client，不要单独复制本目录中的某个文件开始部署。请按对应用户指南操作：

- [Linux 部署方式](https://github.com/Aethersailor/Rule-Bot-Client/wiki/Linux-%E4%B8%8E-Docker)
- [OpenWrt 使用指南](https://github.com/Aethersailor/Rule-Bot-Client/wiki/OpenWrt-%E4%BD%BF%E7%94%A8%E6%8C%87%E5%8D%97)

| 路径 | 用途 |
| --- | --- |
| [`docker/config.json`](docker/config.json) | 与顶层 [`compose.yaml`](../compose.yaml) 配套的容器配置示例 |
| [`systemd/config.json`](systemd/config.json) | Debian 或其他 systemd 系统的配置示例 |
| [`systemd/rule-bot-client.service`](systemd/rule-bot-client.service) | 让 Rule-Bot Client 作为 systemd 后台服务运行的配置 |

这些文件跟随当前分支演进。部署正式版本时，应使用对应正式版本标签下的文件，不要混用 `master` 配置与旧版程序或镜像。

用于构建 OpenWrt 软件包的源码位于 [`openwrt/package/luci-app-rule-bot-client`](../openwrt/package/luci-app-rule-bot-client)。普通用户不需要使用这些源码，只需安装正式版本页面中与 OpenWrt 系列和设备架构匹配的软件包。

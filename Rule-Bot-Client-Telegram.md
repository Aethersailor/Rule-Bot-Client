📢 Rule-Bot Client：自动整理 Mihomo `MATCH` 域名

Rule-Bot Client 连接一个或多个 Mihomo 控制接口，筛选最终由 `MATCH` 处理的域名，并把去重结果保存到本地清单。它不会修改 Mihomo 配置，也不是代理客户端。

默认情况下，域名只保存在本地。需要时，可以单独启用 Rule-Bot 投递：

`命中 MATCH` → `本地规范化与去重` → `提交至 Rule-Bot` → `服务端检查规则、GeoSite 和网络归属`

Rule-Bot 可能返回已存在、GeoSite 已覆盖、无效或策略拒绝。只有通过检查的域名才会写入目标 GitHub 规则仓库；规则何时生效取决于实际使用的规则源及其更新周期。

支持的部署方式：

- Linux 原生二进制和 Debian 软件包；
- Docker Compose；
- OpenWrt IPK/APK 和 LuCI 管理页面。

一个客户端可以连接多个 Mihomo 实例。Rule-Bot 投递默认关闭，首次启用默认不发送已有历史清单。

⚠️ Rule-Bot Client 会观察设备或网络访问过的域名。共享网络使用前，应确认有权收集这些数据。成功添加的域名会写入服务方配置的 GitHub 仓库及提交历史；使用项目公共 Rule-Bot 时，这些内容会公开。

正式发布流程包含 Go 测试、race 检查、容器烟测、多架构构建和 OpenWrt 软件包检查。当前版本、验证结果和已知限制以 GitHub Release 与 Actions 为准。

GitHub：
https://github.com/Aethersailor/Rule-Bot-Client

用户文档：
https://github.com/Aethersailor/Rule-Bot-Client/wiki

公共 Rule-Bot 接入流程：
https://github.com/Aethersailor/Rule-Bot-Client/wiki/接入公共-Rule-Bot

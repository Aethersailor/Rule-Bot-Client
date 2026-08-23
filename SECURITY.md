# 安全策略

## 支持的版本

只有最新发布的 Rule-Bot Client 稳定版本会获得安全修复。默认分支可能包含尚未发布的变更，不能替代正式 Release。

## 私密报告安全问题

发现可能跨越安全或隐私边界的问题时，请使用 GitHub 的[私密漏洞报告表单](https://github.com/Aethersailor/Rule-Bot-Client/security/advisories/new)。不要为疑似漏洞创建公开 Issue。

报告中请提供：

- 受影响版本和部署方式；
- 可被访问的入口；
- 预期的安全边界；
- 可以公开的最小复现步骤。

不要提交 Mihomo 控制接口密钥、Rule-Bot Token、代理凭据、私有入口、域名输出、私有网络地址或完整配置文件。

不涉及安全或隐私边界的使用问题和普通缺陷，可以在移除敏感信息后提交到 [GitHub Issues](https://github.com/Aethersailor/Rule-Bot-Client/issues)。

## 安全边界

Rule-Bot Client 会拒绝 HTTP 重定向，控制接口流量不会读取环境代理变量，程序也不监听网络端口。这些措施可以降低凭据泄漏和攻击面风险，但不能为未加密的远程控制接口提供保密性。跨越不可信网络访问 Mihomo 控制接口时，请使用 HTTPS 或受保护的专用网络。

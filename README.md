<div align="center">

<h1>Rule-Bot Client</h1>

<p><strong>Find domains from connections that reach Mihomo's fallback rule and build a list of candidates for review.</strong></p>

<p>Local storage by default · Optional Rule-Bot integration · One client can monitor multiple Mihomo instances</p>

<p>English · <a href="README-zh-CN.md">简体中文</a></p>

<p>
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/Aethersailor/Rule-Bot-Client?display_name=tag&amp;sort=semver&amp;style=flat-square"></a>
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/actions/workflows/release.yml"><img alt="Release build status" src="https://img.shields.io/github/actions/workflow/status/Aethersailor/Rule-Bot-Client/release.yml?event=push&amp;label=Release&amp;style=flat-square"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/Aethersailor/Rule-Bot-Client?style=flat-square"></a>
</p>

<p>
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/releases/latest">⬇️ Download</a> ·
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/wiki">📖 User Wiki</a> ·
  <a href="https://github.com/Aethersailor/Rule-Bot-Client/issues">💬 Report an Issue</a>
</p>

</div>

---

> [!WARNING]
> This software is intended solely for study and research outside the People's Republic of China.
>
> **Organizations and individuals are prohibited from accessing, downloading, deploying, or using any part of this project in mainland China. If you are in mainland China, close this page immediately.**
>
> Organizations and individuals must not use any part of this project for activities that violate applicable laws or regulations. Users are individually responsible for the legal consequences of their use.

Rule-Bot Client finds domains that may need additional rules. It reads Mihomo connection information, identifies domains from connections ultimately handled by the fallback rule `MATCH`, and saves them to a local list. It can also send domains to [Rule-Bot](https://github.com/Aethersailor/Rule-Bot) for further checks when needed.

> [!IMPORTANT]
> **Project scope**
>
> This project provides no proxy servers, proxy nodes, or node subscriptions for users. It operates no commercial or other proxy services and provides no internet access services. Neither the project nor its maintainers partner with, represent, or act as guarantors for such providers.

> [!TIP]
> **First time here?** Start with the four concepts below, then choose a deployment method for your device. After installation, verify local collection before deciding whether to enable Rule-Bot integration.

<a id="key-concepts"></a>
## 🧭 Key Concepts

- **Mihomo** is the core that handles proxy connections and matches them against rules. Tools such as OpenClash and Nikki can manage the Mihomo core.
- **`MATCH`** is the fallback rule at the end of Mihomo's rule list. If no earlier rule matches, Mihomo uses `MATCH` to handle the connection. A `MATCH` result can be expected behavior, though the domains are often worth reviewing.
- **Mihomo controller** is the HTTP management interface built into Mihomo, typically configured via `external-controller`. Dashboards and other tools use it to read Mihomo's runtime status. Rule-Bot Client uses the same interface to read logs and active connections. It comes with Mihomo and requires no separate installation. English documentation and some interfaces call it the Controller.
- **Rule-Bot** is an optional companion service. It checks whether a domain is already covered by an existing rule or the GeoSite domain database and whether it meets the server's policy. Local collection works without Rule-Bot.

<a id="features"></a>
## ✨ Features

- Keep a deduplicated local list of domains from connections ultimately handled by `MATCH`.
- Connect to multiple Mihomo controllers at once and collect domains from multiple running Mihomo cores, including cores managed by OpenClash or Nikki.
- Optionally send domains to Rule-Bot so the server can decide whether to add direct-routing rules.

Rule-Bot Client does not proxy traffic or modify the Mihomo configuration. It does not record URL paths, query parameters, page contents, or application names, and it cannot substitute for a full traffic auditing tool.

<a id="how-it-works"></a>
## 🔄 How It Works

| ① Data source | ② Client processing | ③ Default result |
|:---:|:---:|:---:|
| Mihomo logs and active connections | Rule-Bot Client selects domains from connections ultimately handled by `MATCH` | Domains are saved to the local list |

> [!NOTE]
> When Rule-Bot integration is enabled, the client also sends domains to Rule-Bot for review. Only domains that pass the checks are added to the GitHub rules repository.

Sending to Rule-Bot is disabled by default. With local collection only, the client never sends domains to Rule-Bot.

<a id="related-projects"></a>
## 🔗 Related Projects

Rule-Bot Client can collect domains locally on its own. The other projects apply in the following cases.

| Project | Role | Required? |
| --- | --- | --- |
| [Rule-Bot](https://github.com/Aethersailor/Rule-Bot) | Checks candidate domains and writes them to the target GitHub rules repository according to server policy | Optional for submissions. Local collection works independently |
| [Custom_OpenClash_Rules](https://github.com/Aethersailor/Custom_OpenClash_Rules) | The target rules repository for the project's public Rule-Bot instance | Relevant only when using the public submission service |
| [SubConverter-Extended](https://github.com/Aethersailor/SubConverter-Extended) | An optional subscription conversion backend that can generate configurations with Custom_OpenClash_Rules templates | Neither collection nor submission depends directly on it |

Data flows through the public service as follows.

`Mihomo connections matching MATCH` → `Rule-Bot Client collects locally` → `User enables sending` → `Rule-Bot checks domains` → `Custom_OpenClash_Rules`

<a id="prerequisites"></a>
## ✅ Prerequisites

- A running Mihomo core. If you use OpenClash or Nikki, confirm that it is currently using Mihomo.
- The device running Rule-Bot Client must be able to reach the Mihomo controller. If the controller is configured with a secret, have that secret ready.
- Mihomo's log level must be `info` or more verbose. If only warnings or errors are logged, the client cannot see the connection logs it uses to identify `MATCH` traffic.
- Confirm that you have permission to collect domains on the device or network. On shared networks in particular, read the privacy guidance below first.

The OpenWrt package can automatically discover OpenClash or Nikki on the same device. Other deployment methods typically require the Mihomo controller address and secret.

See [Configuration](https://github.com/Aethersailor/Rule-Bot-Client/wiki/%E9%85%8D%E7%BD%AE%E8%AF%B4%E6%98%8E) in the Wiki for where to find these settings and for examples.

<a id="choose-one-deployment-location"></a>
## 🎯 Choose One Deployment Location

> [!IMPORTANT]
> One Rule-Bot Client installation is usually enough for a network. Installing it on both OpenWrt and Linux is unnecessary. As long as the chosen host can reach each Mihomo controller, one client can collect domains from multiple running cores and deduplicate them together.

Prefer an always-on Linux, NAS, or other Docker-capable device that can reach every Mihomo controller. Install the client on OpenWrt only when it is the sole suitable always-on device. Multiple clients are necessary only if the networks are isolated and no single deployment location can reach all controllers.

<a id="choose-an-installation-method"></a>
## 🚀 Choose an Installation Method

| Environment | Recommended method | Details |
| --- | --- | --- |
| 🐳 Linux or NAS with Docker installed | Docker Compose | Images support `linux/amd64` and `linux/arm64` |
| 📦 Debian or Ubuntu | `.deb` package | Supports `amd64`, `arm64`, and `armhf` and runs as a system service |
| 🧰 Linux without Docker | Native binary | Includes AMD64, 386, ARM, MIPS, MIPS64, and RISC-V builds. Manage the service yourself |
| 🪟 Windows 10 or 11 | Portable archive | AMD64 and ARM64 builds are available. Extract the archive and edit the configuration to run |
| 📡 OpenWrt is the only always-on device | LuCI package | Provides IPK packages for 31 OpenWrt 24.10 architectures and APK packages for 30 OpenWrt 25.12 architectures. The packages support corresponding ImmortalWrt systems and are configured through the OpenWrt web interface |

There are currently no official macOS builds. On OpenWrt, use the dedicated IPK or APK package and avoid installing a generic Linux archive.

### Recommended for Linux, NAS, or Docker

See [Linux Deployment](https://github.com/Aethersailor/Rule-Bot-Client/wiki/Linux-%E4%B8%8E-Docker) for the full instructions for Debian packages, Docker Compose, and native binaries.

The Linux version has no web administration page. Edit the JSON configuration file directly and use logs and output files to verify the client status.

### Windows Portable Build

Download the Windows archive for your device from the stable release page. Extract it, copy and edit the example configuration, and run the client. No installer is required. See [Windows Portable Build](https://github.com/Aethersailor/Rule-Bot-Client/wiki/Windows-%E4%BE%BF%E6%90%BA%E7%89%88) for the full instructions.

### When OpenWrt Is the Only Available Device

> [!WARNING]
> The project publishes OpenWrt packages through GitHub Releases. They are outside the official OpenWrt package feeds. A firmware upgrade may leave the package missing without automatically reinstalling it.
>
> If the new firmware omits the package, the Rule-Bot Client executable, background service, and LuCI administration page will be removed. The configuration and default persistent data can carry over if you retain settings and the list of files to retain is complete. Reinstall the package if it is missing. See the [OpenWrt User Guide](https://github.com/Aethersailor/Rule-Bot-Client/wiki/OpenWrt-%E4%BD%BF%E7%94%A8%E6%8C%87%E5%8D%97#%E5%A4%87%E4%BB%BD%E4%B8%8E%E5%9B%BA%E4%BB%B6%E5%8D%87%E7%BA%A7).
>
> Avoid uploading the APK directly through the LuCI package manager. The firmware's package feed signing keys do not trust third-party release APKs, so a direct upload fails with `UNTRUSTED signature`. The script below verifies the release manifest, file size, and SHA-256 first. It uses `--allow-untrusted` only for the local APK that passed those checks.

After installation, the package enables the background service and attempts to start it. By default, it discovers OpenClash automatically and stores domains locally. Sending to Rule-Bot is disabled by default. Before running the installation commands, confirm that you have permission to collect domains on this router.

Run these commands in an SSH session on OpenWrt.

```sh
wget -O /tmp/install-rule-bot-client-openwrt.sh \
  https://github.com/Aethersailor/Rule-Bot-Client/releases/latest/download/install-rule-bot-client-openwrt.sh
cat /tmp/install-rule-bot-client-openwrt.sh
sh /tmp/install-rule-bot-client-openwrt.sh
```

The script detects OpenWrt or ImmortalWrt, the package format, and the device architecture. It verifies the downloaded file before installation. On ImmortalWrt SNAPSHOT, the script uses a hash-verified OpenWrt 25.12 APK as a compatibility build. The device's `apk` still performs the final dependency checks.

After installation, open **Services → Rule-Bot Client**. See the [OpenWrt User Guide](https://github.com/Aethersailor/Rule-Bot-Client/wiki/OpenWrt-%E4%BD%BF%E7%94%A8%E6%8C%87%E5%8D%97) for the initial connection and verification steps.

<a id="verify-operation"></a>
## 🔎 Verify Operation

After installation, verify the Mihomo connection and collection in the following order.

| What to check | Expected result |
| --- | --- |
| 🟢 Background service | **Overview** on OpenWrt shows that the service is running, or Linux or Docker reports that the process is running |
| 🔗 Mihomo controller | **Test connection** succeeds on OpenWrt, or the Linux log contains `instance=... connected` |
| 📝 Local collection | After visiting a domain that has yet to be collected and is ultimately handled by `MATCH`, a new line appears under **Local results** or in the output file |
| 🤖 Rule-Bot processing | Once sending is enabled, the log shows a final outcome such as the domain being added, already present, covered by GeoSite, or rejected by policy |

Local output refreshes every 5 seconds by default. `--check` validates JSON syntax, credentials, and HTTPS certificate files without connecting to Mihomo. A `configuration is valid` result confirms that the configuration can be read. Check Mihomo connectivity separately.

If you are unsure whether `MATCH` handled a connection, check the matched rule in the Mihomo dashboard, OpenClash logs, or connection details. When every connection matches an earlier rule, it is normal for the client to collect no new domains.

<a id="privacy-and-usage-boundaries"></a>
## 🔐 Privacy and Usage Boundaries

> [!CAUTION]
> Domain lists may reveal browsing activity on personal, household, or organizational networks. Local lists never expire automatically. Successfully added domains enter the target repository's commit history and are public if the repository is public. Revoking an access token or turning off the client afterward cannot undo existing commits.

- The local list stores one domain per line, with no per-domain visit counts or timestamps. Runtime logs and optional status files still include operational timestamps and counts.
- By default, the client also collects domains from failed connections ultimately handled by `MATCH`. Disable this behavior in the configuration if you do not need it.
- The OpenWrt package and bundled examples store only the registrable domain by default. For example, `service.example.co.uk` is saved as `example.co.uk`. This reduces the identification risk of storing full subdomains.
- Mihomo's real-time logs have no history queue. The client may be unable to recover short-lived connections that ended while it was disconnected, so the project cannot guarantee lossless, audit-grade collection.
- Public Rule-Bot submission endpoints require HTTPS. Keep access tokens, Mihomo controller secrets, and full configurations out of issues, chat logs, and public repositories.

Before enabling Rule-Bot integration, read the full [Client Privacy Notice](PRIVACY.md) and the relevant service provider's privacy notice. If you use the project's public service, also read the [Rule-Bot Privacy Notice](https://github.com/Aethersailor/Rule-Bot/blob/master/PRIVACY.md).

<a id="optional-send-to-rule-bot"></a>
## 🤖 Optional Submission to Rule-Bot

Once local collection is working, you can enter the Rule-Bot submission endpoint and access token supplied by the service provider.

- For a private instance, obtain the endpoint from the Rule-Bot operator.
- For the public instance, open a private Telegram chat with [@asailor_rulebot](https://t.me/asailor_rulebot), then read and agree to the Privacy Notice before requesting a personal token. Group membership is unnecessary. The bot supplies the full submission endpoint and personal token. See [Connect to the Public Rule-Bot](https://github.com/Aethersailor/Rule-Bot-Client/wiki/%E6%8E%A5%E5%85%A5%E5%85%AC%E5%85%B1-Rule-Bot) for the steps.
- The client submits domains through the Rule-Bot API and obtains the processing result from the API response.
- If you enable sending for the first time and no submission progress file exists, the client sends only domains collected from that point onward by default. It skips existing entries in the local list. With a progress file, re-enabling sending resumes from the recorded position.

The project's public Rule-Bot instance writes domains that pass its checks to [`rule/Custom_Direct.list` in Custom_OpenClash_Rules](https://github.com/Aethersailor/Custom_OpenClash_Rules/blob/main/rule/Custom_Direct.list). Self-hosted Rule-Bot instances can use other target repositories. The Rule-Bot deployment determines the target repository and rule file.

See [Configuration](https://github.com/Aethersailor/Rule-Bot-Client/wiki/%E9%85%8D%E7%BD%AE%E8%AF%B4%E6%98%8E) in the Wiki for specific fields.

If you deploy your own Rule-Bot instance, also see [Rule-Bot Client Integration](https://github.com/Aethersailor/Rule-Bot/wiki/Rule-Bot-Client-%E6%8E%A5%E5%85%A5).

<a id="user-documentation"></a>
## 📚 User Documentation

| Document | Topics |
| --- | --- |
| 🐧 [Linux Deployment](https://github.com/Aethersailor/Rule-Bot-Client/wiki/Linux-%E4%B8%8E-Docker) | Choose a Debian package, Docker Compose, or a native binary based on your system |
| 🪟 [Windows Portable Build](https://github.com/Aethersailor/Rule-Bot-Client/wiki/Windows-%E4%BE%BF%E6%90%BA%E7%89%88) | Download, configure, run, and automatically update the Windows portable package |
| 📡 [OpenWrt User Guide](https://github.com/Aethersailor/Rule-Bot-Client/wiki/OpenWrt-%E4%BD%BF%E7%94%A8%E6%8C%87%E5%8D%97) | Installation, initial configuration, upgrades, backups, and uninstallation |
| ⚙️ [Configuration](https://github.com/Aethersailor/Rule-Bot-Client/wiki/%E9%85%8D%E7%BD%AE%E8%AF%B4%E6%98%8E) | Mihomo controllers, domain storage, Rule-Bot, HTTPS, and proxy settings |
| 🔗 [Connect to the Public Rule-Bot](https://github.com/Aethersailor/Rule-Bot-Client/wiki/%E6%8E%A5%E5%85%A5%E5%85%AC%E5%85%B1-Rule-Bot) | Request a personal token, configure the client, verify processing results, and manage credentials |
| 🩺 [Troubleshooting](https://github.com/Aethersailor/Rule-Bot-Client/wiki/%E6%95%85%E9%9A%9C%E6%8E%92%E6%9F%A5) | Startup and connection failures, missing results, and failed submissions to Rule-Bot |
| 🔐 [Privacy Notice](PRIVACY.md) | Local storage, data sent externally, egress IP addresses, and user controls |
| 🛡️ [Security Policy](SECURITY.md) | Report security issues privately and learn what to remove before posting publicly |

<a id="feedback-and-license"></a>
## 💬 Feedback and License

Report problems or suggest features in [GitHub Issues](https://github.com/Aethersailor/Rule-Bot-Client/issues). Before posting publicly, remove access tokens, Mihomo controller secrets, private addresses, and actual domain lists.

This project is licensed under the [GNU Affero General Public License v3.0](LICENSE).

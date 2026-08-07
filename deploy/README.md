# Deployment assets

## Docker Compose

Prepare the fixed `/opt` deployment directory before starting the container:

```sh
sudo install -d -m 0755 /opt/rule-bot-client
sudo install -d -o 10001 -g 10001 -m 0750 /opt/rule-bot-client/data
sudo install -o root -g 10001 -m 0640 deploy/docker/config.json /opt/rule-bot-client/config.json
sudo install -o root -g 10001 -m 0640 /path/to/controller.secret /opt/rule-bot-client/home.secret
sudo install -m 0644 compose.yaml /opt/rule-bot-client/compose.yaml
cd /opt/rule-bot-client
docker compose up -d
```

The example uses one bind mount, `/opt/rule-bot-client:/data`. Configuration and
secret files are readable by the image's `10001:10001` user, while collected
domains are written under `/opt/rule-bot-client/data`. Container logs are rotated at
1 MiB with two retained files.

The process exposes no port. The example deliberately uses host networking so
it can reach a controller bound only to host loopback without exposing that
controller on another host address.

## Debian/systemd

The release `.deb` creates the `rule-bot-client` system user, installs the binary and
unit, and preserves `/etc/rule-bot-client/config.json` as administrator-owned
configuration. For a manual tarball installation, install the files under
`deploy/systemd/`, create the service user and `/var/lib/rule-bot-client`, then run:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now rule-bot-client
```

Secret files should be owned by `root:rule-bot-client` with mode `0640`.

## OpenWrt

OpenWrt is shipped only as the architecture-specific `luci-app-rule-bot-client` IPK or APK
built by `.github/workflows/openwrt-packages.yml` with the matching official
OpenWrt SDK. The single package contains the core binary, procd service, UCI
configuration, adapter backend, authenticated LuCI/rpcd interface, initialization,
backup/restore support, and the sysupgrade keep list. Do not install the generic
Linux tarball on OpenWrt.

After package installation, open **Services → Rule-Bot Client**. The package can
discover OpenClash and Nikki and can run both alongside multiple manually
configured Mihomo controllers. Generated runtime configuration, discovered
controller secrets, and status remain under `/var/run/rule-bot-client`; durable
configuration, credentials, certificates, output, and Rule-Bot state remain
under `/etc` by default. An unavailable external `/mnt/...` storage target fails
closed instead of falling back to Overlay.

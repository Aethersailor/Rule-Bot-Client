#!/bin/sh
set -eu

if [ "$#" -ne 4 ]; then
  echo "usage: package-deb.sh <deb-arch> <binary> <version> <output-dir>" >&2
  exit 2
fi

arch=$1
binary=$2
version=${3#v}
output_dir=$4
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
chmod 0755 "$root"

install -d "$root/DEBIAN" "$root/usr/bin" "$root/lib/systemd/system" \
  "$root/etc/rule-bot-client" "$root/var/lib/rule-bot-client"
install -m 0755 "$binary" "$root/usr/bin/rule-bot-client"
install -m 0644 deploy/systemd/rule-bot-client.service "$root/lib/systemd/system/rule-bot-client.service"
install -m 0644 deploy/systemd/rule-bot-client-update.service "$root/lib/systemd/system/rule-bot-client-update.service"
install -m 0644 deploy/systemd/rule-bot-client-update.timer "$root/lib/systemd/system/rule-bot-client-update.timer"
install -m 0600 deploy/systemd/config.json "$root/etc/rule-bot-client/config.json"

cat > "$root/DEBIAN/control" <<EOF
Package: rule-bot-client
Version: $version
Section: net
Priority: optional
Architecture: $arch
Maintainer: Aethersailor
Depends: adduser, ca-certificates
Description: Lightweight final MATCH-rule domain collector
 Rule-Bot Client watches compatible external controller log streams and writes
 previously unseen final-rule domains to an append-only text file.
EOF

cat > "$root/DEBIAN/conffiles" <<'EOF'
/etc/rule-bot-client/config.json
EOF

cat > "$root/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if ! getent group rule-bot-client >/dev/null; then
  addgroup --system rule-bot-client
fi
if ! getent passwd rule-bot-client >/dev/null; then
  adduser --system --ingroup rule-bot-client --home /var/lib/rule-bot-client --no-create-home rule-bot-client
fi
chown root:rule-bot-client /etc/rule-bot-client/config.json
chmod 0640 /etc/rule-bot-client/config.json
chown rule-bot-client:rule-bot-client /var/lib/rule-bot-client
chmod 0750 /var/lib/rule-bot-client
if command -v systemctl >/dev/null; then
  systemctl daemon-reload || true
  systemctl enable --now rule-bot-client-update.timer || true
fi
exit 0
EOF
chmod 0755 "$root/DEBIAN/postinst"

cat > "$root/DEBIAN/prerm" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = remove ] && command -v systemctl >/dev/null; then
  systemctl disable --now rule-bot-client-update.timer || true
fi
exit 0
EOF
chmod 0755 "$root/DEBIAN/prerm"

mkdir -p "$output_dir"
dpkg-deb --root-owner-group --build "$root" "$output_dir/rule-bot-client_${version}_${arch}.deb"

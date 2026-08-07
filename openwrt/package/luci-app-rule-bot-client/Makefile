include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-rule-bot-client
PKG_VERSION:=$(if $(RULE_BOT_CLIENT_VERSION),$(RULE_BOT_CLIENT_VERSION),0.1.0)
PKG_RELEASE:=1
PKG_LICENSE:=GPL-3.0-only
PKG_MAINTAINER:=Aethersailor
PKG_BUILD_DEPENDS:=luci-base/host

include $(INCLUDE_DIR)/package.mk

define Package/luci-app-rule-bot-client
  SECTION:=net
  CATEGORY:=Network
  SUBMENU:=Web Servers/Proxies
  TITLE:=Multi-controller Mihomo MATCH collector with LuCI
  URL:=https://github.com/Aethersailor/Rule-Bot-Client
  DEPENDS:=+ca-bundle +rpcd +ucode +ucode-mod-fs +luci-base
endef

define Package/luci-app-rule-bot-client/description
 Rule-Bot Client collects final MATCH domains from multiple Mihomo controllers.
 This architecture-specific package contains the collector, procd service,
 OpenClash/Nikki/manual-controller adapter, and LuCI management interface.
endef

define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	cp $(CURDIR)/src/rule-bot-client $(PKG_BUILD_DIR)/rule-bot-client
	cp $(CURDIR)/src/rule-bot-client-openwrt $(PKG_BUILD_DIR)/rule-bot-client-openwrt
endef

define Build/Compile
endef

define Package/luci-app-rule-bot-client/conffiles
/etc/config/rule_bot_client
/etc/rule-bot-client/credentials/
/etc/rule-bot-client/certs/
/etc/rule-bot-client/exclude.list
/etc/rule-bot-client/data/
/etc/rule-bot-client/recover.sh
endef

define Package/luci-app-rule-bot-client/install
	$(INSTALL_DIR) $(1)/usr/bin $(1)/usr/libexec $(1)/etc/init.d $(1)/etc/config
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/rule-bot-client $(1)/usr/bin/rule-bot-client
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/rule-bot-client-openwrt $(1)/usr/libexec/rule-bot-client-openwrt
	$(INSTALL_BIN) ./files/etc/init.d/rule-bot-client $(1)/etc/init.d/rule-bot-client
	$(INSTALL_CONF) ./files/etc/config/rule_bot_client $(1)/etc/config/rule_bot_client

	$(INSTALL_DIR) $(1)/etc/rule-bot-client/credentials $(1)/etc/rule-bot-client/certs $(1)/etc/rule-bot-client/data
	$(INSTALL_CONF) ./files/etc/rule-bot-client/exclude.list $(1)/etc/rule-bot-client/exclude.list
	$(INSTALL_BIN) ./files/etc/rule-bot-client/recover.sh $(1)/etc/rule-bot-client/recover.sh

	$(INSTALL_DIR) $(1)/lib/upgrade/keep.d
	$(INSTALL_CONF) ./files/lib/upgrade/keep.d/rule-bot-client $(1)/lib/upgrade/keep.d/rule-bot-client

	$(INSTALL_DIR) $(1)/usr/share/rpcd/ucode $(1)/usr/share/rpcd/acl.d $(1)/usr/share/luci/menu.d
	$(INSTALL_BIN) ./files/usr/share/rpcd/ucode/luci.rule_bot_client $(1)/usr/share/rpcd/ucode/luci.rule_bot_client
	$(INSTALL_CONF) ./files/usr/share/rpcd/acl.d/luci-app-rule-bot-client.json $(1)/usr/share/rpcd/acl.d/luci-app-rule-bot-client.json
	$(INSTALL_CONF) ./files/usr/share/luci/menu.d/luci-app-rule-bot-client.json $(1)/usr/share/luci/menu.d/luci-app-rule-bot-client.json
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/i18n
	$(STAGING_DIR_HOSTPKG)/bin/po2lmo ./po/zh_Hans/rule_bot_client.po $(1)/usr/lib/lua/luci/i18n/rule_bot_client.zh-cn.lmo

	$(INSTALL_DIR) $(1)/www/luci-static/resources/rule_bot_client $(1)/www/luci-static/resources/view/rule_bot_client
	$(INSTALL_DATA) ./files/www/luci-static/resources/rule_bot_client/api.js $(1)/www/luci-static/resources/rule_bot_client/api.js
	$(INSTALL_DATA) ./files/www/luci-static/resources/view/rule_bot_client/*.js $(1)/www/luci-static/resources/view/rule_bot_client/
endef

define Package/luci-app-rule-bot-client/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
/usr/libexec/rule-bot-client-openwrt initialize >/dev/null 2>&1 || exit 1
/etc/init.d/rule-bot-client enable >/dev/null 2>&1 || true
/etc/init.d/rpcd restart >/dev/null 2>&1 || true
/etc/init.d/uhttpd reload >/dev/null 2>&1 || true
/etc/init.d/rule-bot-client restart >/dev/null 2>&1 || true
exit 0
endef

define Package/luci-app-rule-bot-client/prerm
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
/etc/init.d/rule-bot-client stop >/dev/null 2>&1 || true
/etc/init.d/rule-bot-client disable >/dev/null 2>&1 || true
exit 0
endef

$(eval $(call BuildPackage,luci-app-rule-bot-client))

package openwrt

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Aethersailor/Rule-Bot-Client/internal/client"
	"gopkg.in/yaml.v3"
)

type discoveryResult struct {
	Instances []client.InstanceConfig
	Adapters  map[string]AdapterStatus
}

func discoverSources(root string, settings Settings) (discoveryResult, error) {
	result := discoveryResult{Adapters: map[string]AdapterStatus{}}
	endpoints := map[string]string{}
	for _, source := range settings.Sources {
		if !source.Enabled {
			continue
		}
		var instance *client.InstanceConfig
		var adapter AdapterStatus
		var err error
		switch source.Type {
		case SourceOpenClash:
			instance, adapter, err = discoverOpenClash(root, source)
		case SourceNikki:
			instance, adapter, err = discoverNikki(root, source)
		case SourceManual:
			instance, err = manualInstance(root, source)
		default:
			err = fmt.Errorf("unsupported source type %q", source.Type)
		}
		if source.Type != SourceManual {
			result.Adapters[source.ID] = adapter
		}
		if err != nil {
			if source.Type == SourceManual {
				return discoveryResult{}, fmt.Errorf("source %q: %w", source.ID, err)
			}
			adapter.Error = err.Error()
			result.Adapters[source.ID] = adapter
			continue
		}
		if instance == nil {
			continue
		}
		key := strings.ToLower(instance.URL)
		if previous, exists := endpoints[key]; exists {
			return discoveryResult{}, fmt.Errorf("normalized controller address %q is shared by %q and %q", instance.URL, previous, source.ID)
		}
		endpoints[key] = source.ID
		result.Instances = append(result.Instances, *instance)
	}
	if settings.Enabled && len(result.Instances) == 0 {
		return result, errors.New("no enabled controller could be generated; unavailable auto adapters remain in wait state")
	}
	return result, nil
}

func discoverOpenClash(root string, source Source) (*client.InstanceConfig, AdapterStatus, error) {
	status := AdapterStatus{ID: source.ID, Type: SourceOpenClash, Source: "/etc/config/openclash"}
	path := rooted(root, status.Source)
	config, err := loadUCI(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, status, errors.New("OpenClash UCI is not installed")
		}
		return nil, status, fmt.Errorf("read OpenClash UCI: %w", err)
	}
	section := config.section("openclash", "config")
	if section == nil {
		for index := range config.Sections {
			if config.Sections[index].Name == "config" {
				section = &config.Sections[index]
				break
			}
		}
	}
	if section == nil {
		return nil, status, errors.New("OpenClash config section is missing")
	}
	port := intOption(section, "cn_port")
	if port < 1 || port > 65535 {
		return nil, status, errors.New("OpenClash controller port is invalid")
	}
	controllerURL := "http://127.0.0.1:" + strconv.Itoa(port)
	status.Available = true
	status.Running = serviceRunning(root, "openclash")
	status.URL = controllerURL
	status.HTTPPort = intOption(section, "http_port")
	status.MixedPort = intOption(section, "mixed_port")
	secret := strings.TrimSpace(section.Options["dashboard_password"])
	status.SecretSet = secret != ""
	secretFile := ""
	if secret != "" {
		secretFile = "/var/run/rule-bot-client/openclash.secret"
		if err := writeRuntimeSecret(root, secretFile, secret); err != nil {
			return nil, status, err
		}
	}
	return &client.InstanceConfig{
		Name: source.ID, URL: controllerURL, SecretFile: secretFile,
	}, status, nil
}

func discoverNikki(root string, source Source) (*client.InstanceConfig, AdapterStatus, error) {
	status := AdapterStatus{ID: source.ID, Type: SourceNikki}
	values := map[string]any{}
	runtimePath := rooted(root, "/etc/nikki/run/config.yaml")
	data, err := os.ReadFile(runtimePath)
	if err == nil {
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, status, fmt.Errorf("parse Nikki runtime config: %w", err)
		}
		status.Source = "/etc/nikki/run/config.yaml"
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, status, fmt.Errorf("read Nikki runtime config: %w", err)
	} else {
		fallback, fallbackErr := nikkiUCIFallback(root)
		if fallbackErr != nil {
			return nil, status, fallbackErr
		}
		values = fallback
		status.Source = "/etc/config/nikki (waiting for final runtime config)"
	}
	httpListen := scalarString(values["external-controller"])
	tlsListen := scalarString(values["external-controller-tls"])
	useTLS := source.PreferTLS && tlsListen != ""
	if httpListen == "" && tlsListen != "" {
		useTLS = true
	}
	listen := httpListen
	scheme := "http"
	if useTLS {
		listen = tlsListen
		scheme = "https"
	}
	if listen == "" {
		return nil, status, errors.New("Nikki has no external controller yet; waiting for runtime config")
	}
	controllerURL, err := normalizeListenURL(listen, scheme)
	if err != nil {
		return nil, status, fmt.Errorf("Nikki controller: %w", err)
	}
	status.Available = true
	status.Running = serviceRunning(root, "nikki")
	status.URL = controllerURL
	status.MixedPort = scalarInt(values["mixed-port"])
	status.HTTPPort = scalarInt(values["port"])
	secret := strings.TrimSpace(scalarString(values["secret"]))
	status.SecretSet = secret != ""
	secretFile := ""
	if secret != "" {
		secretFile = "/var/run/rule-bot-client/nikki.secret"
		if err := writeRuntimeSecret(root, secretFile, secret); err != nil {
			return nil, status, err
		}
	}
	instance := &client.InstanceConfig{
		Name: source.ID, URL: controllerURL, SecretFile: secretFile,
		TLS: client.TLSConfig{
			ServerName: source.TLSServerName, InsecureSkipVerify: source.InsecureSkipVerify,
		},
	}
	if useTLS && regularNonempty(rooted(root, sourceCAPath(source.ID))) {
		instance.TLS.CAFile = sourceCAPath(source.ID)
	}
	return instance, status, nil
}

func nikkiUCIFallback(root string) (map[string]any, error) {
	config, err := loadUCI(rooted(root, "/etc/config/nikki"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("Nikki UCI is not installed")
		}
		return nil, fmt.Errorf("read Nikki UCI: %w", err)
	}
	mixin := config.section("mixin", "mixin")
	if mixin == nil {
		mixin = config.section("mixin", "")
	}
	if mixin == nil {
		return nil, errors.New("Nikki mixin section is missing")
	}
	return map[string]any{
		"external-controller":     mixin.Options["api_listen"],
		"external-controller-tls": mixin.Options["api_tls_listen"],
		"secret":                  mixin.Options["api_secret"],
		"mixed-port":              intOption(mixin, "mixed_port"),
		"port":                    intOption(mixin, "http_port"),
	}, nil
}

func manualInstance(root string, source Source) (*client.InstanceConfig, error) {
	controllerURL, err := normalizeControllerURL(source.URL)
	if err != nil {
		return nil, err
	}
	instance := &client.InstanceConfig{
		Name: source.ID, URL: controllerURL,
		TLS: client.TLSConfig{
			ServerName: source.TLSServerName, InsecureSkipVerify: source.InsecureSkipVerify,
		},
	}
	if regularNonempty(rooted(root, sourceSecretPath(source.ID))) {
		instance.SecretFile = sourceSecretPath(source.ID)
	}
	if regularNonempty(rooted(root, sourceCAPath(source.ID))) {
		instance.TLS.CAFile = sourceCAPath(source.ID)
	}
	return instance, nil
}

func normalizeListenURL(listen, scheme string) (string, error) {
	listen = strings.TrimSpace(listen)
	if strings.Contains(listen, "://") {
		parsed, err := url.Parse(listen)
		if err != nil {
			return "", err
		}
		if parsed.Scheme != scheme {
			return "", errors.New("runtime controller scheme conflicts with selected HTTP/TLS endpoint")
		}
		return normalizeControllerURL(listen)
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", errors.New("controller must include host and port")
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	return normalizeControllerURL(scheme + "://" + net.JoinHostPort(host, port))
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	default:
		return ""
	}
}

func scalarInt(value any) int {
	parsed, _ := strconv.Atoi(scalarString(value))
	return parsed
}

func writeRuntimeSecret(root, path, value string) error {
	fullPath := rooted(root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return err
	}
	if err := atomicWrite(fullPath, []byte(value+"\n"), 0o640); err != nil {
		return fmt.Errorf("write runtime controller credential: %w", err)
	}
	if root == "" || root == "/" {
		if err := os.Chown(fullPath, 0, 65534); err != nil {
			return fmt.Errorf("secure runtime controller credential ownership: %w", err)
		}
	}
	return nil
}

func serviceRunning(root, name string) bool {
	initPath := rooted(root, "/etc/init.d/"+name)
	if root != "" && root != "/" {
		return false
	}
	if _, err := os.Stat(initPath); err != nil {
		return false
	}
	return exec.Command(initPath, "status").Run() == nil
}

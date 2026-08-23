package openwrt

import "time"

const (
	SchemaVersion       = 1
	WorkModeLocal       = "local"
	WorkModeRuleBot     = "rulebot"
	StoragePersistent   = "persistent"
	StorageTemporary    = "temporary"
	StorageExternal     = "external"
	SourceOpenClash     = "openclash"
	SourceNikki         = "nikki"
	SourceManual        = "manual"
	defaultFlush        = "5s"
	maxRPCRequestBytes  = 4 << 20
	maxBackupBytes      = 4 << 20
	maxDomainExportSize = 4 << 20
)

type Settings struct {
	SchemaVersion            int      `json:"schema_version"`
	Enabled                  bool     `json:"enabled"`
	WorkMode                 string   `json:"work_mode"`
	DomainMode               string   `json:"domain_mode"`
	FlushInterval            string   `json:"flush_interval"`
	IncludeFailedConnections bool     `json:"include_failed_connections"`
	IncludeSingleLabelHosts  bool     `json:"include_single_label_hosts"`
	AutoUpdate               bool     `json:"auto_update"`
	Storage                  Storage  `json:"storage"`
	Sources                  []Source `json:"sources"`
	RuleBot                  RuleBot  `json:"rule_bot"`
	Warnings                 []string `json:"warnings,omitempty"`
}

type Storage struct {
	Mode         string `json:"mode"`
	ExternalPath string `json:"external_path,omitempty"`
}

type Source struct {
	ID                 string `json:"id"`
	Type               string `json:"type"`
	Enabled            bool   `json:"enabled"`
	Name               string `json:"name"`
	URL                string `json:"url,omitempty"`
	Secret             string `json:"secret,omitempty"`
	SecretSet          bool   `json:"secret_set,omitempty"`
	PreserveSecret     bool   `json:"preserve_secret,omitempty"`
	ClearSecret        bool   `json:"clear_secret,omitempty"`
	CAPEM              string `json:"ca_pem,omitempty"`
	CASet              bool   `json:"ca_set,omitempty"`
	PreserveCA         bool   `json:"preserve_ca,omitempty"`
	ClearCA            bool   `json:"clear_ca,omitempty"`
	TLSServerName      string `json:"tls_server_name,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
	PreferTLS          bool   `json:"prefer_tls,omitempty"`
}

type RuleBot struct {
	Enabled             bool   `json:"enabled"`
	Endpoint            string `json:"endpoint,omitempty"`
	Token               string `json:"token,omitempty"`
	TokenSet            bool   `json:"token_set,omitempty"`
	PreserveToken       bool   `json:"preserve_token,omitempty"`
	ClearToken          bool   `json:"clear_token,omitempty"`
	SendExisting        bool   `json:"send_existing"`
	ProxyURL            string `json:"proxy_url,omitempty"`
	SensitiveRedacted   bool   `json:"sensitive_redacted,omitempty"`
	ProxyCredentialsSet bool   `json:"proxy_credentials_set,omitempty"`
}

type AdapterStatus struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Available bool   `json:"available"`
	Running   bool   `json:"running"`
	URL       string `json:"url,omitempty"`
	SecretSet bool   `json:"secret_set"`
	MixedPort int    `json:"mixed_port,omitempty"`
	HTTPPort  int    `json:"http_port,omitempty"`
	Source    string `json:"source,omitempty"`
	Error     string `json:"error,omitempty"`
}

type ServiceStatus struct {
	Version        int                      `json:"version"`
	Service        string                   `json:"service"`
	GeneratedAt    time.Time                `json:"generated_at"`
	Config         Settings                 `json:"config"`
	Adapters       map[string]AdapterStatus `json:"adapters"`
	DiscoveryError string                   `json:"discovery_error,omitempty"`
	Runtime        map[string]any           `json:"runtime,omitempty"`
	Output         map[string]any           `json:"output"`
	RuleBot        map[string]any           `json:"rule_bot"`
	Storage        map[string]any           `json:"storage"`
}

type SaveResponse struct {
	OK       bool     `json:"ok"`
	Config   Settings `json:"config"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		SchemaVersion:            SchemaVersion,
		Enabled:                  true,
		WorkMode:                 WorkModeLocal,
		DomainMode:               "registrable_domain",
		FlushInterval:            defaultFlush,
		IncludeFailedConnections: true,
		IncludeSingleLabelHosts:  false,
		AutoUpdate:               false,
		Storage:                  Storage{Mode: StoragePersistent},
		Sources: []Source{
			{ID: SourceOpenClash, Type: SourceOpenClash, Enabled: true, Name: "OpenClash"},
			{ID: SourceNikki, Type: SourceNikki, Enabled: false, Name: "Nikki"},
		},
		RuleBot: RuleBot{Enabled: false, SendExisting: false},
	}
}

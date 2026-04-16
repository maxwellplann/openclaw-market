package market

import "time"

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

type ModelProfile struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Provider     string   `json:"provider"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
}

type ProviderCatalog struct {
	Provider    string              `json:"provider"`
	DisplayName string              `json:"display_name"`
	BaseURL     string              `json:"base_url"`
	APIType     string              `json:"api_type"`
	Models      []AgentAccountModel `json:"models"`
}

type AgentAccountModel struct {
	RecordID      int64    `json:"record_id"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ContextWindow int      `json:"context_window"`
	MaxTokens     int      `json:"max_tokens"`
	Reasoning     bool     `json:"reasoning"`
	Input         []string `json:"input"`
	SortOrder     int      `json:"sort_order"`
}

type AgentAccount struct {
	ID             int64               `json:"id"`
	UserID         int64               `json:"user_id"`
	Provider       string              `json:"provider"`
	Name           string              `json:"name"`
	APIKey         string              `json:"api_key"`
	BaseURL        string              `json:"base_url"`
	APIType        string              `json:"api_type"`
	RememberAPIKey bool                `json:"remember_api_key"`
	Verified       bool                `json:"verified"`
	Remark         string              `json:"remark"`
	Models         []AgentAccountModel `json:"models"`
	CreatedAt      time.Time           `json:"created_at"`
}

type AgentModelConfig struct {
	AccountID int64    `json:"account_id"`
	Model     string   `json:"model"`
	Fallbacks []string `json:"fallbacks"`
}

type AgentSecurityConfig struct {
	AllowedOrigins []string `json:"allowed_origins"`
}

type AgentOtherConfig struct {
	AutoUpgrade    bool   `json:"auto_upgrade"`
	Timezone       string `json:"timezone"`
	Language       string `json:"language"`
	Theme          string `json:"theme"`
	SearchProvider string `json:"search_provider"`
	DefaultPrompt  string `json:"default_prompt"`
}

type AgentConfigFile struct {
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AgentSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Enabled     bool   `json:"enabled"`
}

type AgentRole struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Prompt    string    `json:"prompt"`
	Model     string    `json:"model"`
	Channels  []string  `json:"channels"`
	CreatedAt time.Time `json:"created_at"`
}

type WeixinChannelConfig struct {
	Enabled        bool              `json:"enabled"`
	Mode           string            `json:"mode"`
	AppID          string            `json:"app_id"`
	AppSecret      string            `json:"app_secret"`
	Token          string            `json:"token"`
	EncodingAESKey string            `json:"encoding_aes_key"`
	BoundChannel   string            `json:"bound_channel"`
	Plugin         AgentPluginStatus `json:"plugin"`
}

type AgentPluginStatus struct {
	Type           string    `json:"type"`
	Installed      bool      `json:"installed"`
	CurrentVersion string    `json:"current_version"`
	LatestVersion  string    `json:"latest_version"`
	Upgradable     bool      `json:"upgradable"`
	LastAction     string    `json:"last_action"`
	LastMessage    string    `json:"last_message"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type TelegramChannelConfig struct {
	Enabled    bool   `json:"enabled"`
	BotToken   string `json:"bot_token"`
	BotName    string `json:"bot_name"`
	WebhookURL string `json:"webhook_url"`
	ChatID     string `json:"chat_id"`
}

type DiscordChannelConfig struct {
	Enabled     bool   `json:"enabled"`
	BotToken    string `json:"bot_token"`
	Application string `json:"application"`
	GuildID     string `json:"guild_id"`
	WebhookURL  string `json:"webhook_url"`
}

type FeishuChannelConfig struct {
	Enabled      bool   `json:"enabled"`
	AppID        string `json:"app_id"`
	AppSecret    string `json:"app_secret"`
	EncryptKey   string `json:"encrypt_key"`
	Verification string `json:"verification"`
}

type Agent struct {
	ID                   int64                 `json:"id"`
	UserID               int64                 `json:"user_id"`
	Name                 string                `json:"name"`
	Remark               string                `json:"remark"`
	AgentType            string                `json:"agent_type"`
	Provider             string                `json:"provider"`
	Model                string                `json:"model"`
	APIType              string                `json:"api_type"`
	MaxTokens            int                   `json:"max_tokens"`
	ContextWindow        int                   `json:"context_window"`
	BaseURL              string                `json:"base_url"`
	APIKey               string                `json:"api_key"`
	Token                string                `json:"token"`
	Status               string                `json:"status"`
	Message              string                `json:"message"`
	AccountID            int64                 `json:"account_id"`
	ModelConfig          AgentModelConfig      `json:"model_config"`
	SecurityConfig       AgentSecurityConfig   `json:"security_config"`
	OtherConfig          AgentOtherConfig      `json:"other_config"`
	ConfigFile           AgentConfigFile       `json:"config_file"`
	Skills               []AgentSkill          `json:"skills"`
	Roles                []AgentRole           `json:"roles"`
	WeixinChannel        WeixinChannelConfig   `json:"weixin_channel"`
	TelegramChannel      TelegramChannelConfig `json:"telegram_channel"`
	DiscordChannel       DiscordChannelConfig  `json:"discord_channel"`
	FeishuChannel        FeishuChannelConfig   `json:"feishu_channel"`
	AppVersion           string                `json:"app_version"`
	RestartPolicy        string                `json:"restart_policy"`
	AllowPort            bool                  `json:"allow_port"`
	SpecifyIP            string                `json:"specify_ip"`
	ConfigPath           string                `json:"config_path"`
	WebUIPort            int                   `json:"web_ui_port"`
	BridgePort           int                   `json:"bridge_port"`
	AllowedOrigins       []string              `json:"allowed_origins"`
	DockerContainerID    string                `json:"docker_container_id"`
	DockerContainerName  string                `json:"docker_container_name"`
	DockerImage          string                `json:"docker_image"`
	DockerGatewayToken   string                `json:"docker_gateway_token"`
	DockerConfigDir      string                `json:"docker_config_dir"`
	DockerWorkspaceDir   string                `json:"docker_workspace_dir"`
	WebsitePrimaryDomain string                `json:"website_primary_domain"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

type ChannelBinding struct {
	ID          int64      `json:"id"`
	AgentID     int64      `json:"agent_id"`
	UserID      int64      `json:"user_id"`
	ChannelName string     `json:"channel_name"`
	ScanToken   string     `json:"scan_token"`
	QRContent   string     `json:"qr_content"`
	Status      string     `json:"status"`
	TaskOutput  string     `json:"task_output"`
	TaskError   string     `json:"task_error"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	BoundAt     *time.Time `json:"bound_at,omitempty"`
}

type AgentDashboardItem struct {
	Agent           Agent
	Account         *AgentAccount
	SelectedModel   *AgentAccountModel
	Binding         *ChannelBinding
	BindingURL      string
	FallbackSummary string
}

type DashboardStats struct {
	Providers int
	Accounts  int
	Models    int
	Agents    int
	Connected int
}

type PageLink struct {
	Number int
	URL    string
	Active bool
}

type PageInfo struct {
	Page       int
	PageSize   int
	Total      int
	TotalPages int
	PrevURL    string
	NextURL    string
	Pages      []PageLink
}

type AgentDetail struct {
	Agent   Agent
	Account *AgentAccount
	Binding *ChannelBinding
}

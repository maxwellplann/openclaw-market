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

type Agent struct {
	ID                   int64            `json:"id"`
	UserID               int64            `json:"user_id"`
	Name                 string           `json:"name"`
	Remark               string           `json:"remark"`
	AgentType            string           `json:"agent_type"`
	Provider             string           `json:"provider"`
	Model                string           `json:"model"`
	APIType              string           `json:"api_type"`
	MaxTokens            int              `json:"max_tokens"`
	ContextWindow        int              `json:"context_window"`
	BaseURL              string           `json:"base_url"`
	APIKey               string           `json:"api_key"`
	Token                string           `json:"token"`
	Status               string           `json:"status"`
	Message              string           `json:"message"`
	AccountID            int64            `json:"account_id"`
	ModelConfig          AgentModelConfig `json:"model_config"`
	ConfigPath           string           `json:"config_path"`
	WebUIPort            int              `json:"web_ui_port"`
	BridgePort           int              `json:"bridge_port"`
	AllowedOrigins       []string         `json:"allowed_origins"`
	DockerContainerID    string           `json:"docker_container_id"`
	DockerContainerName  string           `json:"docker_container_name"`
	DockerImage          string           `json:"docker_image"`
	DockerGatewayToken   string           `json:"docker_gateway_token"`
	DockerConfigDir      string           `json:"docker_config_dir"`
	DockerWorkspaceDir   string           `json:"docker_workspace_dir"`
	WebsitePrimaryDomain string           `json:"website_primary_domain"`
	CreatedAt            time.Time        `json:"created_at"`
}

type ChannelBinding struct {
	ID          int64      `json:"id"`
	AgentID     int64      `json:"agent_id"`
	UserID      int64      `json:"user_id"`
	ChannelName string     `json:"channel_name"`
	ScanToken   string     `json:"scan_token"`
	QRContent   string     `json:"qr_content"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
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

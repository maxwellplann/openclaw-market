package market

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	ErrEmailExists        = errors.New("email already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrOpenClawNotFound   = errors.New("openclaw not found")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrBindingNotFound    = errors.New("binding not found")
	ErrDockerUnavailable  = errors.New("docker unavailable")
	ErrAccountNotFound    = errors.New("account not found")
	ErrModelNotFound      = errors.New("model not found")
	ErrRoleNotFound       = errors.New("role not found")
)

type storeState struct {
	NextUserID         int64            `json:"next_user_id"`
	NextAgentID        int64            `json:"next_agent_id"`
	NextAccountID      int64            `json:"next_account_id"`
	NextAccountModelID int64            `json:"next_account_model_id"`
	NextBindingID      int64            `json:"next_binding_id"`
	NextRoleID         int64            `json:"next_role_id"`
	NextClaimID        int64            `json:"next_claim_id,omitempty"`
	Users              []User           `json:"users"`
	Models             []ModelProfile   `json:"models,omitempty"`
	Accounts           []AgentAccount   `json:"accounts"`
	Agents             []Agent          `json:"agents"`
	Bindings           []ChannelBinding `json:"bindings"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data storeState
}

func NewStore(path string) (*Store, error) {
	s := &Store{
		path: path,
		data: storeState{
			NextUserID:         1,
			NextAgentID:        1,
			NextAccountID:      1,
			NextAccountModelID: 1,
			NextBindingID:      1,
			NextRoleID:         1,
		},
	}

	if err := s.load(); err != nil {
		return nil, err
	}
	s.ensureDefaults()
	return s, nil
}

func (s *Store) ensureDefaults() {
	if s.data.NextUserID == 0 {
		s.data.NextUserID = 1
	}
	if s.data.NextAgentID == 0 {
		if s.data.NextClaimID > 0 {
			s.data.NextAgentID = s.data.NextClaimID
		} else {
			s.data.NextAgentID = 1
		}
	}
	if s.data.NextAccountID == 0 {
		s.data.NextAccountID = 1
	}
	if s.data.NextAccountModelID == 0 {
		s.data.NextAccountModelID = 1
	}
	if s.data.NextBindingID == 0 {
		s.data.NextBindingID = 1
	}
	if s.data.NextRoleID == 0 {
		s.data.NextRoleID = 1
	}
	if s.data.Accounts == nil {
		s.data.Accounts = []AgentAccount{}
	}
	if s.data.Agents == nil {
		s.data.Agents = []Agent{}
	}
	if s.data.Bindings == nil {
		s.data.Bindings = []ChannelBinding{}
	}
	for i := range s.data.Agents {
		s.data.Agents[i] = normalizeAgent(s.data.Agents[i])
	}
}

func defaultProviderCatalogs() []ProviderCatalog {
	return []ProviderCatalog{
		{
			Provider:    "openai",
			DisplayName: "OpenAI",
			BaseURL:     "https://api.openai.com/v1",
			APIType:     "responses",
			Models: []AgentAccountModel{
				{ID: "gpt-4.1", Name: "GPT-4.1", ContextWindow: 128000, MaxTokens: 32768, Input: []string{"text", "image"}, SortOrder: 1},
				{ID: "gpt-4o", Name: "GPT-4o", ContextWindow: 128000, MaxTokens: 16384, Input: []string{"text", "image"}, SortOrder: 2},
			},
		},
		{
			Provider:    "deepseek",
			DisplayName: "DeepSeek",
			BaseURL:     "https://api.deepseek.com",
			APIType:     "openai-completions",
			Models: []AgentAccountModel{
				{ID: "deepseek-chat", Name: "DeepSeek Chat", ContextWindow: 64000, MaxTokens: 8192, Input: []string{"text"}, SortOrder: 1},
				{ID: "deepseek-reasoner", Name: "DeepSeek Reasoner", ContextWindow: 64000, MaxTokens: 8192, Reasoning: true, Input: []string{"text"}, SortOrder: 2},
			},
		},
		{
			Provider:    "ollama",
			DisplayName: "Ollama",
			BaseURL:     "http://127.0.0.1:11434",
			APIType:     "openai-completions",
			Models: []AgentAccountModel{
				{ID: "qwen3:8b", Name: "Qwen3 8B", ContextWindow: 32768, MaxTokens: 4096, Input: []string{"text"}, SortOrder: 1},
				{ID: "llama3.1:8b", Name: "Llama 3.1 8B", ContextWindow: 32768, MaxTokens: 4096, Input: []string{"text"}, SortOrder: 2},
			},
		},
	}
}

func providerCatalogByKey(provider string) (ProviderCatalog, bool) {
	for _, item := range defaultProviderCatalogs() {
		if item.Provider == provider {
			return item, true
		}
	}
	return ProviderCatalog{}, false
}

func defaultSkills() []AgentSkill {
	return []AgentSkill{
		{Name: "browser", Description: "网页浏览与抓取", Source: "builtin", Enabled: true},
		{Name: "workflow", Description: "任务编排与自动执行", Source: "builtin", Enabled: true},
		{Name: "knowledge", Description: "知识库与检索增强", Source: "builtin", Enabled: false},
	}
}

func defaultAgentOtherConfig() AgentOtherConfig {
	return AgentOtherConfig{
		AutoUpgrade:    true,
		Timezone:       "Asia/Shanghai",
		Language:       "zh-CN",
		Theme:          "light",
		SearchProvider: "bing",
		DefaultPrompt:  "You are OpenClaw, a pragmatic AI agent.",
	}
}

func defaultAgentConfigFile(agent Agent) AgentConfigFile {
	content := fmt.Sprintf(`{
  "provider": %q,
  "model": %q,
  "apiType": %q,
  "baseURL": %q,
  "webUIPort": %d,
  "bridgePort": %d,
  "allowedOrigins": [%s]
}`, agent.Provider, agent.Model, agent.APIType, agent.BaseURL, agent.WebUIPort, agent.BridgePort, quotedList(agent.AllowedOrigins))
	return AgentConfigFile{Content: content, UpdatedAt: time.Now()}
}

func normalizeAgent(agent Agent) Agent {
	if agent.AppVersion == "" {
		agent.AppVersion = "2026.4.14"
	}
	if agent.AgentType == "" {
		agent.AgentType = "openclaw"
	}
	if agent.Status == "" {
		agent.Status = "running"
	}
	if agent.RestartPolicy == "" {
		agent.RestartPolicy = "unless-stopped"
	}
	if agent.SecurityConfig.AllowedOrigins == nil {
		agent.SecurityConfig.AllowedOrigins = compactStrings(agent.AllowedOrigins)
	}
	if len(agent.SecurityConfig.AllowedOrigins) == 0 {
		agent.SecurityConfig.AllowedOrigins = []string{"http://127.0.0.1"}
	}
	agent.AllowedOrigins = compactStrings(agent.SecurityConfig.AllowedOrigins)
	if agent.OtherConfig.Timezone == "" {
		agent.OtherConfig = defaultAgentOtherConfig()
	}
	if agent.Skills == nil {
		agent.Skills = defaultSkills()
	}
	if agent.Roles == nil {
		agent.Roles = []AgentRole{}
	}
	if agent.ConfigFile.Content == "" {
		agent.ConfigFile = defaultAgentConfigFile(agent)
	}
	if agent.WeixinChannel.Mode == "" {
		agent.WeixinChannel.Mode = "service"
	}
	if agent.UpdatedAt.IsZero() {
		agent.UpdatedAt = agent.CreatedAt
	}
	return agent
}

func agentEnabledChannelCount(agent Agent) int {
	count := 0
	if agent.WeixinChannel.Enabled {
		count++
	}
	if agent.TelegramChannel.Enabled {
		count++
	}
	if agent.DiscordChannel.Enabled {
		count++
	}
	if agent.FeishuChannel.Enabled {
		count++
	}
	return count
}

func (s *Store) load() error {
	if _, err := os.Stat(s.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat store: %w", err)
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read store: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}

	var state storeState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("decode store: %w", err)
	}
	s.data = state
	return nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir store dir: %w", err)
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	if err := os.WriteFile(s.path, raw, 0o600); err != nil {
		return fmt.Errorf("write store: %w", err)
	}
	return nil
}

func (s *Store) CreateUser(email, passwordHash string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := normalizeEmail(email)
	for _, user := range s.data.Users {
		if user.Email == normalized {
			return User{}, ErrEmailExists
		}
	}
	user := User{
		ID:           s.data.NextUserID,
		Email:        normalized,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}
	s.data.NextUserID++
	s.data.Users = append(s.data.Users, user)
	if err := s.saveLocked(); err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Store) GetUserByEmail(email string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	normalized := normalizeEmail(email)
	for _, user := range s.data.Users {
		if user.Email == normalized {
			return user, true
		}
	}
	return User{}, false
}

func (s *Store) GetUserByID(userID int64) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.data.Users {
		if user.ID == userID {
			return user, true
		}
	}
	return User{}, false
}

func (s *Store) Stats(userID int64) DashboardStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := DashboardStats{Providers: len(defaultProviderCatalogs())}
	for _, account := range s.data.Accounts {
		if account.UserID != userID {
			continue
		}
		stats.Accounts++
		stats.Models += len(account.Models)
	}
	for _, agent := range s.data.Agents {
		if agent.UserID != userID {
			continue
		}
		stats.Agents++
		stats.Connected += agentEnabledChannelCount(normalizeAgent(agent))
	}
	return stats
}

func (s *Store) ListProviderCatalogs() []ProviderCatalog {
	return slices.Clone(defaultProviderCatalogs())
}

func (s *Store) ListAccounts(userID int64) []AgentAccount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var items []AgentAccount
	for _, account := range s.data.Accounts {
		if account.UserID == userID {
			items = append(items, account)
		}
	}
	slices.SortFunc(items, func(a, b AgentAccount) int {
		if a.CreatedAt.Before(b.CreatedAt) {
			return -1
		}
		if a.CreatedAt.After(b.CreatedAt) {
			return 1
		}
		return 0
	})
	return items
}

func (s *Store) FilterAccounts(userID int64, query string) []AgentAccount {
	items := s.ListAccounts(userID)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return items
	}
	filtered := make([]AgentAccount, 0, len(items))
	for _, item := range items {
		searchable := strings.ToLower(strings.Join([]string{
			item.Name,
			item.Provider,
			item.Remark,
			item.BaseURL,
			item.APIType,
		}, " "))
		if strings.Contains(searchable, query) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (s *Store) CreateAccount(userID int64, provider, name, apiKey, baseURL, apiType, remark string) (AgentAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	catalog, ok := providerCatalogByKey(provider)
	if !ok {
		return AgentAccount{}, ErrAccountNotFound
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = catalog.BaseURL
	}
	if strings.TrimSpace(apiType) == "" {
		apiType = catalog.APIType
	}
	account := AgentAccount{
		ID:             s.data.NextAccountID,
		UserID:         userID,
		Provider:       provider,
		Name:           strings.TrimSpace(name),
		APIKey:         strings.TrimSpace(apiKey),
		BaseURL:        strings.TrimSpace(baseURL),
		APIType:        strings.TrimSpace(apiType),
		RememberAPIKey: true,
		Verified:       strings.TrimSpace(apiKey) != "",
		Remark:         strings.TrimSpace(remark),
		Models:         make([]AgentAccountModel, 0, len(catalog.Models)),
		CreatedAt:      time.Now(),
	}
	s.data.NextAccountID++
	for _, model := range catalog.Models {
		model.RecordID = s.data.NextAccountModelID
		s.data.NextAccountModelID++
		account.Models = append(account.Models, model)
	}
	s.data.Accounts = append(s.data.Accounts, account)
	if err := s.saveLocked(); err != nil {
		return AgentAccount{}, err
	}
	return account, nil
}

func (s *Store) CreateAccountModel(userID, accountID int64, model AgentAccountModel) (AgentAccountModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Accounts {
		if s.data.Accounts[i].ID != accountID || s.data.Accounts[i].UserID != userID {
			continue
		}
		model.RecordID = s.data.NextAccountModelID
		s.data.NextAccountModelID++
		model.ID = strings.TrimSpace(model.ID)
		model.Name = strings.TrimSpace(model.Name)
		model.Input = compactStrings(model.Input)
		s.data.Accounts[i].Models = append(s.data.Accounts[i].Models, model)
		if err := s.saveLocked(); err != nil {
			return AgentAccountModel{}, err
		}
		return model, nil
	}
	return AgentAccountModel{}, ErrAccountNotFound
}

func (s *Store) GetAccount(userID, accountID int64) (AgentAccount, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, account := range s.data.Accounts {
		if account.ID == accountID && account.UserID == userID {
			return account, true
		}
	}
	return AgentAccount{}, false
}

func (s *Store) CreateAgent(userID int64, agent Agent) (Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	agent.ID = s.data.NextAgentID
	s.data.NextAgentID++
	agent.UserID = userID
	agent.CreatedAt = time.Now()
	agent.UpdatedAt = agent.CreatedAt
	agent = normalizeAgent(agent)
	s.data.Agents = append(s.data.Agents, agent)
	if err := s.saveLocked(); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (s *Store) NextHostPort(base int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	next := base
	for _, agent := range s.data.Agents {
		if agent.WebUIPort >= next {
			next = agent.WebUIPort + 1
		}
	}
	return next
}

func (s *Store) ListDashboardAgents(userID int64) []AgentDashboardItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]AgentDashboardItem, 0)
	for _, raw := range s.data.Agents {
		if raw.UserID != userID {
			continue
		}
		agent := normalizeAgent(raw)
		item := AgentDashboardItem{Agent: agent}
		for i := range s.data.Accounts {
			if s.data.Accounts[i].ID == agent.AccountID && s.data.Accounts[i].UserID == userID {
				account := s.data.Accounts[i]
				item.Account = &account
				for _, model := range account.Models {
					if model.ID == agent.ModelConfig.Model {
						selected := model
						item.SelectedModel = &selected
						break
					}
				}
				break
			}
		}
		for i := range s.data.Bindings {
			if s.data.Bindings[i].AgentID == agent.ID {
				binding := s.data.Bindings[i]
				item.Binding = &binding
				item.BindingURL = "/bindings/" + binding.ScanToken
				break
			}
		}
		item.FallbackSummary = strings.Join(agent.ModelConfig.Fallbacks, ", ")
		items = append(items, item)
	}
	slices.SortFunc(items, func(a, b AgentDashboardItem) int {
		if a.Agent.CreatedAt.After(b.Agent.CreatedAt) {
			return -1
		}
		if a.Agent.CreatedAt.Before(b.Agent.CreatedAt) {
			return 1
		}
		return 0
	})
	return items
}

func (s *Store) FilterDashboardAgents(userID int64, query string) []AgentDashboardItem {
	items := s.ListDashboardAgents(userID)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return items
	}
	filtered := make([]AgentDashboardItem, 0, len(items))
	for _, item := range items {
		accountName := ""
		if item.Account != nil {
			accountName = item.Account.Name
		}
		searchable := strings.ToLower(strings.Join([]string{
			item.Agent.Name,
			item.Agent.Model,
			item.Agent.AgentType,
			item.Agent.DockerContainerName,
			item.Agent.WebsitePrimaryDomain,
			item.Agent.Remark,
			accountName,
			item.FallbackSummary,
		}, " "))
		if strings.Contains(searchable, query) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (s *Store) GetAgentDetail(userID, agentID int64) (AgentDetail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, raw := range s.data.Agents {
		if raw.ID != agentID || raw.UserID != userID {
			continue
		}
		detail := AgentDetail{Agent: normalizeAgent(raw)}
		for _, account := range s.data.Accounts {
			if account.ID == raw.AccountID && account.UserID == userID {
				copy := account
				detail.Account = &copy
				break
			}
		}
		for _, binding := range s.data.Bindings {
			if binding.AgentID == raw.ID && binding.UserID == userID {
				copy := binding
				detail.Binding = &copy
				break
			}
		}
		return detail, nil
	}
	return AgentDetail{}, ErrOpenClawNotFound
}

func (s *Store) UpdateAgentModelConfig(userID, agentID, accountID int64, modelID string, fallbacks []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	account, ok := s.findAccountLocked(userID, accountID)
	if !ok {
		return ErrAccountNotFound
	}
	var selected *AgentAccountModel
	for _, item := range account.Models {
		if item.ID == modelID {
			copy := item
			selected = &copy
			break
		}
	}
	if selected == nil {
		return ErrModelNotFound
	}

	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		s.data.Agents[i].AccountID = accountID
		s.data.Agents[i].Provider = account.Provider
		s.data.Agents[i].BaseURL = account.BaseURL
		s.data.Agents[i].APIType = account.APIType
		s.data.Agents[i].APIKey = account.APIKey
		s.data.Agents[i].Model = modelID
		s.data.Agents[i].MaxTokens = selected.MaxTokens
		s.data.Agents[i].ContextWindow = selected.ContextWindow
		s.data.Agents[i].ModelConfig = AgentModelConfig{
			AccountID: accountID,
			Model:     modelID,
			Fallbacks: compactUniqueStrings(fallbacks),
		}
		s.data.Agents[i].UpdatedAt = time.Now()
		s.data.Agents[i].ConfigFile = defaultAgentConfigFile(normalizeAgent(s.data.Agents[i]))
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) UpdateAgentSecurityConfig(userID, agentID int64, allowedOrigins []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		origins := compactUniqueStrings(allowedOrigins)
		if len(origins) == 0 {
			origins = []string{"http://127.0.0.1"}
		}
		s.data.Agents[i].SecurityConfig.AllowedOrigins = origins
		s.data.Agents[i].AllowedOrigins = origins
		s.data.Agents[i].UpdatedAt = time.Now()
		s.data.Agents[i].ConfigFile = defaultAgentConfigFile(normalizeAgent(s.data.Agents[i]))
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) UpdateAgentOtherConfig(userID, agentID int64, cfg AgentOtherConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		s.data.Agents[i].OtherConfig = cfg
		s.data.Agents[i].UpdatedAt = time.Now()
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) UpdateAgentConfigFile(userID, agentID int64, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		s.data.Agents[i].ConfigFile = AgentConfigFile{Content: strings.TrimSpace(content), UpdatedAt: time.Now()}
		s.data.Agents[i].UpdatedAt = time.Now()
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) UpsertAgentSkill(userID, agentID int64, skill AgentSkill) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		skill.Name = strings.TrimSpace(skill.Name)
		skill.Description = strings.TrimSpace(skill.Description)
		skill.Source = strings.TrimSpace(skill.Source)
		for j := range s.data.Agents[i].Skills {
			if s.data.Agents[i].Skills[j].Name == skill.Name {
				s.data.Agents[i].Skills[j] = skill
				s.data.Agents[i].UpdatedAt = time.Now()
				return s.saveLocked()
			}
		}
		s.data.Agents[i].Skills = append(s.data.Agents[i].Skills, skill)
		s.data.Agents[i].UpdatedAt = time.Now()
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) CreateAgentRole(userID, agentID int64, role AgentRole) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		role.ID = s.data.NextRoleID
		s.data.NextRoleID++
		role.Name = strings.TrimSpace(role.Name)
		role.Prompt = strings.TrimSpace(role.Prompt)
		role.Model = strings.TrimSpace(role.Model)
		role.Channels = compactUniqueStrings(role.Channels)
		role.CreatedAt = time.Now()
		s.data.Agents[i].Roles = append(s.data.Agents[i].Roles, role)
		s.data.Agents[i].UpdatedAt = time.Now()
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) DeleteAgentRole(userID, agentID, roleID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		for j := range s.data.Agents[i].Roles {
			if s.data.Agents[i].Roles[j].ID == roleID {
				s.data.Agents[i].Roles = append(s.data.Agents[i].Roles[:j], s.data.Agents[i].Roles[j+1:]...)
				s.data.Agents[i].UpdatedAt = time.Now()
				return s.saveLocked()
			}
		}
		return ErrRoleNotFound
	}
	return ErrOpenClawNotFound
}

func (s *Store) UpdateAgentWeixinChannel(userID, agentID int64, cfg WeixinChannelConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		cfg.Mode = strings.TrimSpace(cfg.Mode)
		if cfg.Mode == "" {
			cfg.Mode = "service"
		}
		cfg.AppID = strings.TrimSpace(cfg.AppID)
		cfg.AppSecret = strings.TrimSpace(cfg.AppSecret)
		cfg.Token = strings.TrimSpace(cfg.Token)
		cfg.EncodingAESKey = strings.TrimSpace(cfg.EncodingAESKey)
		cfg.BoundChannel = strings.TrimSpace(cfg.BoundChannel)
		s.data.Agents[i].WeixinChannel = cfg
		s.data.Agents[i].UpdatedAt = time.Now()
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) UpdateAgentTelegramChannel(userID, agentID int64, cfg TelegramChannelConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		s.data.Agents[i].TelegramChannel = TelegramChannelConfig{
			Enabled:    cfg.Enabled,
			BotToken:   strings.TrimSpace(cfg.BotToken),
			BotName:    strings.TrimSpace(cfg.BotName),
			WebhookURL: strings.TrimSpace(cfg.WebhookURL),
			ChatID:     strings.TrimSpace(cfg.ChatID),
		}
		s.data.Agents[i].UpdatedAt = time.Now()
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) UpdateAgentDiscordChannel(userID, agentID int64, cfg DiscordChannelConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		s.data.Agents[i].DiscordChannel = DiscordChannelConfig{
			Enabled:     cfg.Enabled,
			BotToken:    strings.TrimSpace(cfg.BotToken),
			Application: strings.TrimSpace(cfg.Application),
			GuildID:     strings.TrimSpace(cfg.GuildID),
			WebhookURL:  strings.TrimSpace(cfg.WebhookURL),
		}
		s.data.Agents[i].UpdatedAt = time.Now()
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) UpdateAgentFeishuChannel(userID, agentID int64, cfg FeishuChannelConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		s.data.Agents[i].FeishuChannel = FeishuChannelConfig{
			Enabled:      cfg.Enabled,
			AppID:        strings.TrimSpace(cfg.AppID),
			AppSecret:    strings.TrimSpace(cfg.AppSecret),
			EncryptKey:   strings.TrimSpace(cfg.EncryptKey),
			Verification: strings.TrimSpace(cfg.Verification),
		}
		s.data.Agents[i].UpdatedAt = time.Now()
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) UpdateAgentRemark(userID, agentID int64, remark string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		s.data.Agents[i].Remark = strings.TrimSpace(remark)
		s.data.Agents[i].UpdatedAt = time.Now()
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) UpdateAgentStatus(userID, agentID int64, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		switch action {
		case "start":
			s.data.Agents[i].Status = "running"
			s.data.Agents[i].Message = "容器已启动"
		case "stop":
			s.data.Agents[i].Status = "stopped"
			s.data.Agents[i].Message = "容器已停止"
		case "restart":
			s.data.Agents[i].Status = "running"
			s.data.Agents[i].Message = "容器已重启"
		default:
			return fmt.Errorf("invalid action: %s", action)
		}
		s.data.Agents[i].UpdatedAt = time.Now()
		return s.saveLocked()
	}
	return ErrOpenClawNotFound
}

func (s *Store) ResetAgentToken(userID, agentID int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Agents {
		if s.data.Agents[i].ID != agentID || s.data.Agents[i].UserID != userID {
			continue
		}
		token, err := randomToken(12)
		if err != nil {
			return "", err
		}
		s.data.Agents[i].Token = token
		s.data.Agents[i].DockerGatewayToken = token
		s.data.Agents[i].UpdatedAt = time.Now()
		if err := s.saveLocked(); err != nil {
			return "", err
		}
		return token, nil
	}
	return "", ErrOpenClawNotFound
}

func (s *Store) CreateBinding(userID, agentID int64, channelName string) (ChannelBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ownsAgentLocked(userID, agentID) {
		return ChannelBinding{}, ErrUnauthorized
	}
	token, err := randomToken(12)
	if err != nil {
		return ChannelBinding{}, err
	}
	binding := ChannelBinding{
		ID:          s.data.NextBindingID,
		AgentID:     agentID,
		UserID:      userID,
		ChannelName: strings.TrimSpace(channelName),
		ScanToken:   token,
		QRContent:   fmt.Sprintf("wechat://openclaw-market/connect?token=%s", token),
		Status:      "pending",
		CreatedAt:   time.Now(),
	}
	s.data.NextBindingID++
	replaced := false
	for i := range s.data.Bindings {
		if s.data.Bindings[i].AgentID == agentID {
			binding.ID = s.data.Bindings[i].ID
			s.data.Bindings[i] = binding
			replaced = true
			break
		}
	}
	if !replaced {
		s.data.Bindings = append(s.data.Bindings, binding)
	}
	for i := range s.data.Agents {
		if s.data.Agents[i].ID == agentID && s.data.Agents[i].UserID == userID {
			s.data.Agents[i].WeixinChannel.BoundChannel = binding.ChannelName
			break
		}
	}
	if err := s.saveLocked(); err != nil {
		return ChannelBinding{}, err
	}
	return binding, nil
}

func (s *Store) GetBindingByToken(userID int64, token string) (ChannelBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, binding := range s.data.Bindings {
		if binding.ScanToken == token && binding.UserID == userID {
			return binding, nil
		}
	}
	return ChannelBinding{}, ErrBindingNotFound
}

func (s *Store) CompleteBinding(userID int64, token string) (ChannelBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Bindings {
		if s.data.Bindings[i].ScanToken == token && s.data.Bindings[i].UserID == userID {
			now := time.Now()
			s.data.Bindings[i].Status = "connected"
			s.data.Bindings[i].BoundAt = &now
			for j := range s.data.Agents {
				if s.data.Agents[j].ID == s.data.Bindings[i].AgentID && s.data.Agents[j].UserID == userID {
					s.data.Agents[j].WeixinChannel.Enabled = true
					s.data.Agents[j].UpdatedAt = now
					break
				}
			}
			if err := s.saveLocked(); err != nil {
				return ChannelBinding{}, err
			}
			return s.data.Bindings[i], nil
		}
	}
	return ChannelBinding{}, ErrBindingNotFound
}

func (s *Store) findAccountLocked(userID, accountID int64) (AgentAccount, bool) {
	for _, account := range s.data.Accounts {
		if account.ID == accountID && account.UserID == userID {
			return account, true
		}
	}
	return AgentAccount{}, false
}

func (s *Store) ownsAgentLocked(userID, agentID int64) bool {
	for _, agent := range s.data.Agents {
		if agent.ID == agentID && agent.UserID == userID {
			return true
		}
	}
	return false
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func compactUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range compactStrings(values) {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func quotedList(values []string) string {
	items := make([]string, 0, len(values))
	for _, value := range compactStrings(values) {
		items = append(items, fmt.Sprintf("%q", value))
	}
	return strings.Join(items, ", ")
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rand token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

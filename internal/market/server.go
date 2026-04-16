package market

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
)

//go:embed web/templates/*.html web/static/*
var webFS embed.FS

type Server struct {
	store     *Store
	sessions  *SessionManager
	templates *template.Template
	runtime   Runtime
}

type viewData struct {
	Title             string
	CurrentUser       *User
	CurrentSection    string
	CurrentTab        string
	CurrentSubTab     string
	ProviderCatalogs  []ProviderCatalog
	Accounts          []AgentAccount
	Agents            []AgentDashboardItem
	Stats             DashboardStats
	Detail            *AgentDetail
	Message           string
	Error             string
	Binding           *ChannelBinding
	ChannelBindingURL string
	QRDataURI         string
}

func NewServer(storePath string) (*Server, error) {
	return NewServerWithRuntime(storePath, NewDockerRuntimeFromEnv())
}

func NewServerWithRuntime(storePath string, runtime Runtime) (*Server, error) {
	store, err := NewStore(storePath)
	if err != nil {
		return nil, err
	}
	sessions, err := NewSessionManager()
	if err != nil {
		return nil, err
	}
	tpl, err := template.New("").Funcs(template.FuncMap{
		"join":     strings.Join,
		"contains": strings.Contains,
	}).ParseFS(webFS, "web/templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Server{store: store, sessions: sessions, templates: tpl, runtime: runtime}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	staticFS, err := fs.Sub(webFS, "web/static")
	if err != nil {
		panic(err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/", s.handleHome)
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/dashboard", s.handleDashboardRedirect)
	mux.HandleFunc("/ai/accounts", s.handleAccountsPage)
	mux.HandleFunc("/ai/accounts/create", s.handleCreateAccount)
	mux.HandleFunc("/ai/accounts/models/create", s.handleCreateAccountModel)
	mux.HandleFunc("/ai/agents", s.handleAgentsPage)
	mux.HandleFunc("/ai/agents/create", s.handleCreateAgent)
	mux.HandleFunc("/ai/agents/", s.handleAgentRoutes)
	mux.HandleFunc("/bindings/", s.handleBindingRoutes)
	return mux
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if user, _ := s.currentUser(r); user != nil {
		http.Redirect(w, r, "/ai/agents", http.StatusSeeOther)
		return
	}
	tab := r.URL.Query().Get("tab")
	if tab != "register" {
		tab = "login"
	}
	s.render(w, "index.html", viewData{
		Title:      "OpenClaw Market",
		CurrentTab: tab,
		Message:    r.URL.Query().Get("message"),
		Error:      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/?tab=register&error=form%20parse%20failed", http.StatusSeeOther)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	if email == "" || len(password) < 6 {
		http.Redirect(w, r, "/?tab=register&error=邮箱不能为空，密码至少6位", http.StatusSeeOther)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Redirect(w, r, "/?tab=register&error=密码加密失败", http.StatusSeeOther)
		return
	}
	user, err := s.store.CreateUser(email, string(hash))
	if err != nil {
		if errors.Is(err, ErrEmailExists) {
			http.Redirect(w, r, "/?tab=register&error=该邮箱已注册", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/?tab=register&error=注册失败", http.StatusSeeOther)
		return
	}
	s.setSession(w, user.ID)
	http.Redirect(w, r, "/ai/agents?message=注册成功，已自动登录", http.StatusSeeOther)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/?tab=login&error=form%20parse%20failed", http.StatusSeeOther)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	user, ok := s.store.GetUserByEmail(email)
	if !ok || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		http.Redirect(w, r, "/?tab=login&error=邮箱或密码错误", http.StatusSeeOther)
		return
	}
	s.setSession(w, user.ID)
	http.Redirect(w, r, "/ai/agents?message=登录成功", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "openclaw_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/?message=已退出登录", http.StatusSeeOther)
}

func (s *Server) handleDashboardRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/ai/agents", http.StatusSeeOther)
}

func (s *Server) handleAgentsPage(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.render(w, "agents.html", viewData{
		Title:            "智能体",
		CurrentUser:      user,
		CurrentSection:   "agents",
		ProviderCatalogs: s.store.ListProviderCatalogs(),
		Accounts:         s.store.ListAccounts(user.ID),
		Agents:           s.store.ListDashboardAgents(user.ID),
		Stats:            s.store.Stats(user.ID),
		Message:          r.URL.Query().Get("message"),
		Error:            r.URL.Query().Get("error"),
	})
}

func (s *Server) handleAccountsPage(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.render(w, "accounts.html", viewData{
		Title:            "模型账号",
		CurrentUser:      user,
		CurrentSection:   "accounts",
		ProviderCatalogs: s.store.ListProviderCatalogs(),
		Accounts:         s.store.ListAccounts(user.ID),
		Stats:            s.store.Stats(user.ID),
		Message:          r.URL.Query().Get("message"),
		Error:            r.URL.Query().Get("error"),
	})
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/ai/accounts?error=表单解析失败", http.StatusSeeOther)
		return
	}
	provider := strings.TrimSpace(r.FormValue("provider"))
	name := strings.TrimSpace(r.FormValue("name"))
	apiKey := strings.TrimSpace(r.FormValue("api_key"))
	baseURL := strings.TrimSpace(r.FormValue("base_url"))
	apiType := strings.TrimSpace(r.FormValue("api_type"))
	remark := strings.TrimSpace(r.FormValue("remark"))
	if _, err := s.store.CreateAccount(user.ID, provider, name, apiKey, baseURL, apiType, remark); err != nil {
		http.Redirect(w, r, "/ai/accounts?error=创建模型账号失败", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/ai/accounts?message=模型账号已创建", http.StatusSeeOther)
}

func (s *Server) handleCreateAccountModel(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/ai/accounts?error=表单解析失败", http.StatusSeeOther)
		return
	}
	accountID, err := strconv.ParseInt(r.FormValue("account_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/ai/accounts?error=无效的模型账号", http.StatusSeeOther)
		return
	}
	contextWindow, _ := strconv.Atoi(r.FormValue("context_window"))
	maxTokens, _ := strconv.Atoi(r.FormValue("max_tokens"))
	model := AgentAccountModel{
		ID:            strings.TrimSpace(r.FormValue("model_id")),
		Name:          strings.TrimSpace(r.FormValue("name")),
		ContextWindow: contextWindow,
		MaxTokens:     maxTokens,
		Input:         splitFormList(r.FormValue("input_types")),
		Reasoning:     r.FormValue("reasoning") == "on",
	}
	if _, err := s.store.CreateAccountModel(user.ID, accountID, model); err != nil {
		http.Redirect(w, r, "/ai/accounts?error=新增模型失败", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/ai/accounts?message=账号模型已添加", http.StatusSeeOther)
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/ai/agents?error=表单解析失败", http.StatusSeeOther)
		return
	}

	accountID, err := strconv.ParseInt(r.FormValue("account_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/ai/agents?error=请选择模型账号", http.StatusSeeOther)
		return
	}
	account, ok := s.store.GetAccount(user.ID, accountID)
	if !ok {
		http.Redirect(w, r, "/ai/agents?error=模型账号不存在", http.StatusSeeOther)
		return
	}
	modelID := strings.TrimSpace(r.FormValue("model"))
	var selected *AgentAccountModel
	for _, item := range account.Models {
		if item.ID == modelID {
			copy := item
			selected = &copy
			break
		}
	}
	if selected == nil {
		http.Redirect(w, r, "/ai/agents?error=请选择账号下的有效模型", http.StatusSeeOther)
		return
	}
	webUIPort, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("web_ui_port")))
	if webUIPort == 0 {
		webUIPort = s.store.NextHostPort(defaultGatewayPort)
	}
	bridgePort, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("bridge_port")))
	if bridgePort == 0 {
		bridgePort = webUIPort + 1000
	}
	allowedOrigins := splitFormList(r.FormValue("allowed_origins"))
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"http://127.0.0.1"}
	}
	token := strings.TrimSpace(r.FormValue("token"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "OpenClaw Agent"
	}

	ctx, cancel := context.WithTimeout(r.Context(), defaultProvisionTimeout())
	defer cancel()
	provision, err := s.runtime.ProvisionOpenClaw(ctx, ProvisionRequest{
		UserID:         user.ID,
		Name:           name,
		AgentType:      "openclaw",
		WebUIPort:      webUIPort,
		BridgePort:     bridgePort,
		StorageDir:     "data/openclaws",
		Token:          token,
		Provider:       account.Provider,
		Model:          selected.ID,
		APIType:        account.APIType,
		BaseURL:        account.BaseURL,
		APIKey:         account.APIKey,
		MaxTokens:      selected.MaxTokens,
		ContextWindow:  selected.ContextWindow,
		AllowedOrigins: allowedOrigins,
	})
	if err != nil {
		if errors.Is(err, ErrDockerUnavailable) {
			http.Redirect(w, r, "/ai/agents?error=Docker%20不可用，请先启动%20Docker%20daemon", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/ai/agents?error="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
		return
	}

	fallbacks := splitFormList(r.FormValue("fallbacks"))
	agent := Agent{
		Name:          name,
		Remark:        strings.TrimSpace(r.FormValue("remark")),
		AgentType:     "openclaw",
		Provider:      account.Provider,
		Model:         selected.ID,
		APIType:       account.APIType,
		MaxTokens:     selected.MaxTokens,
		ContextWindow: selected.ContextWindow,
		BaseURL:       account.BaseURL,
		APIKey:        account.APIKey,
		Token:         provision.GatewayToken,
		Status:        "running",
		Message:       "容器已创建",
		AccountID:     account.ID,
		ModelConfig: AgentModelConfig{
			AccountID: account.ID,
			Model:     selected.ID,
			Fallbacks: fallbacks,
		},
		SecurityConfig: AgentSecurityConfig{
			AllowedOrigins: allowedOrigins,
		},
		AppVersion:           "2026.4.14",
		RestartPolicy:        valueOrDefault(strings.TrimSpace(r.FormValue("restart_policy")), "unless-stopped"),
		AllowPort:            r.FormValue("allow_port") == "on",
		SpecifyIP:            strings.TrimSpace(r.FormValue("specify_ip")),
		ConfigPath:           provision.ConfigPath,
		WebUIPort:            provision.WebUIPort,
		BridgePort:           provision.BridgePort,
		AllowedOrigins:       allowedOrigins,
		DockerContainerID:    provision.ContainerID,
		DockerContainerName:  provision.ContainerName,
		DockerImage:          provision.Image,
		DockerGatewayToken:   provision.GatewayToken,
		DockerConfigDir:      provision.ConfigDir,
		DockerWorkspaceDir:   provision.WorkspaceDir,
		WebsitePrimaryDomain: strings.TrimSpace(r.FormValue("website_primary_domain")),
	}
	created, err := s.store.CreateAgent(user.ID, agent)
	if err != nil {
		http.Redirect(w, r, "/ai/agents?error=创建智能体失败", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?message=智能体容器已创建", created.ID), http.StatusSeeOther)
}

func (s *Server) handleAgentRoutes(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/ai/agents/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	agentID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && parts[1] == "config":
		s.handleAgentConfigPage(w, r, user, agentID)
	case r.Method == http.MethodPost && parts[1] == "remark":
		s.handleUpdateAgentRemark(w, r, user, agentID)
	case r.Method == http.MethodPost && parts[1] == "status":
		s.handleUpdateAgentStatus(w, r, user, agentID)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "token" && parts[2] == "reset":
		s.handleResetAgentToken(w, r, user, agentID)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "settings" && parts[2] == "security":
		s.handleUpdateAgentSecurityConfig(w, r, user, agentID)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "settings" && parts[2] == "other":
		s.handleUpdateAgentOtherConfig(w, r, user, agentID)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "settings" && parts[2] == "config-file":
		s.handleUpdateAgentConfigFile(w, r, user, agentID)
	case r.Method == http.MethodPost && parts[1] == "model":
		s.handleUpdateAgentModelConfig(w, r, user, agentID)
	case r.Method == http.MethodPost && parts[1] == "skills":
		s.handleUpdateAgentSkill(w, r, user, agentID)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "roles" && parts[2] == "create":
		s.handleCreateAgentRole(w, r, user, agentID)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "roles" && parts[2] == "delete":
		s.handleDeleteAgentRole(w, r, user, agentID)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[1] == "channels" && parts[2] == "weixin":
		s.handleUpdateWeixinChannel(w, r, user, agentID)
	case r.Method == http.MethodPost && parts[1] == "connect":
		s.handleCreateBindingForAgent(w, r, user, agentID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleAgentConfigPage(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	detail, err := s.store.GetAgentDetail(user.ID, agentID)
	if err != nil {
		http.Redirect(w, r, "/ai/agents?error=智能体不存在", http.StatusSeeOther)
		return
	}
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "channels"
	}
	subTab := r.URL.Query().Get("setting")
	if subTab == "" {
		subTab = "security"
	}
	bindingURL := ""
	if detail.Binding != nil {
		bindingURL = "/bindings/" + detail.Binding.ScanToken
	}
	s.render(w, "agent_config.html", viewData{
		Title:             detail.Agent.Name + " 配置",
		CurrentUser:       user,
		CurrentSection:    "agents",
		CurrentTab:        tab,
		CurrentSubTab:     subTab,
		Accounts:          s.store.ListAccounts(user.ID),
		Detail:            &detail,
		ChannelBindingURL: bindingURL,
		Message:           r.URL.Query().Get("message"),
		Error:             r.URL.Query().Get("error"),
	})
}

func (s *Server) handleUpdateAgentRemark(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "channels", "", "表单解析失败")
		return
	}
	if err := s.store.UpdateAgentRemark(user.ID, agentID, r.FormValue("remark")); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "channels", "", "更新备注失败")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?message=备注已更新", agentID), http.StatusSeeOther)
}

func (s *Server) handleUpdateAgentStatus(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/ai/agents?error=表单解析失败", http.StatusSeeOther)
		return
	}
	if err := s.store.UpdateAgentStatus(user.ID, agentID, strings.TrimSpace(r.FormValue("action"))); err != nil {
		http.Redirect(w, r, "/ai/agents?error=状态更新失败", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/ai/agents?message=智能体状态已更新", http.StatusSeeOther)
}

func (s *Server) handleResetAgentToken(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if _, err := s.store.ResetAgentToken(user.ID, agentID); err != nil {
		http.Redirect(w, r, "/ai/agents?error=重置 Token 失败", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/ai/agents?message=Token 已重置", http.StatusSeeOther)
}

func (s *Server) handleUpdateAgentSecurityConfig(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "settings", "security", "表单解析失败")
		return
	}
	if err := s.store.UpdateAgentSecurityConfig(user.ID, agentID, splitFormList(r.FormValue("allowed_origins"))); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "settings", "security", "保存安全设置失败")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?tab=settings&setting=security&message=安全设置已保存", agentID), http.StatusSeeOther)
}

func (s *Server) handleUpdateAgentOtherConfig(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "settings", "other", "表单解析失败")
		return
	}
	cfg := AgentOtherConfig{
		AutoUpgrade:    r.FormValue("auto_upgrade") == "on",
		Timezone:       valueOrDefault(strings.TrimSpace(r.FormValue("timezone")), "Asia/Shanghai"),
		Language:       valueOrDefault(strings.TrimSpace(r.FormValue("language")), "zh-CN"),
		Theme:          valueOrDefault(strings.TrimSpace(r.FormValue("theme")), "light"),
		SearchProvider: valueOrDefault(strings.TrimSpace(r.FormValue("search_provider")), "bing"),
		DefaultPrompt:  strings.TrimSpace(r.FormValue("default_prompt")),
	}
	if err := s.store.UpdateAgentOtherConfig(user.ID, agentID, cfg); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "settings", "other", "保存其他设置失败")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?tab=settings&setting=other&message=其他设置已保存", agentID), http.StatusSeeOther)
}

func (s *Server) handleUpdateAgentConfigFile(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "settings", "config-file", "表单解析失败")
		return
	}
	if err := s.store.UpdateAgentConfigFile(user.ID, agentID, r.FormValue("content")); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "settings", "config-file", "保存配置文件失败")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?tab=settings&setting=config-file&message=配置文件已保存", agentID), http.StatusSeeOther)
}

func (s *Server) handleUpdateAgentModelConfig(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "model", "", "表单解析失败")
		return
	}
	accountID, _ := strconv.ParseInt(r.FormValue("account_id"), 10, 64)
	if err := s.store.UpdateAgentModelConfig(user.ID, agentID, accountID, strings.TrimSpace(r.FormValue("model")), splitFormList(r.FormValue("fallbacks"))); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "model", "", "更新模型配置失败")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?tab=model&message=模型配置已更新", agentID), http.StatusSeeOther)
}

func (s *Server) handleUpdateAgentSkill(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "skills", "", "表单解析失败")
		return
	}
	skill := AgentSkill{
		Name:        strings.TrimSpace(r.FormValue("name")),
		Description: strings.TrimSpace(r.FormValue("description")),
		Source:      valueOrDefault(strings.TrimSpace(r.FormValue("source")), "custom"),
		Enabled:     r.FormValue("enabled") == "on",
	}
	if err := s.store.UpsertAgentSkill(user.ID, agentID, skill); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "skills", "", "更新 Skill 失败")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?tab=skills&message=Skill 已更新", agentID), http.StatusSeeOther)
}

func (s *Server) handleCreateAgentRole(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "agent", "", "表单解析失败")
		return
	}
	role := AgentRole{
		Name:     strings.TrimSpace(r.FormValue("name")),
		Prompt:   strings.TrimSpace(r.FormValue("prompt")),
		Model:    strings.TrimSpace(r.FormValue("model")),
		Channels: splitFormList(r.FormValue("channels")),
	}
	if err := s.store.CreateAgentRole(user.ID, agentID, role); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "agent", "", "新增角色失败")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?tab=agent&message=角色已新增", agentID), http.StatusSeeOther)
}

func (s *Server) handleDeleteAgentRole(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "agent", "", "表单解析失败")
		return
	}
	roleID, _ := strconv.ParseInt(r.FormValue("role_id"), 10, 64)
	if err := s.store.DeleteAgentRole(user.ID, agentID, roleID); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "agent", "", "删除角色失败")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?tab=agent&message=角色已删除", agentID), http.StatusSeeOther)
}

func (s *Server) handleUpdateWeixinChannel(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "channels", "", "表单解析失败")
		return
	}
	cfg := WeixinChannelConfig{
		Enabled:        r.FormValue("enabled") == "on",
		Mode:           strings.TrimSpace(r.FormValue("mode")),
		AppID:          strings.TrimSpace(r.FormValue("app_id")),
		AppSecret:      strings.TrimSpace(r.FormValue("app_secret")),
		Token:          strings.TrimSpace(r.FormValue("token")),
		EncodingAESKey: strings.TrimSpace(r.FormValue("encoding_aes_key")),
		BoundChannel:   valueOrDefault(strings.TrimSpace(r.FormValue("bound_channel")), "微信服务号"),
	}
	if err := s.store.UpdateAgentWeixinChannel(user.ID, agentID, cfg); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "channels", "", "保存微信渠道失败")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?tab=channels&message=微信渠道已保存", agentID), http.StatusSeeOther)
}

func (s *Server) handleCreateBindingForAgent(w http.ResponseWriter, r *http.Request, user *User, agentID int64) {
	if err := r.ParseForm(); err != nil {
		s.redirectAgentConfigError(w, r, agentID, "channels", "", "表单解析失败")
		return
	}
	channelName := valueOrDefault(strings.TrimSpace(r.FormValue("channel_name")), "微信服务号")
	binding, err := s.store.CreateBinding(user.ID, agentID, channelName)
	if err != nil {
		s.redirectAgentConfigError(w, r, agentID, "channels", "", "生成绑定二维码失败")
		return
	}
	http.Redirect(w, r, "/bindings/"+binding.ScanToken, http.StatusSeeOther)
}

func (s *Server) handleBindingRoutes(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	token := path.Base(r.URL.Path)
	if token == "" || token == "bindings" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost {
		binding, err := s.store.CompleteBinding(user.ID, token)
		if err != nil {
			http.Redirect(w, r, "/ai/agents?error=绑定确认失败", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/ai/agents/%d/config?tab=channels&message=微信渠道已连接", binding.AgentID), http.StatusSeeOther)
		return
	}
	binding, err := s.store.GetBindingByToken(user.ID, token)
	if err != nil {
		http.Redirect(w, r, "/ai/agents?error=未找到待绑定记录", http.StatusSeeOther)
		return
	}
	png, err := qrcode.Encode(binding.QRContent, qrcode.Medium, 256)
	if err != nil {
		http.Redirect(w, r, "/ai/agents?error=二维码生成失败", http.StatusSeeOther)
		return
	}
	s.render(w, "binding.html", viewData{
		Title:             "微信扫码绑定",
		CurrentUser:       user,
		CurrentSection:    "agents",
		Binding:           &binding,
		ChannelBindingURL: fmt.Sprintf("/ai/agents/%d/config?tab=channels", binding.AgentID),
		QRDataURI:         "data:image/png;base64," + encodeBase64(png),
	})
}

func (s *Server) currentUser(r *http.Request) (*User, bool) {
	cookie, err := r.Cookie("openclaw_session")
	if err != nil || cookie.Value == "" {
		return nil, false
	}
	userID, err := s.sessions.Verify(cookie.Value)
	if err != nil {
		return nil, false
	}
	user, ok := s.store.GetUserByID(userID)
	if !ok {
		return nil, false
	}
	return &user, true
}

func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (*User, bool) {
	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/?error=请先登录", http.StatusSeeOther)
		return nil, false
	}
	return user, true
}

func (s *Server) setSession(w http.ResponseWriter, userID int64) {
	http.SetCookie(w, &http.Cookie{
		Name:     "openclaw_session",
		Value:    s.sessions.Sign(userID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) render(w http.ResponseWriter, name string, data viewData) {
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "render template failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) redirectAgentConfigError(w http.ResponseWriter, r *http.Request, agentID int64, tab, subTab, message string) {
	target := fmt.Sprintf("/ai/agents/%d/config?tab=%s", agentID, template.URLQueryEscaper(tab))
	if subTab != "" {
		target += "&setting=" + template.URLQueryEscaper(subTab)
	}
	target += "&error=" + template.URLQueryEscaper(message)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func splitFormList(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\n", ",")
	return compactUniqueStrings(strings.Split(raw, ","))
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func encodeBase64(raw []byte) string {
	return base64.StdEncoding.EncodeToString(raw)
}

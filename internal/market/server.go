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
	Title            string
	CurrentUser      *User
	ProviderCatalogs []ProviderCatalog
	Accounts         []AgentAccount
	Agents           []AgentDashboardItem
	Message          string
	Error            string
	CurrentTab       string
	Binding          *ChannelBinding
	QRDataURI        string
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
	tpl, err := template.ParseFS(webFS, "web/templates/*.html")
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
	mux.HandleFunc("/dashboard", s.handleDashboard)
	mux.HandleFunc("/ai/accounts/create", s.handleCreateAccount)
	mux.HandleFunc("/ai/accounts/models/create", s.handleCreateAccountModel)
	mux.HandleFunc("/ai/agents/create", s.handleCreateAgent)
	mux.HandleFunc("/ai/agents/model/update", s.handleUpdateAgentModelConfig)
	mux.HandleFunc("/ai/agents/connect", s.handleCreateBinding)
	mux.HandleFunc("/bindings/", s.handleBindingRoutes)
	return mux
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if user, _ := s.currentUser(r); user != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
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
	http.Redirect(w, r, "/dashboard?message=注册成功，已自动登录", http.StatusSeeOther)
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
	http.Redirect(w, r, "/dashboard?message=登录成功", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "openclaw_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/?message=已退出登录", http.StatusSeeOther)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	s.render(w, "dashboard.html", viewData{
		Title:            "AI 控制台",
		CurrentUser:      user,
		ProviderCatalogs: s.store.ListProviderCatalogs(),
		Accounts:         s.store.ListAccounts(user.ID),
		Agents:           s.store.ListDashboardAgents(user.ID),
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
		http.Redirect(w, r, "/dashboard?error=表单解析失败", http.StatusSeeOther)
		return
	}
	provider := strings.TrimSpace(r.FormValue("provider"))
	name := strings.TrimSpace(r.FormValue("name"))
	apiKey := strings.TrimSpace(r.FormValue("api_key"))
	baseURL := strings.TrimSpace(r.FormValue("base_url"))
	apiType := strings.TrimSpace(r.FormValue("api_type"))
	remark := strings.TrimSpace(r.FormValue("remark"))
	if _, err := s.store.CreateAccount(user.ID, provider, name, apiKey, baseURL, apiType, remark); err != nil {
		http.Redirect(w, r, "/dashboard?error=创建模型账号失败", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/dashboard?message=模型账号已创建", http.StatusSeeOther)
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
		http.Redirect(w, r, "/dashboard?error=表单解析失败", http.StatusSeeOther)
		return
	}
	accountID, err := strconv.ParseInt(r.FormValue("account_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/dashboard?error=无效的模型账号", http.StatusSeeOther)
		return
	}
	contextWindow, _ := strconv.Atoi(r.FormValue("context_window"))
	maxTokens, _ := strconv.Atoi(r.FormValue("max_tokens"))
	model := AgentAccountModel{
		ID:            strings.TrimSpace(r.FormValue("model_id")),
		Name:          strings.TrimSpace(r.FormValue("name")),
		ContextWindow: contextWindow,
		MaxTokens:     maxTokens,
		Input:         compactStrings(strings.Split(r.FormValue("input_types"), ",")),
		Reasoning:     r.FormValue("reasoning") == "on",
	}
	if _, err := s.store.CreateAccountModel(user.ID, accountID, model); err != nil {
		http.Redirect(w, r, "/dashboard?error=新增模型失败", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/dashboard?message=账号模型已添加", http.StatusSeeOther)
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
		http.Redirect(w, r, "/dashboard?error=表单解析失败", http.StatusSeeOther)
		return
	}

	accountID, err := strconv.ParseInt(r.FormValue("account_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/dashboard?error=请选择模型账号", http.StatusSeeOther)
		return
	}
	account, ok := s.store.GetAccount(user.ID, accountID)
	if !ok {
		http.Redirect(w, r, "/dashboard?error=模型账号不存在", http.StatusSeeOther)
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
		http.Redirect(w, r, "/dashboard?error=请选择账号下的有效模型", http.StatusSeeOther)
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
	allowedOrigins := compactStrings(strings.Split(r.FormValue("allowed_origins"), ","))
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"http://127.0.0.1"}
	}
	token := strings.TrimSpace(r.FormValue("token"))

	ctx, cancel := context.WithTimeout(r.Context(), defaultProvisionTimeout())
	defer cancel()
	provision, err := s.runtime.ProvisionOpenClaw(ctx, ProvisionRequest{
		UserID:         user.ID,
		Name:           strings.TrimSpace(r.FormValue("name")),
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
			http.Redirect(w, r, "/dashboard?error=Docker%20不可用，请先启动%20Docker%20daemon", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/dashboard?error="+template.URLQueryEscaper(err.Error()), http.StatusSeeOther)
		return
	}

	fallbacks := compactStrings(strings.Split(r.FormValue("fallbacks"), ","))
	agent := Agent{
		Name:          strings.TrimSpace(r.FormValue("name")),
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
		Status:        "container_created",
		AccountID:     account.ID,
		ModelConfig: AgentModelConfig{
			AccountID: account.ID,
			Model:     selected.ID,
			Fallbacks: fallbacks,
		},
		ConfigPath:          provision.ConfigPath,
		WebUIPort:           provision.WebUIPort,
		BridgePort:          provision.BridgePort,
		AllowedOrigins:      allowedOrigins,
		DockerContainerID:   provision.ContainerID,
		DockerContainerName: provision.ContainerName,
		DockerImage:         provision.Image,
		DockerGatewayToken:  provision.GatewayToken,
		DockerConfigDir:     provision.ConfigDir,
		DockerWorkspaceDir:  provision.WorkspaceDir,
	}
	if _, err := s.store.CreateAgent(user.ID, agent); err != nil {
		http.Redirect(w, r, "/dashboard?error=创建智能体失败", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/dashboard?message=智能体容器已创建", http.StatusSeeOther)
}

func (s *Server) handleUpdateAgentModelConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/dashboard?error=表单解析失败", http.StatusSeeOther)
		return
	}
	agentID, _ := strconv.ParseInt(r.FormValue("agent_id"), 10, 64)
	accountID, _ := strconv.ParseInt(r.FormValue("account_id"), 10, 64)
	modelID := strings.TrimSpace(r.FormValue("model"))
	fallbacks := compactStrings(strings.Split(r.FormValue("fallbacks"), ","))
	if err := s.store.UpdateAgentModelConfig(user.ID, agentID, accountID, modelID, fallbacks); err != nil {
		http.Redirect(w, r, "/dashboard?error=更新智能体模型配置失败", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/dashboard?message=智能体模型配置已更新", http.StatusSeeOther)
}

func (s *Server) handleCreateBinding(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/dashboard?error=表单解析失败", http.StatusSeeOther)
		return
	}
	agentID, err := strconv.ParseInt(r.FormValue("agent_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/dashboard?error=无效的智能体", http.StatusSeeOther)
		return
	}
	channelName := strings.TrimSpace(r.FormValue("channel_name"))
	if channelName == "" {
		channelName = "微信"
	}
	binding, err := s.store.CreateBinding(user.ID, agentID, channelName)
	if err != nil {
		http.Redirect(w, r, "/dashboard?error=生成渠道绑定二维码失败", http.StatusSeeOther)
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
		if _, err := s.store.CompleteBinding(user.ID, token); err != nil {
			http.Redirect(w, r, "/dashboard?error=绑定确认失败", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/dashboard?message=微信 channel 已连接", http.StatusSeeOther)
		return
	}
	binding, err := s.store.GetBindingByToken(user.ID, token)
	if err != nil {
		http.Redirect(w, r, "/dashboard?error=未找到待绑定记录", http.StatusSeeOther)
		return
	}
	png, err := qrcode.Encode(binding.QRContent, qrcode.Medium, 256)
	if err != nil {
		http.Redirect(w, r, "/dashboard?error=二维码生成失败", http.StatusSeeOther)
		return
	}
	s.render(w, "binding.html", viewData{
		Title:       "微信扫码绑定",
		CurrentUser: user,
		Binding:     &binding,
		QRDataURI:   "data:image/png;base64," + encodeBase64(png),
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

func encodeBase64(raw []byte) string {
	return base64.StdEncoding.EncodeToString(raw)
}

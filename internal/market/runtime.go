package market

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultGatewayPort = 18789

type ProvisionRequest struct {
	UserID         int64
	Name           string
	AgentType      string
	WebUIPort      int
	BridgePort     int
	StorageDir     string
	Token          string
	Provider       string
	Model          string
	APIType        string
	BaseURL        string
	APIKey         string
	MaxTokens      int
	ContextWindow  int
	AllowedOrigins []string
}

type ProvisionedContainer struct {
	ContainerID   string
	ContainerName string
	Image         string
	WebUIPort     int
	BridgePort    int
	GatewayToken  string
	ConfigDir     string
	WorkspaceDir  string
	ConfigPath    string
}

type Runtime interface {
	ProvisionOpenClaw(ctx context.Context, req ProvisionRequest) (ProvisionedContainer, error)
	ChangeContainerState(ctx context.Context, containerName, action string) error
	CheckWeixinPlugin(ctx context.Context, agent Agent, checkLatest bool) (AgentPluginStatus, error)
	ManageWeixinPlugin(ctx context.Context, agent Agent, action string) (AgentPluginStatus, error)
	LoginWeixinChannel(ctx context.Context, agent Agent, onOutput func(string)) error
}

type DockerRuntime struct {
	image       string
	gatewayPort int
}

func NewDockerRuntimeFromEnv() *DockerRuntime {
	image := strings.TrimSpace(os.Getenv("OPENCLAW_AGENT_IMAGE"))
	if image == "" {
		image = "1panel/openclaw:2026.4.14"
	}
	return &DockerRuntime{image: image, gatewayPort: defaultGatewayPort}
}

func (r *DockerRuntime) ProvisionOpenClaw(ctx context.Context, req ProvisionRequest) (ProvisionedContainer, error) {
	containerName := fmt.Sprintf("%s-u%d-%s", req.AgentType, req.UserID, slugify(req.Name))
	configDir := filepath.Join(req.StorageDir, containerName, "config")
	workspaceDir := filepath.Join(req.StorageDir, containerName, "workspace")
	absConfigDir, err := filepath.Abs(configDir)
	if err != nil {
		return ProvisionedContainer{}, fmt.Errorf("resolve config dir: %w", err)
	}
	absWorkspaceDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		return ProvisionedContainer{}, fmt.Errorf("resolve workspace dir: %w", err)
	}
	if err := os.MkdirAll(absConfigDir, 0o755); err != nil {
		return ProvisionedContainer{}, fmt.Errorf("mkdir config dir: %w", err)
	}
	if err := os.MkdirAll(absWorkspaceDir, 0o755); err != nil {
		return ProvisionedContainer{}, fmt.Errorf("mkdir workspace dir: %w", err)
	}

	gatewayToken := strings.TrimSpace(req.Token)
	if gatewayToken == "" {
		var err error
		gatewayToken, err = randomHex(16)
		if err != nil {
			return ProvisionedContainer{}, fmt.Errorf("gateway token: %w", err)
		}
	}

	args := []string{
		"create",
		"--name", containerName,
		"-p", fmt.Sprintf("%d:%d", req.WebUIPort, r.gatewayPort),
		"-e", "OPENCLAW_GATEWAY_TOKEN=" + gatewayToken,
		"-e", "PROVIDER=" + req.Provider,
		"-e", "MODEL=" + req.Model,
		"-e", "API_TYPE=" + req.APIType,
		"-e", "BASE_URL=" + req.BaseURL,
		"-e", "API_KEY=" + req.APIKey,
		"-e", fmt.Sprintf("MAX_TOKENS=%d", req.MaxTokens),
		"-e", fmt.Sprintf("CONTEXT_WINDOW=%d", req.ContextWindow),
		"-e", "ALLOWED_ORIGIN=" + firstAllowedOrigin(req.AllowedOrigins),
		"-v", absConfigDir + ":/home/node/.openclaw",
		"-v", absWorkspaceDir + ":/home/node/.openclaw/workspace",
		r.image,
	}

	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(text, "Cannot connect to the Docker daemon") {
			return ProvisionedContainer{}, fmt.Errorf("%w: %s", ErrDockerUnavailable, text)
		}
		if text == "" {
			text = err.Error()
		}
		return ProvisionedContainer{}, fmt.Errorf("docker create failed: %s", text)
	}
	return ProvisionedContainer{
		ContainerID:   strings.TrimSpace(string(out)),
		ContainerName: containerName,
		Image:         r.image,
		WebUIPort:     req.WebUIPort,
		BridgePort:    req.BridgePort,
		GatewayToken:  gatewayToken,
		ConfigDir:     absConfigDir,
		WorkspaceDir:  absWorkspaceDir,
		ConfigPath:    filepath.Join(absConfigDir, "openclaw.json"),
	}, nil
}

type FakeRuntime struct {
	Provision ProvisionedContainer
	Err       error
	Actions   []string
	Plugin    AgentPluginStatus
	Logs      []string
}

func (r FakeRuntime) ProvisionOpenClaw(_ context.Context, req ProvisionRequest) (ProvisionedContainer, error) {
	if r.Err != nil {
		return ProvisionedContainer{}, r.Err
	}
	item := r.Provision
	if item.ContainerID == "" {
		item.ContainerID = "ctr-test-001"
	}
	if item.ContainerName == "" {
		item.ContainerName = fmt.Sprintf("%s-u%d-%s", req.AgentType, req.UserID, slugify(req.Name))
	}
	if item.Image == "" {
		item.Image = "1panel/openclaw:2026.4.14"
	}
	if item.WebUIPort == 0 {
		item.WebUIPort = req.WebUIPort
	}
	if item.GatewayToken == "" {
		item.GatewayToken = req.Token
	}
	if item.GatewayToken == "" {
		item.GatewayToken = "fake-token"
	}
	if item.ConfigDir == "" {
		item.ConfigDir = filepath.Join(req.StorageDir, item.ContainerName, "config")
	}
	if item.WorkspaceDir == "" {
		item.WorkspaceDir = filepath.Join(req.StorageDir, item.ContainerName, "workspace")
	}
	if item.ConfigPath == "" {
		item.ConfigPath = filepath.Join(item.ConfigDir, "openclaw.json")
	}
	return item, nil
}

func (r FakeRuntime) ChangeContainerState(_ context.Context, _ string, _ string) error {
	return r.Err
}

func (r FakeRuntime) CheckWeixinPlugin(_ context.Context, _ Agent, _ bool) (AgentPluginStatus, error) {
	if r.Err != nil {
		return AgentPluginStatus{}, r.Err
	}
	status := r.Plugin
	if status.Type == "" {
		status.Type = "weixin"
	}
	return status, nil
}

func (r FakeRuntime) ManageWeixinPlugin(_ context.Context, _ Agent, action string) (AgentPluginStatus, error) {
	if r.Err != nil {
		return AgentPluginStatus{}, r.Err
	}
	status := r.Plugin
	if status.Type == "" {
		status.Type = "weixin"
	}
	status.LastAction = action
	switch action {
	case "install", "upgrade":
		status.Installed = true
	case "uninstall":
		status.Installed = false
		status.CurrentVersion = ""
		status.LatestVersion = ""
		status.Upgradable = false
	default:
		return AgentPluginStatus{}, fmt.Errorf("invalid plugin action: %s", action)
	}
	status.UpdatedAt = time.Now()
	return status, nil
}

func (r FakeRuntime) LoginWeixinChannel(_ context.Context, _ Agent, onOutput func(string)) error {
	if r.Err != nil {
		return r.Err
	}
	for _, item := range r.Logs {
		if onOutput != nil {
			onOutput(item)
		}
	}
	return nil
}

func (r *DockerRuntime) ChangeContainerState(ctx context.Context, containerName, action string) error {
	action = strings.TrimSpace(action)
	if action != "start" && action != "stop" && action != "restart" {
		return fmt.Errorf("invalid docker action: %s", action)
	}
	out, err := exec.CommandContext(ctx, "docker", action, containerName).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(text, "Cannot connect to the Docker daemon") {
			return fmt.Errorf("%w: %s", ErrDockerUnavailable, text)
		}
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("docker %s failed: %s", action, text)
	}
	return nil
}

func (r *DockerRuntime) CheckWeixinPlugin(ctx context.Context, agent Agent, checkLatest bool) (AgentPluginStatus, error) {
	status := AgentPluginStatus{Type: "weixin"}
	packagePath, err := resolvePluginPackagePath(agent.DockerConfigDir, "weixin")
	if err != nil {
		return status, err
	}
	if _, err := os.Stat(packagePath); err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		return status, err
	}
	status.Installed = true
	currentVersion, err := loadPluginCurrentVersion(packagePath)
	if err != nil {
		return status, err
	}
	status.CurrentVersion = currentVersion
	if !checkLatest {
		return status, nil
	}
	latestVersion, err := loadPluginLatestVersion(ctx, agent.DockerContainerName, "weixin")
	if err != nil {
		return status, err
	}
	status.LatestVersion = latestVersion
	status.Upgradable = compareVersion(latestVersion, currentVersion) > 0
	return status, nil
}

func (r *DockerRuntime) ManageWeixinPlugin(ctx context.Context, agent Agent, action string) (AgentPluginStatus, error) {
	spec, pluginID, err := resolvePluginMeta("weixin")
	if err != nil {
		return AgentPluginStatus{}, err
	}
	timeout := 10 * time.Minute
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch strings.TrimSpace(action) {
	case "install":
		if _, err := runDockerCommand(runCtx, "exec", agent.DockerContainerName, "sh", "-c", buildPluginInstallScript(spec, pluginID)); err != nil {
			return AgentPluginStatus{}, err
		}
		if err := appendPluginAllow(agent.ConfigPath, pluginID); err != nil {
			return AgentPluginStatus{}, err
		}
	case "upgrade":
		if _, err := runDockerCommand(runCtx, "exec", "-i", agent.DockerContainerName, "sh", "-c", buildPluginUninstallScript(pluginID)); err != nil {
			return AgentPluginStatus{}, err
		}
		if _, err := runDockerCommand(runCtx, "exec", agent.DockerContainerName, "sh", "-c", buildPluginInstallScript(spec, pluginID)); err != nil {
			return AgentPluginStatus{}, err
		}
		if err := appendPluginAllow(agent.ConfigPath, pluginID); err != nil {
			return AgentPluginStatus{}, err
		}
	case "uninstall":
		if _, err := runDockerCommand(runCtx, "exec", "-i", agent.DockerContainerName, "sh", "-c", buildPluginUninstallScript(pluginID)); err != nil {
			return AgentPluginStatus{}, err
		}
		if err := cleanupWeixinPluginConfig(agent.ConfigPath, pluginID); err != nil {
			return AgentPluginStatus{}, err
		}
	default:
		return AgentPluginStatus{}, fmt.Errorf("invalid plugin action: %s", action)
	}
	status, err := r.CheckWeixinPlugin(ctx, agent, true)
	if err != nil {
		return AgentPluginStatus{}, err
	}
	status.LastAction = action
	switch action {
	case "install":
		status.LastMessage = "微信插件安装完成"
	case "upgrade":
		status.LastMessage = "微信插件已升级到最新版本"
	case "uninstall":
		status.LastMessage = "微信插件已卸载"
	}
	status.UpdatedAt = time.Now()
	return status, nil
}

func (r *DockerRuntime) LoginWeixinChannel(ctx context.Context, agent Agent, onOutput func(string)) error {
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "docker", "exec", "-i", agent.DockerContainerName, "openclaw", "channels", "login", "--channel", "openclaw-weixin")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start weixin login: %w", err)
	}

	var (
		buf bytes.Buffer
		mu  sync.Mutex
		wg  sync.WaitGroup
	)
	appendChunk := func(chunk []byte) {
		if len(chunk) == 0 {
			return
		}
		mu.Lock()
		buf.Write(chunk)
		mu.Unlock()
		if onOutput != nil {
			onOutput(string(chunk))
		}
	}
	stream := func(reader io.Reader) {
		defer wg.Done()
		chunk := make([]byte, 2048)
		for {
			n, readErr := reader.Read(chunk)
			if n > 0 {
				appendChunk(chunk[:n])
			}
			if readErr != nil {
				return
			}
		}
	}
	wg.Add(2)
	go stream(stdout)
	go stream(stderr)
	wg.Wait()
	if err := cmd.Wait(); err != nil {
		text := strings.TrimSpace(buf.String())
		if strings.Contains(text, "Cannot connect to the Docker daemon") {
			return fmt.Errorf("%w: %s", ErrDockerUnavailable, text)
		}
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("weixin login failed: %s", text)
	}
	return nil
}

func slugify(input string) string {
	value := strings.ToLower(strings.TrimSpace(input))
	value = strings.ReplaceAll(value, " ", "-")
	re := regexp.MustCompile(`[^a-z0-9\-]+`)
	value = re.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "agent"
	}
	if len(value) > 32 {
		value = value[:32]
	}
	return value
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func defaultProvisionTimeout() time.Duration {
	return 20 * time.Second
}

func firstAllowedOrigin(origins []string) string {
	if len(origins) == 0 {
		return "http://127.0.0.1"
	}
	return strings.TrimSpace(origins[0])
}

type pluginPackage struct {
	Version string `json:"version"`
}

func resolvePluginMeta(pluginType string) (string, string, error) {
	switch strings.TrimSpace(pluginType) {
	case "weixin":
		return "@tencent-weixin/openclaw-weixin", "openclaw-weixin", nil
	default:
		return "", "", fmt.Errorf("unsupported plugin type: %s", pluginType)
	}
}

func resolvePluginPackagePath(configDir, pluginType string) (string, error) {
	_, pluginID, err := resolvePluginMeta(pluginType)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(configDir) == "" {
		return "", fmt.Errorf("empty config dir")
	}
	return filepath.Join(configDir, "conf", "extensions", pluginID, "package.json"), nil
}

func buildPluginInstallScript(spec, pluginID string) string {
	return fmt.Sprintf(
		"set -e; workdir=/tmp/openclaw-market-plugins/%s; rm -rf \"$workdir\"; mkdir -p \"$workdir\"; cd \"$workdir\"; npm pack --silent %q >/dev/null 2>&1; pkg=$(find \"$workdir\" -maxdepth 1 -type f -name '*.tgz' | head -n 1); printf '%%s\\n' \"$pkg\"; openclaw plugins install \"$pkg\" --dangerously-force-unsafe-install; rm -rf \"$workdir\"",
		pluginID,
		spec,
	)
}

func buildPluginUninstallScript(pluginID string) string {
	return fmt.Sprintf(
		"set +e; printf 'yes\\n' | openclaw plugins uninstall %s; code=$?; if [ \"$code\" -eq 137 ]; then exit 0; fi; exit \"$code\"",
		pluginID,
	)
}

func runDockerCommand(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(text, "Cannot connect to the Docker daemon") {
			return "", fmt.Errorf("%w: %s", ErrDockerUnavailable, text)
		}
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("docker %s failed: %s", strings.Join(args, " "), text)
	}
	return text, nil
}

func loadPluginCurrentVersion(packagePath string) (string, error) {
	content, err := os.ReadFile(packagePath)
	if err != nil {
		return "", err
	}
	var pkg pluginPackage
	if err := json.Unmarshal(content, &pkg); err != nil {
		return "", err
	}
	return strings.TrimSpace(pkg.Version), nil
}

func loadPluginLatestVersion(ctx context.Context, containerName, pluginType string) (string, error) {
	spec, _, err := resolvePluginMeta(pluginType)
	if err != nil {
		return "", err
	}
	out, err := runDockerCommand(ctx, "exec", containerName, "npm", "view", spec, "version", "--json")
	if err != nil {
		return "", err
	}
	var version string
	if err := json.Unmarshal([]byte(out), &version); err == nil {
		return strings.TrimSpace(version), nil
	}
	return strings.Trim(strings.TrimSpace(out), `"`), nil
}

func appendPluginAllow(configPath, pluginID string) error {
	conf, err := readConfigJSON(configPath)
	if err != nil {
		return err
	}
	plugins := ensureMap(conf, "plugins")
	rawAllow, _ := plugins["allow"].([]interface{})
	allow := make([]string, 0, len(rawAllow))
	for _, item := range rawAllow {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			allow = append(allow, value)
		}
	}
	if !slices.Contains(allow, pluginID) {
		allow = append(allow, pluginID)
	}
	items := make([]interface{}, 0, len(allow))
	for _, item := range allow {
		items = append(items, item)
	}
	plugins["allow"] = items
	return writeConfigJSON(configPath, conf)
}

func cleanupWeixinPluginConfig(configPath, pluginID string) error {
	conf, err := readConfigJSON(configPath)
	if err != nil {
		return err
	}
	if channels, ok := conf["channels"].(map[string]interface{}); ok {
		delete(channels, "weixin")
	}
	if plugins, ok := conf["plugins"].(map[string]interface{}); ok {
		if entries, ok := plugins["entries"].(map[string]interface{}); ok {
			delete(entries, pluginID)
		}
		if rawAllow, ok := plugins["allow"].([]interface{}); ok {
			allow := make([]interface{}, 0, len(rawAllow))
			for _, item := range rawAllow {
				value, isString := item.(string)
				if !isString || value != pluginID {
					allow = append(allow, item)
				}
			}
			plugins["allow"] = allow
		}
	}
	return writeConfigJSON(configPath, conf)
}

func readConfigJSON(configPath string) (map[string]interface{}, error) {
	if strings.TrimSpace(configPath) == "" {
		return map[string]interface{}{}, nil
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return map[string]interface{}{}, nil
	}
	var conf map[string]interface{}
	if err := json.Unmarshal(content, &conf); err != nil {
		return nil, err
	}
	if conf == nil {
		conf = map[string]interface{}{}
	}
	return conf, nil
}

func writeConfigJSON(configPath string, conf map[string]interface{}) error {
	if strings.TrimSpace(configPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(configPath, content, 0o644)
}

func ensureMap(parent map[string]interface{}, key string) map[string]interface{} {
	child, ok := parent[key].(map[string]interface{})
	if ok && child != nil {
		return child
	}
	child = map[string]interface{}{}
	parent[key] = child
	return child
}

func compareVersion(left, right string) int {
	leftParts := versionParts(left)
	rightParts := versionParts(right)
	maxLen := len(leftParts)
	if len(rightParts) > maxLen {
		maxLen = len(rightParts)
	}
	for i := 0; i < maxLen; i++ {
		lv, rv := 0, 0
		if i < len(leftParts) {
			lv = leftParts[i]
		}
		if i < len(rightParts) {
			rv = rightParts[i]
		}
		if lv > rv {
			return 1
		}
		if lv < rv {
			return -1
		}
	}
	return 0
}

func versionParts(value string) []int {
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	if value == "" {
		return nil
	}
	chunks := strings.Split(value, ".")
	result := make([]int, 0, len(chunks))
	for _, chunk := range chunks {
		n, err := strconv.Atoi(chunk)
		if err != nil {
			result = append(result, 0)
			continue
		}
		result = append(result, n)
	}
	return result
}

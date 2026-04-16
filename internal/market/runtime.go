package market

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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
}

type DockerRuntime struct {
	image       string
	gatewayPort int
}

func NewDockerRuntimeFromEnv() *DockerRuntime {
	image := strings.TrimSpace(os.Getenv("OPENCLAW_AGENT_IMAGE"))
	if image == "" {
		image = "ghcr.io/openclaw/openclaw:latest"
	}
	return &DockerRuntime{image: image, gatewayPort: defaultGatewayPort}
}

func (r *DockerRuntime) ProvisionOpenClaw(ctx context.Context, req ProvisionRequest) (ProvisionedContainer, error) {
	containerName := fmt.Sprintf("%s-u%d-%s", req.AgentType, req.UserID, slugify(req.Name))
	configDir := filepath.Join(req.StorageDir, containerName, "config")
	workspaceDir := filepath.Join(req.StorageDir, containerName, "workspace")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return ProvisionedContainer{}, fmt.Errorf("mkdir config dir: %w", err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
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
		"-v", configDir + ":/home/node/.openclaw",
		"-v", workspaceDir + ":/home/node/.openclaw/workspace",
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
		ConfigDir:     configDir,
		WorkspaceDir:  workspaceDir,
		ConfigPath:    filepath.Join(configDir, "openclaw.json"),
	}, nil
}

type FakeRuntime struct {
	Provision ProvisionedContainer
	Err       error
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
		item.Image = "ghcr.io/openclaw/openclaw:latest"
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

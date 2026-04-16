package market

import (
	"path/filepath"
	"testing"
)

func TestStoreAccountAgentBindingFlow(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	user, err := store.CreateUser("demo@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	account, err := store.CreateAccount(user.ID, "openai", "主账号", "sk-test", "", "", "demo")
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	model, err := store.CreateAccountModel(user.ID, account.ID, AgentAccountModel{
		ID:            "gpt-4.1-mini",
		Name:          "GPT-4.1 Mini",
		ContextWindow: 128000,
		MaxTokens:     8192,
		Input:         []string{"text"},
	})
	if err != nil {
		t.Fatalf("CreateAccountModel() error = %v", err)
	}

	agent, err := store.CreateAgent(user.ID, Agent{
		Name:      "客服 Agent",
		AgentType: "openclaw",
		Provider:  account.Provider,
		Model:     model.ID,
		APIType:   account.APIType,
		BaseURL:   account.BaseURL,
		APIKey:    account.APIKey,
		AccountID: account.ID,
		ModelConfig: AgentModelConfig{
			AccountID: account.ID,
			Model:     model.ID,
			Fallbacks: []string{"gpt-4o"},
		},
		WebUIPort:           18789,
		DockerContainerName: "openclaw-u1-service",
		DockerImage:         "1panel/openclaw:2026.4.14",
		Status:              "running",
	})
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}

	if err := store.UpdateAgentModelConfig(user.ID, agent.ID, account.ID, model.ID, []string{"gpt-4o"}); err != nil {
		t.Fatalf("UpdateAgentModelConfig() error = %v", err)
	}

	binding, err := store.CreateBinding(user.ID, agent.ID, "微信服务号")
	if err != nil {
		t.Fatalf("CreateBinding() error = %v", err)
	}
	done, err := store.CompleteBinding(user.ID, binding.ScanToken)
	if err != nil {
		t.Fatalf("CompleteBinding() error = %v", err)
	}
	if done.Status != "connected" {
		t.Fatalf("binding status = %s, want connected", done.Status)
	}

	if err := store.UpdateAgentSecurityConfig(user.ID, agent.ID, []string{"http://127.0.0.1", "http://localhost:3000"}); err != nil {
		t.Fatalf("UpdateAgentSecurityConfig() error = %v", err)
	}

	if err := store.UpdateAgentOtherConfig(user.ID, agent.ID, AgentOtherConfig{
		AutoUpgrade:    true,
		Timezone:       "Asia/Shanghai",
		Language:       "zh-CN",
		Theme:          "light",
		SearchProvider: "bing",
		DefaultPrompt:  "你是客服智能体",
	}); err != nil {
		t.Fatalf("UpdateAgentOtherConfig() error = %v", err)
	}

	if err := store.UpsertAgentSkill(user.ID, agent.ID, AgentSkill{
		Name:        "wechat",
		Description: "微信渠道处理",
		Source:      "custom",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("UpsertAgentSkill() error = %v", err)
	}

	if err := store.CreateAgentRole(user.ID, agent.ID, AgentRole{
		Name:     "客服接待",
		Prompt:   "回答售前问题",
		Model:    model.ID,
		Channels: []string{"weixin"},
	}); err != nil {
		t.Fatalf("CreateAgentRole() error = %v", err)
	}

	detail, err := store.GetAgentDetail(user.ID, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentDetail() error = %v", err)
	}
	if len(detail.Agent.Roles) != 1 {
		t.Fatalf("role count = %d, want 1", len(detail.Agent.Roles))
	}
	if len(detail.Agent.Skills) == 0 {
		t.Fatalf("skills should not be empty")
	}
	if len(detail.Agent.SecurityConfig.AllowedOrigins) != 2 {
		t.Fatalf("allowed origins = %d, want 2", len(detail.Agent.SecurityConfig.AllowedOrigins))
	}
}

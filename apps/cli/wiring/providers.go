package wiring

import (
	"tofu/internal/models/llm/accounts"
	claudesub "tofu/internal/models/llm/providers/claude-sub"
	codexsub "tofu/internal/models/llm/providers/codex-sub"
	"tofu/internal/models/llm/quota"
)

func Specs() accounts.Specs {
	return accounts.Specs{accounts.ClaudeSub: claudesub.Spec(), accounts.CodexSub: codexsub.Spec()}
}

func UsageEndpoints(urls map[quota.Provider]string) quota.Endpoints {
	endpoints := quota.Endpoints{quota.ClaudeSub: claudesub.UsageEndpoint(), quota.CodexSub: codexsub.UsageEndpoint()}
	if urls == nil {
		return endpoints
	}
	for provider, endpoint := range endpoints {
		endpoint.URL = urls[provider]
		endpoints[provider] = endpoint
	}
	return endpoints
}

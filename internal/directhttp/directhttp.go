package directhttp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/oneshotdecode"
	"github.com/sergeknystautas/schmux/internal/schema"
)

const APISuffix = "::api"

func StripAPISuffix(target string) (string, bool) {
	if strings.HasSuffix(target, APISuffix) {
		return strings.TrimSuffix(target, APISuffix), true
	}
	return target, false
}

type apiKind int

const (
	apiKindUnknown apiKind = iota
	apiKindAnthropic
	apiKindThirdPartyClaudeHarness
	apiKindOllama
)

type classifyAPIModelMeta struct {
	provider               string
	claudeRunnerModelValue string
	claudeRunnerEndpoint   string
}

func anthropicEndpoint() string {
	if v := os.Getenv("SCHMUX_ANTHROPIC_ENDPOINT"); v != "" {
		return v
	}
	return "https://api.anthropic.com/v1/messages"
}

func ExecuteAPI[T any](
	ctx context.Context,
	cfg *config.Config,
	targetName, prompt, schemaLabel string,
	timeout time.Duration,
	dir string,
) (T, error) {
	var zero T
	bareID, _ := StripAPISuffix(targetName)
	if bareID == "" {
		return zero, fmt.Errorf("%w: target %q has no model id before ::api", ErrModelNotFound, targetName)
	}

	schemaDoc, err := schema.Get(schemaLabel)
	if err != nil {
		return zero, fmt.Errorf("schema %q: %w", schemaLabel, err)
	}

	kind, modelMeta, err := classifyAPITarget(cfg, bareID)
	if err != nil {
		return zero, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_ = dir

	switch kind {
	case apiKindAnthropic:
		token, err := config.GetAnthropicOAuthToken()
		if err != nil {
			return zero, err
		}
		if token == "" {
			return zero, ErrMissingToken
		}
		raw, err := callAnthropic(timeoutCtx, anthropicCallParams{
			Endpoint: anthropicEndpoint(),
			Token:    token,
			Request: buildAnthropicRequest(
				bareID, prompt, buildAnthropicSystemPrompt(schemaDoc), 0),
		})
		if err != nil {
			return zero, err
		}
		return decodeAPIResponse[T](raw)

	case apiKindThirdPartyClaudeHarness:
		token, endpoint, err := thirdPartyAnthropicAuth(cfg, modelMeta)
		if err != nil {
			return zero, err
		}
		raw, err := callAnthropic(timeoutCtx, anthropicCallParams{
			Endpoint: endpoint,
			Token:    token,
			Request: buildAnthropicRequest(
				modelMeta.claudeRunnerModelValue, prompt,
				buildAnthropicSystemPrompt(schemaDoc), 0),
		})
		if err != nil {
			return zero, err
		}
		return decodeAPIResponse[T](raw)

	case apiKindOllama:
		endpoint := cfg.GetOllamaEndpoint()
		if endpoint == "" {
			endpoint = GetOllamaAutoDetectedEndpoint()
		}
		if endpoint == "" {
			return zero, fmt.Errorf("%w: ollama endpoint not configured", ErrMissingToken)
		}
		raw, err := callOpenAI(timeoutCtx, openaiCallParams{
			Endpoint: strings.TrimRight(endpoint, "/") + "/v1/chat/completions",
			Request:  buildOpenAIRequest(bareID, prompt, buildAnthropicSystemPrompt(schemaDoc), 0),
		})
		if err != nil {
			return zero, err
		}
		return decodeAPIResponse[T](raw)
	}
	return zero, fmt.Errorf("unknown api kind: %v", kind)
}

func classifyAPITarget(cfg *config.Config, bareID string) (apiKind, classifyAPIModelMeta, error) {
	mm := classifyAPIModelMeta{}
	for _, id := range GetOllamaModels() {
		if id == bareID {
			return apiKindOllama, mm, nil
		}
	}
	model, ok := models.LookupForAPI(bareID)
	if !ok {
		return apiKindUnknown, mm, fmt.Errorf("%w: %s", ErrModelNotFound, bareID)
	}
	mm.provider = model.Provider
	if spec, ok := model.RunnerFor("claude"); ok {
		mm.claudeRunnerModelValue = spec.ModelValue
		mm.claudeRunnerEndpoint = spec.Endpoint
	}
	if model.Provider == "anthropic" {
		return apiKindAnthropic, mm, nil
	}
	if mm.claudeRunnerModelValue != "" {
		return apiKindThirdPartyClaudeHarness, mm, nil
	}
	return apiKindUnknown, mm, fmt.Errorf("%w: %s (provider %q has no claude runner)",
		ErrNotImplemented, bareID, model.Provider)
}

func thirdPartyAnthropicAuth(cfg *config.Config, mm classifyAPIModelMeta) (string, string, error) {
	_ = cfg
	secrets, err := config.GetProviderSecrets(mm.provider)
	if err != nil {
		return "", "", err
	}
	for _, key := range []string{"api_key", "ANTHROPIC_API_KEY"} {
		if v, ok := secrets[key]; ok && v != "" {
			return v, chooseEndpoint(mm), nil
		}
	}
	for _, v := range secrets {
		if v != "" {
			return v, chooseEndpoint(mm), nil
		}
	}
	return "", "", ErrMissingToken
}

func chooseEndpoint(mm classifyAPIModelMeta) string {
	if mm.claudeRunnerEndpoint != "" {
		return strings.TrimRight(mm.claudeRunnerEndpoint, "/") + "/v1/messages"
	}
	return "https://api.anthropic.com/v1/messages"
}

// decodeAPIResponse uses the shared oneshotdecode path so CLI and direct-HTTP
// transports return the same error semantics. On decode failure the caller
// gets *oneshotdecode.InvalidResponseError, which satisfies
// errors.Is(err, oneshot.ErrInvalidResponse) just like the CLI path.
func decodeAPIResponse[T any](raw string) (T, error) {
	var zero T
	out, err := oneshotdecode.Decode[T](raw)
	if err != nil {
		return zero, &oneshotdecode.InvalidResponseError{Raw: raw, Err: err}
	}
	return out, nil
}

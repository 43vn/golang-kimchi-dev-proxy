// Package signature provides signature detection and compatibility utilities.
package signature

import "strings"

type SignatureProvider string

const (
	SignatureProviderUnknown      SignatureProvider = "unknown"
	SignatureProviderClaude       SignatureProvider = "claude"
	SignatureProviderGemini       SignatureProvider = "gemini"
	SignatureProviderGeminiBypass SignatureProvider = "gemini_bypass"
	SignatureProviderGPT          SignatureProvider = "gpt"
)

// SignatureProviderFromModelName maps common model names to the provider family.
func SignatureProviderFromModelName(modelName string) SignatureProvider {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	switch {
	case strings.Contains(lower, "claude"):
		return SignatureProviderClaude
	case strings.Contains(lower, "gemini"):
		return SignatureProviderGemini
	case strings.Contains(lower, "gpt"),
		strings.Contains(lower, "openai"),
		strings.Contains(lower, "codex"),
		strings.HasPrefix(lower, "o1"),
		strings.HasPrefix(lower, "o3"),
		strings.HasPrefix(lower, "o4"):
		return SignatureProviderGPT
	default:
		return SignatureProviderUnknown
	}
}

// DetectSignatureProvider classifies the provider family that can replay rawSignature.
func DetectSignatureProvider(rawSignature string) SignatureProvider {
	sig := strings.TrimSpace(rawSignature)
	if sig == "" {
		return SignatureProviderUnknown
	}

	// Check for provider prefix
	if prefix, _, ok := splitSignatureProviderPrefix(sig); ok {
		return prefix
	}

	// Default detection based on signature characteristics
	if strings.Contains(sig, "#") {
		return SignatureProviderUnknown
	}

	return SignatureProviderUnknown
}

func splitSignatureProviderPrefix(rawSignature string) (SignatureProvider, string, bool) {
	prefix, rest, ok := strings.Cut(strings.TrimSpace(rawSignature), "#")
	if !ok {
		return SignatureProviderUnknown, rawSignature, false
	}
	provider := signatureProviderFromCachePrefix(prefix)
	if provider == SignatureProviderUnknown {
		return SignatureProviderUnknown, rawSignature, false
	}
	return provider, strings.TrimSpace(rest), true
}

func signatureProviderFromCachePrefix(prefix string) SignatureProvider {
	switch strings.ToLower(strings.TrimSpace(prefix)) {
	case "claude", "anthropic":
		return SignatureProviderClaude
	case "gemini", "google":
		return SignatureProviderGemini
	case "openai", "gpt", "codex":
		return SignatureProviderGPT
	default:
		return SignatureProviderUnknown
	}
}

// CompatibleSignatureForProvider returns a replayable provider-native signature for targetProvider.
func CompatibleSignatureForProvider(targetProvider SignatureProvider, rawSignature string) (string, bool) {
	detected := DetectSignatureProvider(rawSignature)
	if signatureProviderMatchesTarget(targetProvider, detected) {
		payload := SignaturePayloadWithoutProviderPrefix(rawSignature)
		return payload, true
	}
	return "", false
}

func signatureProviderMatchesTarget(target, detected SignatureProvider) bool {
	switch target {
	case SignatureProviderGemini:
		return detected == SignatureProviderGemini || detected == SignatureProviderGeminiBypass
	case SignatureProviderClaude:
		return detected == SignatureProviderClaude
	case SignatureProviderGPT:
		return detected == SignatureProviderGPT
	default:
		return false
	}
}

// SignaturePayloadWithoutProviderPrefix strips the provider cache prefix.
func SignaturePayloadWithoutProviderPrefix(rawSignature string) string {
	if _, unprefixed, ok := splitSignatureProviderPrefix(rawSignature); ok {
		return unprefixed
	}
	return strings.TrimSpace(rawSignature)
}

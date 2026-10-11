package engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/freality/dexter/pkg/engine/ir"
)

// hashSecrets walks every resource in doc's properties (sorted order for
// determinism) and adds a "sha256" field to each secret token
// ({$dex_type: "secret", ...}) containing HMAC-SHA256(key, plaintext).
func hashSecrets(doc *ir.Document, env map[string]string, key []byte) error {
	for _, name := range doc.Names() {
		r := doc.Resources[name]
		if err := hashProps(r.Properties, key, env); err != nil {
			return fmt.Errorf("resource %q: %w", name, err)
		}
		doc.Resources[name] = r
	}
	return nil
}

// hashProps recursively walks props, hashing every secret token.
func hashProps(props map[string]any, key []byte, env map[string]string) error {
	if _, ok := props["$dex_type"]; ok {
		return hashToken(props, key, env)
	}
	for _, v := range props {
		switch val := v.(type) {
		case map[string]any:
			if err := hashProps(val, key, env); err != nil {
				return err
			}
		case []any:
			for _, item := range val {
				if m, ok := item.(map[string]any); ok {
					if err := hashProps(m, key, env); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// hashToken computes sha256 = HMAC-SHA256(key, env[token.key]) in-place.
func hashToken(token map[string]any, key []byte, env map[string]string) error {
	k, _ := token["key"].(string)
	plaintext, ok := env[k]
	if !ok {
		return fmt.Errorf("secret %q is not set (.env or environment)", k)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(plaintext))
	token["sha256"] = hex.EncodeToString(mac.Sum(nil))
	return nil
}

// resolveSecrets deep-copies props and replaces every secret token map with
// its plaintext string value from env.
func resolveSecrets(props map[string]any, env map[string]string, key []byte) (map[string]any, error) {
	copy := deepCopyMap(props)
	if err := resolveAll(copy, env); err != nil {
		return nil, err
	}
	return copy, nil
}

// resolveAll walks v replacing every secret token map with plaintext.
func resolveAll(v any, env map[string]string) error {
	switch val := v.(type) {
	case map[string]any:
		for k, item := range val {
			if m, ok := item.(map[string]any); ok && m["$dex_type"] == "secret" {
				pt, err := plaintextFromEnv(m, env)
				if err != nil {
					return err
				}
				val[k] = pt
			} else {
				if err := resolveAll(item, env); err != nil {
					return err
				}
			}
		}
	case []any:
		for i, item := range val {
			if m, ok := item.(map[string]any); ok && m["$dex_type"] == "secret" {
				pt, err := plaintextFromEnv(m, env)
				if err != nil {
					return err
				}
				val[i] = pt
			} else {
				if err := resolveAll(item, env); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func plaintextFromEnv(token map[string]any, env map[string]string) (string, error) {
	k, _ := token["key"].(string)
	pt, ok := env[k]
	if !ok {
		return "", fmt.Errorf("secret %q is not set (.env or environment)", k)
	}
	return pt, nil
}

// resolveSecretsWithCurrent is like resolveSecrets but for the "current"
// side of an update: tokens whose stored sha256 matches HMAC(key, env[key])
// are resolved to plaintext; tokens without a match are left as-is.
func resolveSecretsWithCurrent(props map[string]any, env map[string]string, key []byte) (map[string]any, error) {
	copy := deepCopyMap(props)
	resolveAllWithCurrent(copy, env, key)
	return copy, nil
}

// resolveAllWithCurrent walks v resolving tokens whose hash matches.
func resolveAllWithCurrent(v any, env map[string]string, key []byte) error {
	// Never returns an error — tryResolve reports mismatch by returning false, not an error.
	switch val := v.(type) {
	case map[string]any:
		for k, item := range val {
			if m, ok := item.(map[string]any); ok && m["$dex_type"] == "secret" {
				if pt, matched := tryResolve(m, env, key); matched {
					val[k] = pt
				}
			} else {
				resolveAllWithCurrent(item, env, key)
			}
		}
	case []any:
		for i, item := range val {
			if m, ok := item.(map[string]any); ok && m["$dex_type"] == "secret" {
				if pt, matched := tryResolve(m, env, key); matched {
					val[i] = pt
				}
			} else {
				resolveAllWithCurrent(item, env, key)
			}
		}
	}
	return nil
}

// tryResolve returns the plaintext value if the stored sha256 matches
// HMAC(key, env[key]), or false if the hash doesn't match.
func tryResolve(token map[string]any, env map[string]string, key []byte) (string, bool) {
	envKey, _ := token["key"].(string)
	storedHash, _ := token["sha256"].(string)
	plaintext, ok := env[envKey]
	if !ok {
		return "", false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(plaintext))
	expected := hex.EncodeToString(mac.Sum(nil))
	if storedHash != expected {
		return "", false
	}
	return plaintext, true
}

// deepCopyMap returns a deep copy of m. Values are the JSON-ish types that
// appear in resource properties: map[string]any, []any, string, float64, bool, nil.
func deepCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			out[k] = deepCopyMap(val)
		case []any:
			cp := make([]any, len(val))
			for i, item := range val {
				if m, ok := item.(map[string]any); ok {
					cp[i] = deepCopyMap(m)
				} else {
					cp[i] = item
				}
			}
			out[k] = cp
		default:
			out[k] = val
		}
	}
	return out
}

// checkLeak verifies that no string value in outputs contains any resolved
// secret value as a substring. Returns an error naming the first leak.
func checkLeak(outputs map[string]any, resolvedSecrets map[string]string) error {
	if outputs == nil {
		return nil
	}
	secrets := make([]string, 0, len(resolvedSecrets))
	names := make([]string, 0, len(resolvedSecrets))
	for k, v := range resolvedSecrets {
		if v != "" {
			secrets = append(secrets, v)
			names = append(names, k)
		}
	}
	var walk func(v any) error
	walk = func(v any) error {
		switch val := v.(type) {
		case string:
			for i, secret := range secrets {
				if len(secret) > 0 && containsString(val, secret) {
					return fmt.Errorf("output leaks secret %q", names[i])
				}
			}
		case map[string]any:
			for _, item := range val {
				if err := walk(item); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range val {
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(outputs)
}

func containsString(s, substr string) bool {
	if substr == "" {
		return false
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

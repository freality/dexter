package engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine/ir"
)

func secretToken(key string) map[string]any {
	return map[string]any{"$dex_type": "secret", "provider": "env", "key": key}
}

func TestHashSecretsAddsSHA256(t *testing.T) {
	key := make([]byte, 32)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"image": "img",
			"env": map[string]any{
				"PASSWORD": secretToken("DB_PWD"),
			},
		}},
	}}
	env := map[string]string{"DB_PWD": "supersecret"}

	if err := hashSecrets(doc, env, key); err != nil {
		t.Fatalf("hashSecrets() error = %v", err)
	}
	tok := doc.Resources["svc"].Properties["env"].(map[string]any)["PASSWORD"].(map[string]any)
	sha, ok := tok["sha256"]
	if !ok || sha == "" {
		t.Fatal("sha256 field not added")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("supersecret"))
	want := hex.EncodeToString(mac.Sum(nil))
	if sha != want {
		t.Fatalf("sha256 = %q, want %q", sha, want)
	}
}

func TestHashSecretsMissingEnv(t *testing.T) {
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"env": map[string]any{"PASSWORD": secretToken("MISSING")},
		}},
	}}
	err := hashSecrets(doc, map[string]string{"OTHER": "x"}, make([]byte, 32))
	if err == nil {
		t.Fatal("hashSecrets() error = nil, want missing env error")
	}
	if err.Error() != `resource "svc": secret "MISSING" is not set (.env or environment)` {
		t.Fatalf("error = %q, want resource+key prefix", err.Error())
	}
}

func TestHashSecretsDeterministic(t *testing.T) {
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"env": map[string]any{"PASSWORD": secretToken("DB_PWD")},
		}},
	}}
	env := map[string]string{"DB_PWD": "mypass"}
	key := []byte("01234567890123456789012345678901")

	if err := hashSecrets(doc, env, key); err != nil {
		t.Fatalf("hashSecrets() error = %v", err)
	}
	hash1 := doc.Resources["svc"].Properties["env"].(map[string]any)["PASSWORD"].(map[string]any)["sha256"]

	// Second run with different key
	doc2 := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"env": map[string]any{"PASSWORD": secretToken("DB_PWD")},
		}},
	}}
	if err := hashSecrets(doc2, env, key); err != nil {
		t.Fatalf("hashSecrets() error = %v", err)
	}
	hash2 := doc2.Resources["svc"].Properties["env"].(map[string]any)["PASSWORD"].(map[string]any)["sha256"]

	if hash1 != hash2 {
		t.Fatal("same key + same value produced different hashes")
	}

	// Different key should produce different hash
	key2 := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	doc3 := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"env": map[string]any{"PASSWORD": secretToken("DB_PWD")},
		}},
	}}
	if err := hashSecrets(doc3, env, key2); err != nil {
		t.Fatalf("hashSecrets() error = %v", err)
	}
	hash3 := doc3.Resources["svc"].Properties["env"].(map[string]any)["PASSWORD"].(map[string]any)["sha256"]

	if hash1 == hash3 {
		t.Fatal("different keys produced same hash")
	}
}

func TestResolveSecretsReplacesToken(t *testing.T) {
	props := map[string]any{
		"image": "img",
		"env": map[string]any{
			"PASSWORD": secretToken("DB_PWD"),
		},
	}
	env := map[string]string{"DB_PWD": "supersecret"}

	resolved, err := resolveSecrets(props, env, make([]byte, 32))
	if err != nil {
		t.Fatalf("resolveSecrets() error = %v", err)
	}
	envMap := resolved["env"].(map[string]any)
	if envMap["PASSWORD"] != "supersecret" {
		t.Fatalf("PASSWORD = %q, want supersecret", envMap["PASSWORD"])
	}
	// Original should be unchanged
	origEnv := props["env"].(map[string]any)
	if _, ok := origEnv["PASSWORD"].(map[string]any); !ok {
		t.Fatal("original props were mutated")
	}
}

func TestResolveSecretsMissingKey(t *testing.T) {
	props := map[string]any{
		"env": map[string]any{"PASSWORD": secretToken("MISSING")},
	}
	_, err := resolveSecrets(props, map[string]string{}, make([]byte, 32))
	if err == nil {
		t.Fatal("resolveSecrets() error = nil, want missing env error")
	}
}

func TestResolveSecretsWithCurrentMatch(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	env := map[string]string{"DB_PWD": "password123"}

	// Build a token with known sha256
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("password123"))
	expectedHash := hex.EncodeToString(mac.Sum(nil))

	props := map[string]any{
		"env": map[string]any{
			"PASSWORD": map[string]any{
				"$dex_type": "secret", "provider": "env", "key": "DB_PWD",
				"sha256": expectedHash,
			},
		},
	}
	resolved, err := resolveSecretsWithCurrent(props, env, key)
	if err != nil {
		t.Fatalf("resolveSecretsWithCurrent() error = %v", err)
	}
	envMap := resolved["env"].(map[string]any)
	if envMap["PASSWORD"] != "password123" {
		t.Fatalf("PASSWORD = %q, want password123", envMap["PASSWORD"])
	}
}

func TestResolveSecretsWithCurrentNoMatch(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	env := map[string]string{"DB_PWD": "password123"}

	// Wrong hash means the current env is different from desired
	props := map[string]any{
		"env": map[string]any{
			"PASSWORD": map[string]any{
				"$dex_type": "secret", "provider": "env", "key": "DB_PWD",
				"sha256": "deadbeef",
			},
		},
	}
	resolved, err := resolveSecretsWithCurrent(props, env, key)
	if err != nil {
		t.Fatalf("resolveSecretsWithCurrent() error = %v", err)
	}
	envMap := resolved["env"].(map[string]any)
	tok, ok := envMap["PASSWORD"].(map[string]any)
	if !ok {
		t.Fatal("token should have been left as-is (not resolved)")
	}
	if tok["sha256"] != "deadbeef" {
		t.Fatalf("sha256 = %q, want deadbeef (unchanged)", tok["sha256"])
	}
}

func TestCheckLeakDetectsSecretInOutput(t *testing.T) {
	secrets := map[string]string{"DB_PWD": "supersecret"}
	outputs := map[string]any{"id": "container-using-supersecret"}
	if err := checkLeak(outputs, secrets); err == nil {
		t.Fatal("checkLeak() error = nil, want leak detection")
	}
}

func TestCheckLeakNoLeak(t *testing.T) {
	secrets := map[string]string{"DB_PWD": "supersecret"}
	outputs := map[string]any{"id": "container-abc123"}
	if err := checkLeak(outputs, secrets); err != nil {
		t.Fatalf("checkLeak() error = %v, want nil", err)
	}
}

func TestCheckLeakNilOutputs(t *testing.T) {
	if err := checkLeak(nil, map[string]string{"x": "y"}); err != nil {
		t.Fatalf("checkLeak(nil) error = %v, want nil", err)
	}
}

func TestResolveSecretsWithMultipleTokens(t *testing.T) {
	props := map[string]any{
		"env": map[string]any{
			"USER":     secretToken("DB_USER"),
			"PASSWORD": secretToken("DB_PWD"),
		},
	}
	env := map[string]string{"DB_USER": "admin", "DB_PWD": "secret123"}
	resolved, err := resolveSecrets(props, env, make([]byte, 32))
	if err != nil {
		t.Fatalf("resolveSecrets() error = %v", err)
	}
	envMap := resolved["env"].(map[string]any)
	if envMap["USER"] != "admin" {
		t.Fatalf("USER = %q, want admin", envMap["USER"])
	}
	if envMap["PASSWORD"] != "secret123" {
		t.Fatalf("PASSWORD = %q, want secret123", envMap["PASSWORD"])
	}
}

func TestHashPropsNestedInArray(t *testing.T) {
	props := map[string]any{
		"env": []any{"PLAIN=value", secretToken("DB_PWD")},
	}
	key := []byte("01234567890123456789012345678901")
	env := map[string]string{"DB_PWD": "secret123"}
	if err := hashProps(props, key, env); err != nil {
		t.Fatalf("hashProps() error = %v", err)
	}
	arr := props["env"].([]any)
	tok := arr[1].(map[string]any)
	if tok["sha256"] == "" {
		t.Fatal("token in array not hashed")
	}
}

func TestResolveSecretsNestedInArray(t *testing.T) {
	props := map[string]any{
		"env": []any{"PLAIN=value", secretToken("DB_PWD")},
	}
	env := map[string]string{"DB_PWD": "resolved-in-array"}
	resolved, err := resolveSecrets(props, env, make([]byte, 32))
	if err != nil {
		t.Fatalf("resolveSecrets() error = %v", err)
	}
	arr := resolved["env"].([]any)
	if arr[0] != "PLAIN=value" {
		t.Fatalf("arr[0] = %v, want PLAIN=value", arr[0])
	}
	if arr[1] != "resolved-in-array" {
		t.Fatalf("arr[1] = %v, want resolved-in-array", arr[1])
	}
}

func TestResolveSecretsWithCurrentArrayNested(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	env := map[string]string{"DB_PWD": "secret123"}
	macc := hmac.New(sha256.New, key)
	macc.Write([]byte("secret123"))
	expectedHash := hex.EncodeToString(macc.Sum(nil))

	props := map[string]any{
		"env": []any{
			"PLAIN=value",
			map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD", "sha256": expectedHash},
		},
	}
	resolved, err := resolveSecretsWithCurrent(props, env, key)
	if err != nil {
		t.Fatalf("resolveSecretsWithCurrent() error = %v", err)
	}
	arr := resolved["env"].([]any)
	if arr[1] != "secret123" {
		t.Fatalf("arr[1] = %v, want secret123", arr[1])
	}
}

func TestCheckLeakNestedOutput(t *testing.T) {
	secrets := map[string]string{"DB_PWD": "secret"}
	outputs := map[string]any{"nested": map[string]any{"inner": "contains-secret-value"}}
	if err := checkLeak(outputs, secrets); err == nil {
		t.Fatal("checkLeak() error = nil, want leak detection in nested output")
	}
}

func TestContainsStringEdgeCases(t *testing.T) {
	if containsString("abc", "") {
		t.Fatal("containsString('abc', '') = true, want false")
	}
	if containsString("", "a") {
		t.Fatal("containsString('', 'a') = true, want false")
	}
	if !containsString("abc", "abc") {
		t.Fatal("containsString('abc', 'abc') = false, want true")
	}
	if containsString("ab", "abc") {
		t.Fatal("containsString('ab', 'abc') = true, want false")
	}
}
func TestCheckLeakArrayOutput(t *testing.T) {
	secrets := map[string]string{"DB_PWD": "secret"}
	outputs := map[string]any{"list": []any{"safe", "contains-secret-value"}}
	if err := checkLeak(outputs, secrets); err == nil {
		t.Fatal("checkLeak() error = nil, want leak detection in array output")
	}
}

func TestCollectSecretsWithArrayNested(t *testing.T) {
	props := map[string]any{
		"env": []any{
			"PLAIN=value",
			map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD"},
		},
	}
	env := map[string]string{"DB_PWD": "supersecret"}
	result := collectSecrets(props, env)
	if result["DB_PWD"] != "supersecret" {
		t.Fatalf("collectSecrets() = %v, want supersecret for DB_PWD", result)
	}
}

func TestLoadOrCreateKeyReadError(t *testing.T) {
	dir := t.TempDir()
	dexDir := filepath.Join(dir, ".dex")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dexDir, "secret.key")
	// Create a directory at the key path so ReadFile fails with a non-NotExist error
	if err := os.Mkdir(path, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadOrCreateKey(dir)
	if err == nil {
		t.Fatal("loadOrCreateKey() error = nil, want error when path is a directory")
	}
}

func TestHashPropsInArrayMissingEnv(t *testing.T) {
	props := map[string]any{
		"env": []any{"PLAIN=value", secretToken("MISSING_KEY")},
	}
	err := hashProps(props, []byte("key"), map[string]string{})
	if err == nil {
		t.Fatal("hashProps() error = nil, want missing env error")
	}
}

func TestResolveAllWithNestedMap(t *testing.T) {
	props := map[string]any{
		"nested": map[string]any{
			"inner": "value",
		},
	}
	resolved, err := resolveSecrets(props, map[string]string{}, make([]byte, 32))
	if err != nil {
		t.Fatalf("resolveSecrets() error = %v", err)
	}
	inner := resolved["nested"].(map[string]any)
	if inner["inner"] != "value" {
		t.Fatalf("inner = %v, want value", inner["inner"])
	}
}

func TestResolveAllWithNestedArray(t *testing.T) {
	props := map[string]any{
		"items": []any{
			"plain",
			map[string]any{"name": "value"},
		},
	}
	resolved, err := resolveSecrets(props, map[string]string{}, make([]byte, 32))
	if err != nil {
		t.Fatalf("resolveSecrets() error = %v", err)
	}
	arr := resolved["items"].([]any)
	if arr[0] != "plain" || arr[1].(map[string]any)["name"] != "value" {
		t.Fatal("unexpected resolved values")
	}
}

func TestTryResolveMissingEnvKey(t *testing.T) {
	token := map[string]any{
		"$dex_type": "secret", "provider": "env", "key": "MISSING_KEY",
	}
	_, matched := tryResolve(token, map[string]string{}, make([]byte, 32))
	if matched {
		t.Fatal("tryResolve() matched = true, want false when key missing from env")
	}
}

func TestResolveAllWithCurrentNestedWalk(t *testing.T) {
	props := map[string]any{
		"nested": map[string]any{
			"inner": "value",
			"items": []any{
				"plain",
				map[string]any{"name": "val"},
			},
		},
	}
	resolved, err := resolveSecretsWithCurrent(props, map[string]string{}, make([]byte, 32))
	if err != nil {
		t.Fatalf("resolveSecretsWithCurrent() error = %v", err)
	}
	nested := resolved["nested"].(map[string]any)
	if nested["inner"] != "value" {
		t.Fatal("unexpected resolved value")
	}
}

func TestDeepCopyMapNestedArray(t *testing.T) {
	props := map[string]any{
		"items": []any{
			map[string]any{"a": 1},
			"str",
			42,
			true,
			nil,
		},
		"plain": "text",
	}
	copy := deepCopyMap(props)
	arr := copy["items"].([]any)
	if arr[0].(map[string]any)["a"] != 1 {
		t.Fatalf("arr[0][a] = %v, want 1", arr[0].(map[string]any)["a"])
	}
	if arr[1] != "str" {
		t.Fatalf("arr[1] = %v, want str", arr[1])
	}
	if arr[2] != 42 {
		t.Fatalf("arr[2] = %v, want 42", arr[2])
	}
	if arr[3] != true {
		t.Fatalf("arr[3] = %v, want true", arr[3])
	}
	if arr[4] != nil {
		t.Fatalf("arr[4] = %v, want nil", arr[4])
	}
}

func TestResolveSecretsArraySecretMissingEnv(t *testing.T) {
	props := map[string]any{
		"env": []any{"PLAIN=value", secretToken("MISSING_KEY")},
	}
	_, err := resolveSecrets(props, map[string]string{}, make([]byte, 32))
	if err == nil {
		t.Fatal("resolveSecrets() error = nil, want missing env error from array secret")
	}
}

func TestResolveSecretsArrayNestedMissingEnv(t *testing.T) {
	props := map[string]any{
		"items": []any{
			"str",
			map[string]any{"secret": secretToken("MISSING_KEY")},
		},
	}
	_, err := resolveSecrets(props, map[string]string{}, make([]byte, 32))
	if err == nil {
		t.Fatal("resolveSecrets() error = nil, want missing env error from nested array item")
	}
}

func TestHashPropsNestedInArrayMissingEnv(t *testing.T) {
	props := map[string]any{
		"env": []any{"PLAIN=value", map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD"}},
	}
	key := []byte("01234567890123456789012345678901")
	err := hashProps(props, key, map[string]string{"OTHER": "x"})
	if err == nil {
		t.Fatal("hashProps() error = nil, want missing key error")
	}
	if err.Error() != `secret "DB_PWD" is not set (.env or environment)` {
		t.Fatalf("hashProps() error = %q, want missing key error", err)
	}
}

func TestResolveSecretsMissingKeyInArray(t *testing.T) {
	props := map[string]any{
		"env": []any{"PLAIN=value", map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD"}},
	}
	_, err := resolveSecrets(props, map[string]string{}, make([]byte, 32))
	if err == nil {
		t.Fatal("resolveSecrets() error = nil, want missing key error")
	}
	if err.Error() != `secret "DB_PWD" is not set (.env or environment)` {
		t.Fatalf("resolveSecrets() error = %q, want missing key error", err)
	}
}

func TestResolveSecretsNestedMapMissingKey(t *testing.T) {
	props := map[string]any{
		"config": map[string]any{
			"database": map[string]any{"password": map[string]any{
				"$dex_type": "secret", "provider": "env", "key": "DB_PWD",
			}},
		},
	}
	_, err := resolveSecrets(props, map[string]string{}, make([]byte, 32))
	if err == nil {
		t.Fatal("resolveSecrets() error = nil, want missing key error")
	}
	if !strings.Contains(err.Error(), `secret "DB_PWD" is not set`) {
		t.Fatalf("resolveSecrets() error = %q, want missing key error", err)
	}
}

func TestResolveSecretsArrayNestedNonSecretMapMissingKey(t *testing.T) {
	props := map[string]any{
		"env": []any{
			map[string]any{"inner": map[string]any{
				"$dex_type": "secret", "provider": "env", "key": "DB_PWD",
			}},
		},
	}
	_, err := resolveSecrets(props, map[string]string{}, make([]byte, 32))
	if err == nil {
		t.Fatal("resolveSecrets() error = nil, want missing key error")
	}
	if !strings.Contains(err.Error(), `secret "DB_PWD" is not set`) {
		t.Fatalf("resolveSecrets() error = %q, want missing key error", err)
	}
}

func TestResolveSecretsWithCurrentMissingEnvKey(t *testing.T) {
	props := map[string]any{
		"env": map[string]any{"PASSWORD": map[string]any{
			"$dex_type": "secret", "provider": "env", "key": "MISSING", "sha256": "abc",
		}},
	}
	key := []byte("01234567890123456789012345678901")
	resolved, err := resolveSecretsWithCurrent(props, map[string]string{}, key)
	if err != nil {
		t.Fatalf("resolveSecretsWithCurrent() error = %v, want nil (no error on miss)", err)
	}
	envMap := resolved["env"].(map[string]any)
	tok, ok := envMap["PASSWORD"].(map[string]any)
	if !ok {
		t.Fatal("token should have been left as-is (not resolved)")
	}
	if tok["sha256"] != "abc" {
		t.Fatalf("sha256 = %q, want abc (unchanged)", tok["sha256"])
	}
}

func TestDeepCopyArrayWithNestedMap(t *testing.T) {
	prev := map[string]any{
		"items": []any{
			map[string]any{"nested": "value"},
			42,
		},
	}
	copy := deepCopyMap(prev)
	arr := copy["items"].([]any)
	m := arr[0].(map[string]any)
	if m["nested"] != "value" {
		t.Fatalf("arr[0][nested] = %v, want value", arr[0].(map[string]any)["nested"])
	}
	m["nested"] = "mutated"
	if prev["items"].([]any)[0].(map[string]any)["nested"] != "value" {
		t.Fatal("deep copy was not independent from original")
	}
	if arr[1] != 42 {
		t.Fatalf("arr[1] = %v, want 42", arr[1])
	}
}

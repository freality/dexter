package engine

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine/diff"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

// --- helpers ----------------------------------------------------------------

func resolveOne(plugin string, a Adapter) func(string) (Adapter, error) {
	return func(p string) (Adapter, error) {
		if p != plugin {
			return nil, fmt.Errorf("no adapter registered for plugin %q", p)
		}
		return a, nil
	}
}

// --- Secrets integration tests ------------------------------------------------

func TestEnginePlanMissingSecretKeyInEnv(t *testing.T) {
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"env": map[string]any{
				"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD"},
			},
		}},
	}}
	e := New(Options{
		Synth: fakeSynth(t, doc),
		Store: &fakeStore{st: state.New()},
		Env:   map[string]string{},
	})
	_, err := e.Plan(context.Background(), entryDir)
	if err == nil {
		t.Fatal("Plan() error = nil, want missing env error")
	}
	if !strings.Contains(err.Error(), `secret "DB_PWD" is not set`) {
		t.Fatalf("Plan() error = %v, want missing secret error mentioning DB_PWD", err)
	}
}

func TestEngineCreateWithSecret(t *testing.T) {
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"image": "img",
			"env": map[string]any{
				"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD"},
			},
		}},
	}}

	var capturedProps map[string]any
	adapter := adapterFunc{
		create: func(_ context.Context, _ string, r ir.Resource, _ func(Event)) (map[string]any, error) {
			capturedProps = r.Properties
			return map[string]any{"id": "abc"}, nil
		},
	}
	registry := NewRegistry(resolveOne("docker", adapter))

	store := &fakeStore{st: state.New()}
	e := New(Options{
		Synth:    fakeSynth(t, doc),
		Store:    store,
		Lock:     &fakeLocker{},
		Registry: registry,
		Env:      map[string]string{"DB_PWD": "supersecret"},
	})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Adapter should receive plaintext, not the token map
	env := capturedProps["env"].(map[string]any)
	if env["PASSWORD"] != "supersecret" {
		t.Fatalf("adapter received PASSWORD = %v, want supersecret", env["PASSWORD"])
	}

	// State should store hashed properties, not plaintext
	final := store.saves[len(store.saves)-1]
	storedEnv := final.Resources["svc"].Properties["env"].(map[string]any)
	tok, ok := storedEnv["PASSWORD"].(map[string]any)
	if !ok {
		t.Fatal("state stored plaintext instead of token")
	}
	if tok["$dex_type"] != "secret" {
		t.Fatal("state token missing $dex_type")
	}
	if tok["sha256"] == "" {
		t.Fatal("state token missing sha256")
	}
}

func TestEngineUpdateWithUnchangedSecret(t *testing.T) {
	// Compute the expected sha256 for the current state's token
	key := deriveKey()
	env := map[string]string{"DB_PWD": "supersecret"}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("supersecret"))
	sha := hex.EncodeToString(mac.Sum(nil))

	current := state.New()
	current.Project = "p"
	current.Resources["svc"] = state.Resource{
		Type: "container", Plugin: "docker",
		Properties: map[string]any{
			"image": "v1",
			"env": map[string]any{
				"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD", "sha256": sha},
			},
		},
	}

	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"image": "v2", // different image forces an update change
			"env": map[string]any{
				"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD"},
			},
		}},
	}}

	var capturedDesiredProps, capturedCurrentProps map[string]any
	adapter := &updateCaptureAdapter{
		update: func(_ context.Context, _ string, desired, cur map[string]any) (map[string]any, error) {
			capturedDesiredProps = desired
			capturedCurrentProps = cur
			return map[string]any{"id": "abc"}, nil
		},
	}
	registry := NewRegistry(resolveOne("docker", adapter))

	store := &fakeStore{st: current}
	e := New(Options{
		Synth:    fakeSynth(t, desired),
		Store:    store,
		Lock:     &fakeLocker{},
		Registry: registry,
		Env:      env,
	})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Both desired and cur should be resolved to plaintext since the hash matches
	desiredEnv := capturedDesiredProps["env"].(map[string]any)
	if desiredEnv["PASSWORD"] != "supersecret" {
		t.Fatalf("desired PASSWORD = %v, want supersecret", desiredEnv["PASSWORD"])
	}
	curEnv := capturedCurrentProps["env"].(map[string]any)
	if curEnv["PASSWORD"] != "supersecret" {
		t.Fatalf("cur PASSWORD = %v, want supersecret", curEnv["PASSWORD"])
	}
}

func TestEngineUpdateWithChangedSecret(t *testing.T) {
	current := state.New()
	current.Project = "p"
	current.Resources["svc"] = state.Resource{
		Type: "container", Plugin: "docker",
		Properties: map[string]any{
			"image": "v1",
			"env": map[string]any{
				"PASSWORD": map[string]any{
					"$dex_type": "secret", "provider": "env", "key": "DB_PWD", "sha256": "oldhash",
				},
			},
		},
	}

	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"image": "v2", // different image forces an update change
			"env": map[string]any{
				"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD"},
			},
		}},
	}}

	var capturedDesiredProps, capturedCurrentProps map[string]any
	adapter := &updateCaptureAdapter{
		update: func(_ context.Context, _ string, desired, cur map[string]any) (map[string]any, error) {
			capturedDesiredProps = desired
			capturedCurrentProps = cur
			return map[string]any{"id": "abc"}, nil
		},
	}
	registry := NewRegistry(resolveOne("docker", adapter))

	store := &fakeStore{st: current}
	e := New(Options{
		Synth:    fakeSynth(t, desired),
		Store:    store,
		Lock:     &fakeLocker{},
		Registry: registry,
		Env:      map[string]string{"DB_PWD": "newsecret"},
	})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Desired should be resolved to plaintext
	desiredEnv := capturedDesiredProps["env"].(map[string]any)
	if desiredEnv["PASSWORD"] != "newsecret" {
		t.Fatalf("desired PASSWORD = %v, want newsecret", desiredEnv["PASSWORD"])
	}

	// Current should remain as a hashed token (hash doesn't match new env value)
	curEnv := capturedCurrentProps["env"].(map[string]any)
	_, isMap := curEnv["PASSWORD"].(map[string]any)
	if !isMap {
		t.Fatal("cur PASSWORD was resolved to plaintext despite hash mismatch")
	}
}

func TestEngineApplyLeakGuard(t *testing.T) {
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"image": "img",
			"env": map[string]any{
				"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD"},
			},
		}},
	}}

	adapter := adapterFunc{
		create: func(_ context.Context, _ string, _ ir.Resource, _ func(Event)) (map[string]any, error) {
			return map[string]any{"id": "contains-supersecret-value"}, nil
		},
	}
	registry := NewRegistry(resolveOne("docker", adapter))

	store := &fakeStore{st: state.New()}
	e := New(Options{
		Synth:    fakeSynth(t, doc),
		Store:    store,
		Lock:     &fakeLocker{},
		Registry: registry,
		Env:      map[string]string{"DB_PWD": "supersecret"},
	})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("Apply() error = nil, want leak guard error")
	}
	if !strings.Contains(err.Error(), "leaks secret") {
		t.Fatalf("Apply() error = %v, want leak guard error", err)
	}
}

func TestEngineCreateWithMultipleSecrets(t *testing.T) {
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"env": map[string]any{
				"USER":     map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_USER"},
				"PASSWORD": map[string]any{"$dex_type": "secret", "provider": "env", "key": "DB_PWD"},
			},
		}},
	}}

	var capturedProps map[string]any
	adapter := adapterFunc{
		create: func(_ context.Context, _ string, r ir.Resource, _ func(Event)) (map[string]any, error) {
			capturedProps = r.Properties
			return map[string]any{"id": "abc"}, nil
		},
	}
	registry := NewRegistry(resolveOne("docker", adapter))

	store := &fakeStore{st: state.New()}
	e := New(Options{
		Synth:    fakeSynth(t, doc),
		Store:    store,
		Lock:     &fakeLocker{},
		Registry: registry,
		Env:      map[string]string{"DB_USER": "admin", "DB_PWD": "secret123"},
	})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	env := capturedProps["env"].(map[string]any)
	if env["USER"] != "admin" {
		t.Fatalf("adapter received USER = %v, want admin", env["USER"])
	}
	if env["PASSWORD"] != "secret123" {
		t.Fatalf("adapter received PASSWORD = %v, want secret123", env["PASSWORD"])
	}
}

// updateCaptureAdapter records properties passed to Update.
type updateCaptureAdapter struct {
	update func(context.Context, string, map[string]any, map[string]any) (map[string]any, error)
}

func (a *updateCaptureAdapter) Create(_ context.Context, _ string, _ ir.Resource, _ func(Event)) (map[string]any, error) {
	return nil, nil
}

func (a *updateCaptureAdapter) Update(ctx context.Context, name string, r ir.Resource, cur state.Resource, _ func(Event)) (map[string]any, error) {
	return a.update(ctx, name, r.Properties, cur.Properties)
}

func (a *updateCaptureAdapter) Delete(_ context.Context, _ string, _ state.Resource, _ func(Event)) error {
	return nil
}

// Ensure the unused imports are needed (cannot import diff in this file).

// deriveKey reads the secret key created by the Plan call in the test's entryDir.
func deriveKey() []byte {
	k, err := loadOrCreateKey(entryDir)
	if err != nil {
		panic(err)
	}
	return k
}

var _ = &diff.Plan{}

package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/krillinai/Clawee/server/internal/config"
)

func TestNewRuntimeResolvesSecrets(t *testing.T) {
	t.Setenv("TEST_BAILIAN_AK", "access-key")
	t.Setenv("TEST_BAILIAN_SECRET", "access-secret")

	runtime, err := NewRuntime(testKnowledgeConfig())
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if runtime.Provider == nil {
		t.Fatalf("runtime = %#v", runtime)
	}
}

func TestNewRuntimeUsesDirectlyConfiguredSecrets(t *testing.T) {
	cfg := testKnowledgeConfig()
	cfg.Bailian.AccessKeyID = "direct-access-key"
	cfg.Bailian.AccessKeySecret = "direct-access-secret"
	cfg.Bailian.AccessKeyIDRef = ""
	cfg.Bailian.AccessKeySecretRef = ""

	runtime, err := NewRuntime(cfg)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if runtime.Provider == nil {
		t.Fatalf("runtime = %#v", runtime)
	}
}

func TestNewRuntimeRejectsMissingSecret(t *testing.T) {
	t.Setenv("TEST_BAILIAN_AK", "access-key")
	if _, err := NewRuntime(testKnowledgeConfig()); err == nil {
		t.Fatal("NewRuntime() should reject a missing access key secret")
	}
}

func testKnowledgeConfig() config.KnowledgeConfig {
	return config.KnowledgeConfig{
		Enabled:      true,
		ProviderType: ProviderBailian,
		Bailian: config.BailianConfig{
			Endpoint:           "https://example.com",
			WorkspaceID:        "workspace",
			AccessKeyIDRef:     "TEST_BAILIAN_AK",
			AccessKeySecretRef: "TEST_BAILIAN_SECRET",
		},
	}
}

func TestRAGFlowRuntimeResolvesOnlySelectedCredentialWithoutNetwork(t *testing.T) {
	t.Setenv("FIXTURE_RAGFLOW_KEY", "fixture-key")
	t.Setenv("ALIBABA_CLOUD_ACCESS_KEY_ID", "")
	t.Setenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET", "")
	cfg := config.KnowledgeConfig{Enabled: true, ProviderType: " RAGFlow ", RAGFlow: config.RAGFlowConfig{BaseURL: "http://127.0.0.1:1", APIKeyRef: "FIXTURE_RAGFLOW_KEY"}}
	runtime, err := NewRuntime(cfg)
	if err != nil || runtime.Provider == nil || runtime.ProviderType != ProviderRAGFlow {
		t.Fatalf("runtime=%+v err=%v", runtime, err)
	}
	t.Setenv("FIXTURE_RAGFLOW_KEY", "")
	if _, err := NewRuntime(cfg); err == nil {
		t.Fatal("缺失Key必须失败")
	}
	cfg.Enabled = false
	if _, err := NewRuntime(cfg); err == nil || err.Error() != "knowledge is disabled" {
		t.Fatalf("禁用时不应解析Key: %v", err)
	}
}

func TestRAGFlowRuntimeAcceptsInlineAndReferencedKeys(t *testing.T) {
	t.Setenv("FIXTURE_RAGFLOW_KEY", " fixture-key ")
	for _, test := range []struct{ name, reference, key string }{
		{"直接填写", " ragflow-fixture-key ", "ragflow-fixture-key"},
		{"环境变量", " FIXTURE_RAGFLOW_KEY ", "fixture-key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+test.key {
					t.Error("未使用配置的 RAGFlow Key")
				}
				w.Write([]byte(`{"code":0,"data":{"docs":[]}}`))
			}))
			defer server.Close()
			cfg := config.KnowledgeConfig{Enabled: true, ProviderType: ProviderRAGFlow, RAGFlow: config.RAGFlowConfig{BaseURL: server.URL, APIKeyRef: test.reference}}
			runtime, err := NewRuntime(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.Provider.ListDocuments(context.Background(), "fixture-dataset"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRAGFlowRuntimeCredentialErrorsDoNotExposeConfiguredValue(t *testing.T) {
	t.Setenv("MISSING_RAGFLOW_KEY", "")
	for _, reference := range []string{"MISSING_RAGFLOW_KEY", "opaque-secret-with-dashes"} {
		cfg := config.KnowledgeConfig{Enabled: true, ProviderType: ProviderRAGFlow, RAGFlow: config.RAGFlowConfig{BaseURL: "http://127.0.0.1:1", APIKeyRef: reference}}
		_, err := NewRuntime(cfg)
		if err == nil || strings.Contains(err.Error(), reference) {
			t.Fatal("缺失或无效凭据必须返回不包含配置值的错误")
		}
	}
}

package service

import (
	"strings"
	"testing"

	"github.com/kleinai/backend/internal/model"
)

func TestValidateCatalogImagePriceRulesRequiresMatchingModel(t *testing.T) {
	raw := `[{"model_code":"other-model","mode":"t2i","resolution":"1K","unit_points":400}]`
	err := validateCatalogPricingConfiguration(model.ModelCatalogKindImage, "gpt-image-2", model.ModelCatalogPricingMatrix, raw)
	if err == nil || !strings.Contains(err.Error(), "model_code") {
		t.Fatalf("expected model_code validation error, got %v", err)
	}
}

func TestValidateCatalogImagePriceRulesAcceptsUsableMatrix(t *testing.T) {
	raw := `[{"model_code":"gpt-image-2","mode":"t2i","ratio_group":"standard","resolution":"1K","unit_points":400}]`
	if err := validateCatalogPricingConfiguration(model.ModelCatalogKindImage, "gpt-image-2", model.ModelCatalogPricingMatrix, raw); err != nil {
		t.Fatalf("expected valid image matrix, got %v", err)
	}
}

func TestValidateCatalogVideoPriceRulesRequiresPositiveUnitPoints(t *testing.T) {
	raw := `[{"model_code":"sora2","mode":"t2v","duration_sec":6,"quality":"standard","unit_points":0}]`
	err := validateCatalogPricingConfiguration(model.ModelCatalogKindVideo, "sora2", model.ModelCatalogPricingMatrix, raw)
	if err == nil || !strings.Contains(err.Error(), "unit_points") {
		t.Fatalf("expected unit_points validation error, got %v", err)
	}
}

func TestValidateCatalogPricingRejectsRulesForTextModel(t *testing.T) {
	raw := `[{"model_code":"mimo-v2.5-pro","mode":"t2i","resolution":"1K","unit_points":400}]`
	err := validateCatalogPricingConfiguration(model.ModelCatalogKindText, "mimo-v2.5-pro", model.ModelCatalogPricingToken, raw)
	if err == nil || !strings.Contains(err.Error(), "matrix") {
		t.Fatalf("expected text model price rule rejection, got %v", err)
	}
}

func TestValidateCatalogMatrixRequiresRules(t *testing.T) {
	err := validateCatalogPricingConfiguration(model.ModelCatalogKindImage, "gpt-image-2", model.ModelCatalogPricingMatrix, "")
	if err == nil || !strings.Contains(err.Error(), "矩阵计价") {
		t.Fatalf("expected matrix rules required error, got %v", err)
	}
}

func TestValidateCatalogDisabledMatrixAllowsEmptyRules(t *testing.T) {
	if err := validateCatalogPricingConfigurationForStatus(model.ModelCatalogKindImage, "gpt-image-2", model.ModelCatalogPricingMatrix, model.ModelCatalogStatusDisabled, ""); err != nil {
		t.Fatalf("expected disabled matrix model to allow empty rules, got %v", err)
	}
}

func TestValidateCatalogRejectsRulesOutsideMatrixPricing(t *testing.T) {
	raw := `[{"model_code":"gpt-image-2","mode":"t2i","resolution":"1K","unit_points":400}]`
	err := validateCatalogPricingConfiguration(model.ModelCatalogKindImage, "gpt-image-2", model.ModelCatalogPricingFixed, raw)
	if err == nil || !strings.Contains(err.Error(), "matrix") {
		t.Fatalf("expected non-matrix price rule rejection, got %v", err)
	}
}

func TestValidateCatalogRejectsMatrixPricingForTextModel(t *testing.T) {
	err := validateCatalogPricingConfiguration(model.ModelCatalogKindText, "mimo-v2.5-pro", model.ModelCatalogPricingMatrix, "")
	if err == nil || !strings.Contains(err.Error(), "文字/对话模型") {
		t.Fatalf("expected text matrix pricing rejection, got %v", err)
	}
}

func TestValidateCatalogRejectsTokenPricingForImageModel(t *testing.T) {
	err := validateCatalogPricingConfiguration(model.ModelCatalogKindImage, "gpt-image-2", model.ModelCatalogPricingToken, "")
	if err == nil || !strings.Contains(err.Error(), "图片/视频模型") {
		t.Fatalf("expected image token pricing rejection, got %v", err)
	}
}

func TestValidateCatalogParametersSchemaAcceptsControlsObject(t *testing.T) {
	raw := `{"controls":[{"key":"temperature","label":"温度","type":"number","min":0,"max":2,"step":0.1,"default":0.7},{"key":"quality","type":"select","modes":["image"],"options":[{"value":"high","label":"高清"}]}]}`
	if err := validateCatalogParametersSchema(model.ModelCatalogKindImage, raw); err != nil {
		t.Fatalf("expected valid parameter schema, got %v", err)
	}
}

func TestValidateCatalogParametersSchemaRejectsSensitiveKey(t *testing.T) {
	raw := `{"controls":[{"key":"api_key","label":"API Key","type":"text"}]}`
	err := validateCatalogParametersSchema(model.ModelCatalogKindText, raw)
	if err == nil || !strings.Contains(err.Error(), "敏感凭证字段") {
		t.Fatalf("expected sensitive key rejection, got %v", err)
	}
}

func TestValidateCatalogParametersSchemaRejectsModeMismatch(t *testing.T) {
	raw := `{"controls":[{"key":"duration","type":"number","modes":["video"],"min":1,"max":10}]}`
	err := validateCatalogParametersSchema(model.ModelCatalogKindImage, raw)
	if err == nil || !strings.Contains(err.Error(), "当前前台入口") {
		t.Fatalf("expected mode mismatch rejection, got %v", err)
	}
}

func TestValidateCatalogParametersSchemaRejectsSelectWithoutOptions(t *testing.T) {
	raw := `{"controls":[{"key":"quality","type":"select","options":[]}]}`
	err := validateCatalogParametersSchema(model.ModelCatalogKindImage, raw)
	if err == nil || !strings.Contains(err.Error(), "options") {
		t.Fatalf("expected missing options rejection, got %v", err)
	}
}

func TestValidateCatalogParametersSchemaRejectsUnsupportedShape(t *testing.T) {
	err := validateCatalogParametersSchema(model.ModelCatalogKindText, `{"fields":[]}`)
	if err == nil || !strings.Contains(err.Error(), "controls") {
		t.Fatalf("expected unsupported schema shape rejection, got %v", err)
	}
}

func TestDuplicateModelSourceMappingRejectsEquivalentEffectiveUpstream(t *testing.T) {
	item := &model.ModelCatalog{ModelCode: "mimo-v2.5-pro", UpstreamDefaultModel: "mimo-v2.5-pro"}
	sources := []*model.ModelSourceMapping{{
		ID:            12,
		ModelCode:     "mimo-v2.5-pro",
		SourceType:    model.ModelSourceTypeAPIChannel,
		SourceCode:    "mimo-official",
		UpstreamModel: "",
		Adapter:       model.APIChannelAdapterOpenAIChat,
		AuthType:      model.AuthTypeAPIKey,
	}}
	dup := duplicateModelSourceMapping(item, sources, 0, "mimo-v2.5-pro", model.ModelSourceTypeAPIChannel, "mimo-official", model.APIChannelAdapterOpenAIChat, model.AuthTypeAPIKey, "", "mimo-v2.5-pro")
	if dup == nil || dup.ID != 12 {
		t.Fatalf("expected duplicate source mapping, got %#v", dup)
	}
}

func TestDuplicateModelSourceMappingAllowsDifferentAuthType(t *testing.T) {
	item := &model.ModelCatalog{ModelCode: "gpt-image-2", UpstreamDefaultModel: "gpt-image-2"}
	sources := []*model.ModelSourceMapping{{
		ID:            12,
		ModelCode:     "gpt-image-2",
		SourceType:    model.ModelSourceTypeAccountPool,
		SourceCode:    model.ProviderGPT,
		UpstreamModel: "gpt-image-2",
		AuthType:      model.AuthTypeAPIKey,
		ImageAPIMode:  "openai_images",
	}}
	dup := duplicateModelSourceMapping(item, sources, 0, "gpt-image-2", model.ModelSourceTypeAccountPool, model.ProviderGPT, "", model.AuthTypeOAuth, "openai_images", "gpt-image-2")
	if dup != nil {
		t.Fatalf("expected different auth type to be allowed, got duplicate %#v", dup)
	}
}

func TestDuplicateModelSourceMappingIgnoresExcludedID(t *testing.T) {
	item := &model.ModelCatalog{ModelCode: "deepseek-chat", UpstreamDefaultModel: "deepseek-chat"}
	sources := []*model.ModelSourceMapping{{
		ID:            12,
		ModelCode:     "deepseek-chat",
		SourceType:    model.ModelSourceTypeAPIChannel,
		SourceCode:    "deepseek-official",
		UpstreamModel: "deepseek-chat",
		Adapter:       model.APIChannelAdapterOpenAIChat,
		AuthType:      model.AuthTypeAPIKey,
	}}
	dup := duplicateModelSourceMapping(item, sources, 12, "deepseek-chat", model.ModelSourceTypeAPIChannel, "deepseek-official", model.APIChannelAdapterOpenAIChat, model.AuthTypeAPIKey, "", "deepseek-chat")
	if dup != nil {
		t.Fatalf("expected current row to be excluded, got duplicate %#v", dup)
	}
}

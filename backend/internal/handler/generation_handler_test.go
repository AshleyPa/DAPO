package handler

import (
	"encoding/json"
	"testing"

	"github.com/kleinai/backend/internal/model"
)

func TestPublicModelPricingModePreservesCatalogCharMode(t *testing.T) {
	got := publicModelPricingMode(model.ModelCatalogPricingChar, model.ModelCatalogKindText, 0, 100, 300)
	if got != model.ModelCatalogPricingChar {
		t.Fatalf("pricing mode = %q, want char", got)
	}
}

func TestPublicModelPricingModeDerivesLegacyTextTokenMode(t *testing.T) {
	got := publicModelPricingMode("", model.ModelCatalogKindText, 0, 100, 300)
	if got != model.ModelCatalogPricingToken {
		t.Fatalf("pricing mode = %q, want token", got)
	}
}

func TestPublicModelRespSerializesPricingMode(t *testing.T) {
	raw, err := json.Marshal(publicModelResp{
		ModelCode:        "mimo-v2.5-pro",
		Name:             "MiMo",
		Kind:             model.ModelCatalogKindText,
		Provider:         "mimo",
		PricingMode:      model.ModelCatalogPricingChar,
		InputUnitPoints:  100,
		OutputUnitPoints: 300,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got["pricing_mode"] != model.ModelCatalogPricingChar {
		t.Fatalf("pricing_mode = %#v", got["pricing_mode"])
	}
}

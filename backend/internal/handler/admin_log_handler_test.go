package handler

import (
	"testing"
	"time"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
)

func TestModelGatewayRouteSnapshotFromParams(t *testing.T) {
	raw := `{"_model_gateway_route_snapshot":{"model_code":"mimo-v2.5-pro","selected_index":1,"skipped_count":1}}`

	got := modelGatewayRouteSnapshotFromParams(&raw)
	snapshot, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("snapshot = %#v, want object", got)
	}
	if snapshot["model_code"] != "mimo-v2.5-pro" {
		t.Fatalf("model_code = %#v", snapshot["model_code"])
	}
	if snapshot["skipped_count"] != float64(1) {
		t.Fatalf("skipped_count = %#v", snapshot["skipped_count"])
	}
}

func TestModelGatewayRouteSnapshotFromParamsIgnoresInvalidOrMissing(t *testing.T) {
	invalid := `{"_model_gateway_route_snapshot":`
	if got := modelGatewayRouteSnapshotFromParams(&invalid); got != nil {
		t.Fatalf("invalid snapshot = %#v, want nil", got)
	}
	missing := `{"model":"gpt-image-2"}`
	if got := modelGatewayRouteSnapshotFromParams(&missing); got != nil {
		t.Fatalf("missing snapshot = %#v, want nil", got)
	}
}

func TestPricingSnapshotFromParams(t *testing.T) {
	raw := `{"_model_gateway_pricing_snapshot":{"pricing_source":"model_catalog","actual_points":483,"settlement":"partial_refund"}}`

	got := pricingSnapshotFromParams(&raw)
	snapshot, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("snapshot = %#v, want object", got)
	}
	if snapshot["pricing_source"] != "model_catalog" {
		t.Fatalf("pricing_source = %#v", snapshot["pricing_source"])
	}
	if snapshot["actual_points"] != float64(483) {
		t.Fatalf("actual_points = %#v", snapshot["actual_points"])
	}
}

func TestModelGatewayAuditRespFromRowSummarizesSnapshots(t *testing.T) {
	params := `{
		"_model_gateway_route_snapshot": {
			"model_code": "mimo-v2.5-pro",
			"kind": "chat",
			"selected_index": 1,
			"candidate_count": 1,
			"candidates": [
				{"index":1,"source_type":"api_channel","source_code":"mimo-cn","source_name":"MiMo","adapter":"openai_compatible_chat","upstream_model":"mimo-v2.5-pro"}
			],
			"skipped_candidates": [
				{"index":1,"source_type":"account_pool","source_code":"gpt","skip_reason":"account_pool_mismatch"}
			]
		},
		"_model_gateway_pricing_snapshot": {
			"pricing_source": "model_catalog",
			"pricing_mode": "token",
			"settlement": "actual_usage",
			"pre_deduct_points": 500,
			"actual_points": 483,
			"refund_points": 17
		},
		"_model_gateway_output_snapshot": {
			"kind": "chat",
			"stream": false,
			"output_present": true,
			"content_chars": 42,
			"completion_tokens": 21
		},
		"_model_gateway_video_job": {
			"source_type": "api_channel",
			"source_code": "mimo-cn",
			"adapter": "openai_compatible_video",
			"upstream_model": "mimo-video",
			"remote_task_id": "remote-1",
			"phase": "terminal_success",
			"poll_attempts": 3,
			"fallback_locked": true
		}
	}`
	row := &repo.AdminGenerationLogRow{
		TaskID:     "task-1",
		CreatedAt:  time.Unix(100, 0),
		UserID:     22,
		UserLabel:  "user",
		Kind:       "chat",
		ModelCode:  "mimo-v2.5-pro",
		Status:     2,
		CostPoints: 483,
		Params:     &params,
	}
	preview := "https://static.example/result.png"
	row.PreviewURL = &preview

	got := modelGatewayAuditRespFromRow(row)
	if got.PreviewURL != "/admin/api/v1/logs/generations/task-1/preview" {
		t.Fatalf("PreviewURL = %q", got.PreviewURL)
	}
	if got.SelectedSourceCode != "mimo-cn" {
		t.Fatalf("SelectedSourceCode = %q, want mimo-cn", got.SelectedSourceCode)
	}
	if got.SelectedAdapter != "openai_compatible_chat" {
		t.Fatalf("SelectedAdapter = %q", got.SelectedAdapter)
	}
	if len(got.SkipReasons) != 1 || got.SkipReasons[0] != "account_pool_mismatch" {
		t.Fatalf("SkipReasons = %#v", got.SkipReasons)
	}
	if got.PricingSource != "model_catalog" || got.ActualPoints != 483 || got.RefundPoints != 17 {
		t.Fatalf("pricing summary = source %q actual %d refund %d", got.PricingSource, got.ActualPoints, got.RefundPoints)
	}
	output, ok := got.OutputSnapshot.(map[string]any)
	if !ok || output["output_present"] != true || output["content_chars"] != float64(42) {
		t.Fatalf("output snapshot = %#v", got.OutputSnapshot)
	}
	videoJob, ok := got.VideoJobSnapshot.(map[string]any)
	if !ok || videoJob["phase"] != "terminal_success" || videoJob["remote_task_id"] != "remote-1" || videoJob["fallback_locked"] != true {
		t.Fatalf("video job snapshot = %#v", got.VideoJobSnapshot)
	}
}

func TestBillingProofRespSummarizesWalletNetSpend(t *testing.T) {
	now := time.Unix(100, 0)
	refundReason := "chat usage refund"
	proof := &repo.TaskBillingProof{
		ConsumeRecord: &model.ConsumeRecord{
			ID:          1,
			TaskID:      "task-1",
			UserID:      22,
			Kind:        "chat",
			ModelCode:   "mimo-v2.5-pro",
			Count:       1,
			UnitPoints:  483,
			TotalPoints: 483,
			Status:      model.ConsumeStatusSettled,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		WalletLogs: []*model.WalletLog{
			{ID: 10, UserID: 22, Direction: -1, BizType: model.BizConsume, BizID: "task-1", Points: -500, PointsBefore: 1000, PointsAfter: 500, CreatedAt: now},
			{ID: 11, UserID: 22, Direction: 1, BizType: model.BizRefund, BizID: "task-1", Points: 17, PointsBefore: 500, PointsAfter: 517, Remark: &refundReason, CreatedAt: now},
		},
		RefundRecords: []*model.RefundRecord{
			{ID: 20, TaskID: "task-1", UserID: 22, Points: 17, Reason: refundReason, Operator: "system", CreatedAt: now},
		},
	}

	got := billingProofResp("task-1", proof)
	if got.Consume == nil || got.Consume.TotalPoints != 483 || got.Summary.ConsumeStatus != model.ConsumeStatusSettled {
		t.Fatalf("consume summary = %#v", got.Consume)
	}
	if got.Summary.WalletNetPoints != -483 || got.Summary.WalletSpendPoints != 483 {
		t.Fatalf("wallet net/spend = %d/%d", got.Summary.WalletNetPoints, got.Summary.WalletSpendPoints)
	}
	if got.Summary.WalletRefundPoints != 17 || got.Summary.RefundRecordCount != 1 {
		t.Fatalf("refund summary = points %d records %d", got.Summary.WalletRefundPoints, got.Summary.RefundRecordCount)
	}
}

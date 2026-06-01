package repo

import (
	"strings"
	"testing"
)

func TestAppendModelGatewayAuditTypeFilterOutput(t *testing.T) {
	where, args := appendModelGatewayAuditTypeFilter([]string{"base"}, nil, "output")
	joined := strings.Join(where, " AND ")
	if !strings.Contains(joined, "generation_result") {
		t.Fatalf("output filter should accept persisted generation results: %s", joined)
	}
	if !strings.Contains(joined, "t.params LIKE ?") {
		t.Fatalf("output filter should stay scoped to audited tasks: %s", joined)
	}
	if len(args) != 6 || args[4] != modelGatewayOutputPresentLike || args[5] != modelGatewayOutputPresentSpaced {
		t.Fatalf("output args = %#v", args)
	}
}

func TestAppendModelGatewayAuditTypeFilterOutputMissing(t *testing.T) {
	where, args := appendModelGatewayAuditTypeFilter([]string{"base"}, nil, "output_missing")
	joined := strings.Join(where, " AND ")
	if !strings.Contains(joined, "t.params LIKE ?") || !strings.Contains(joined, "NOT EXISTS") {
		t.Fatalf("output_missing filter should stay scoped to audited tasks without results: %s", joined)
	}
	if !strings.Contains(joined, "t.params NOT LIKE ?") {
		t.Fatalf("output_missing filter should reject positive output snapshots: %s", joined)
	}
	if len(args) != 6 {
		t.Fatalf("output_missing args len = %d, want 6: %#v", len(args), args)
	}
	if args[4] != modelGatewayOutputPresentLike || args[5] != modelGatewayOutputPresentSpaced {
		t.Fatalf("output_missing output-present args = %#v", args[4:])
	}
}

func TestAppendModelGatewayAuditTypeFilterVideoMissing(t *testing.T) {
	where, args := appendModelGatewayAuditTypeFilter([]string{"base"}, nil, "video_missing")
	joined := strings.Join(where, " AND ")
	if !strings.Contains(joined, "t.kind = 'video'") || !strings.Contains(joined, "t.params NOT LIKE ?") {
		t.Fatalf("video_missing filter = %s", joined)
	}
	if len(args) != 5 || args[4] != modelGatewayVideoJobLike {
		t.Fatalf("video_missing args = %#v", args)
	}
}

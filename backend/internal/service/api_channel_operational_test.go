package service

import (
	"context"
	"testing"
	"time"

	"github.com/kleinai/backend/internal/model"
)

func TestAPIChannelOperationalAcceptsHealthyChannelWithUsableKey(t *testing.T) {
	repo := fakeReadinessAPIChannelRepo{
		channels: map[string]*model.APIChannel{
			"mimo-official": healthyReadinessAPIChannel(1, "mimo-official"),
		},
		keys: map[uint64][]*model.APIChannelKey{
			1: {{ID: 11, ChannelID: 1, Status: model.APIChannelKeyStatusEnabled, CredentialEnc: []byte("enc")}},
		},
	}

	state := inspectAPIChannelOperational(context.Background(), repo, "mimo-official")

	if reason := apiChannelOperationalSkipReason(state); reason != "" {
		t.Fatalf("skip reason = %q, want empty", reason)
	}
	if !state.Exists || !state.Enabled || !state.HealthOK || state.KeyTotal != 1 || state.UsableKeys != 1 {
		t.Fatalf("operational state = %#v", state)
	}
	if state.CredentialTotal() != 1 || state.UsableCredentials() != 1 {
		t.Fatalf("credential totals = %d/%d, want 1/1", state.CredentialTotal(), state.UsableCredentials())
	}
}

func TestAPIChannelOperationalAcceptsHealthyLegacyCredential(t *testing.T) {
	repo := fakeReadinessAPIChannelRepo{
		channels: map[string]*model.APIChannel{
			"mimo-official": healthyReadinessAPIChannel(1, "mimo-official"),
		},
	}
	repo.channels["mimo-official"].CredentialEnc = []byte("legacy")

	state := inspectAPIChannelOperational(context.Background(), repo, "mimo-official")

	if reason := apiChannelOperationalSkipReason(state); reason != "" {
		t.Fatalf("skip reason = %q, want empty", reason)
	}
	if !state.LegacyKey || state.KeyTotal != 0 || state.UsableKeys != 0 {
		t.Fatalf("operational state = %#v", state)
	}
	if state.CredentialTotal() != 1 || state.UsableCredentials() != 1 {
		t.Fatalf("credential totals = %d/%d, want 1/1", state.CredentialTotal(), state.UsableCredentials())
	}
}

func TestAPIChannelOperationalRequiresRecentHealthyTest(t *testing.T) {
	failedAt := time.Now()
	repo := fakeReadinessAPIChannelRepo{
		channels: map[string]*model.APIChannel{
			"mimo-official": {
				ID:             1,
				Code:           "mimo-official",
				Status:         model.APIChannelStatusEnabled,
				LastTestAt:     &failedAt,
				LastTestStatus: 2,
			},
		},
		keys: map[uint64][]*model.APIChannelKey{
			1: {{ID: 11, ChannelID: 1, Status: model.APIChannelKeyStatusEnabled, CredentialEnc: []byte("enc")}},
		},
	}

	state := inspectAPIChannelOperational(context.Background(), repo, "mimo-official")

	if reason := apiChannelOperationalSkipReason(state); reason != "API 渠道尚未通过最近健康检测" {
		t.Fatalf("skip reason = %q", reason)
	}
}

func TestAPIChannelOperationalRequiresUsableCredential(t *testing.T) {
	repo := fakeReadinessAPIChannelRepo{
		channels: map[string]*model.APIChannel{
			"mimo-official": healthyReadinessAPIChannel(1, "mimo-official"),
		},
		keys: map[uint64][]*model.APIChannelKey{
			1: {
				{ID: 11, ChannelID: 1, Status: model.APIChannelKeyStatusDisabled, CredentialEnc: []byte("enc")},
				{ID: 12, ChannelID: 1, Status: model.APIChannelKeyStatusEnabled},
			},
		},
	}

	state := inspectAPIChannelOperational(context.Background(), repo, "mimo-official")

	if state.KeyTotal != 2 || state.UsableKeys != 0 {
		t.Fatalf("key totals = %d/%d, want 2/0", state.KeyTotal, state.UsableKeys)
	}
	if reason := apiChannelOperationalSkipReason(state); reason != "API 渠道没有可用 API 凭证" {
		t.Fatalf("skip reason = %q", reason)
	}
}

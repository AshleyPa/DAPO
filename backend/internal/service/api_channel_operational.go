package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
)

type apiChannelOperationalRepo interface {
	GetByCode(ctx context.Context, code string) (*model.APIChannel, error)
	ListKeys(ctx context.Context, f repo.APIChannelKeyListFilter) ([]*model.APIChannelKey, int64, error)
}

type apiChannelOperationalStatus struct {
	Code        string
	Channel     *model.APIChannel
	Exists      bool
	Enabled     bool
	HealthOK    bool
	LegacyKey   bool
	KeyTotal    int
	UsableKeys  int
	QueryErr    error
	KeyQueryErr error
}

func inspectAPIChannelOperational(ctx context.Context, r apiChannelOperationalRepo, code string) apiChannelOperationalStatus {
	state := apiChannelOperationalStatus{Code: strings.TrimSpace(code)}
	if r == nil {
		state.QueryErr = fmt.Errorf("api channel repo unavailable")
		return state
	}
	if state.Code == "" {
		return state
	}
	ch, err := r.GetByCode(ctx, state.Code)
	if err != nil {
		if !errors.Is(err, repo.ErrNotFound) {
			state.QueryErr = err
		}
		return state
	}
	if ch == nil {
		return state
	}
	state.Channel = ch
	state.Exists = true
	state.Enabled = ch.Status == model.APIChannelStatusEnabled
	state.HealthOK = apiChannelHealthOK(ch)
	state.LegacyKey = len(ch.CredentialEnc) > 0
	keyTotal, usableKeys, err := countAPIChannelUsableKeys(ctx, r, ch.ID)
	state.KeyTotal = keyTotal
	state.UsableKeys = usableKeys
	state.KeyQueryErr = err
	return state
}

func apiChannelHealthOK(ch *model.APIChannel) bool {
	return ch != nil && ch.LastTestAt != nil && ch.LastTestStatus == 1
}

func (state apiChannelOperationalStatus) CredentialTotal() int {
	total := state.KeyTotal
	if state.LegacyKey {
		total++
	}
	return total
}

func (state apiChannelOperationalStatus) UsableCredentials() int {
	usable := state.UsableKeys
	if state.LegacyKey {
		usable++
	}
	return usable
}

func countAPIChannelUsableKeys(ctx context.Context, r apiChannelOperationalRepo, channelID uint64) (int, int, error) {
	if r == nil || channelID == 0 {
		return 0, 0, nil
	}
	page := 1
	total := 0
	usable := 0
	for {
		items, gotTotal, err := r.ListKeys(ctx, repo.APIChannelKeyListFilter{
			ChannelID: channelID,
			Page:      page,
			PageSize:  500,
		})
		if err != nil {
			return total, usable, err
		}
		for _, key := range items {
			if key == nil {
				continue
			}
			total++
			if key.Status == model.APIChannelKeyStatusEnabled && len(key.CredentialEnc) > 0 {
				usable++
			}
		}
		if len(items) == 0 || gotTotal <= int64(total) {
			return total, usable, nil
		}
		page++
	}
}

func apiChannelOperationalSkipReason(state apiChannelOperationalStatus) string {
	switch {
	case state.QueryErr != nil:
		return "API 渠道读取失败"
	case !state.Exists:
		return "API 渠道不存在或已删除"
	case !state.Enabled:
		return "API 渠道已停用"
	case !state.HealthOK:
		return "API 渠道尚未通过最近健康检测"
	case state.KeyQueryErr != nil && !state.LegacyKey:
		return "API 渠道凭证读取失败"
	case state.UsableCredentials() <= 0:
		return "API 渠道没有可用 API 凭证"
	default:
		return ""
	}
}

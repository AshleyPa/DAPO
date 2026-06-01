package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/crypto"
	"github.com/kleinai/backend/pkg/ratelimit"
)

type APIChannelCredentialRef struct {
	Token     string
	Source    string
	KeyID     uint64
	KeyName   string
	ChannelID uint64
}

type apiChannelCredentialUsageRecorder interface {
	UpdateKey(ctx context.Context, id uint64, fields map[string]any) error
}

type apiChannelCredentialKeyLister interface {
	ListKeys(ctx context.Context, f repo.APIChannelKeyListFilter) ([]*model.APIChannelKey, int64, error)
}

type apiChannelDistributedLimiter interface {
	allowN(ctx context.Context, key string, ratePerMin, n int) (bool, error)
}

type redisAPIChannelLimiter struct {
	limiter *ratelimit.Limiter
}

const (
	apiChannelCredentialSourceKeyPool = "key_pool"
	apiChannelCredentialSourceLegacy  = "channel_legacy"
)

type apiChannelLimiter struct {
	mu      sync.Mutex
	buckets map[string]apiChannelUsageBucket
}

type apiChannelUsageBucket struct {
	Minute   int64
	Requests int
	Tokens   int
}

var defaultAPIChannelLimiter = newAPIChannelLimiter()
var defaultAPIChannelKeyPicker = newAPIChannelKeyPicker()

func newAPIChannelDistributedLimiter(limiter *ratelimit.Limiter) apiChannelDistributedLimiter {
	if limiter == nil {
		return nil
	}
	return &redisAPIChannelLimiter{limiter: limiter}
}

func optionalAPIChannelDistributedLimiter(limiters []*ratelimit.Limiter) apiChannelDistributedLimiter {
	if len(limiters) == 0 {
		return nil
	}
	return newAPIChannelDistributedLimiter(limiters[0])
}

func (l *redisAPIChannelLimiter) allowN(ctx context.Context, key string, ratePerMin, n int) (bool, error) {
	if l == nil || l.limiter == nil {
		return true, nil
	}
	res, err := l.limiter.AllowN(ctx, key, ratePerMin, n)
	if err != nil {
		return false, err
	}
	return res != nil && res.Allowed > 0, nil
}

func newAPIChannelLimiter() *apiChannelLimiter {
	return &apiChannelLimiter{buckets: map[string]apiChannelUsageBucket{}}
}

func (l *apiChannelLimiter) allow(key string, rpmLimit, tpmLimit, tokens int) bool {
	if l == nil {
		return true
	}
	if rpmLimit <= 0 && tpmLimit <= 0 {
		return true
	}
	if tokens < 0 {
		tokens = 0
	}
	minute := time.Now().Unix() / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b.Minute != minute {
		b = apiChannelUsageBucket{Minute: minute}
	}
	if rpmLimit > 0 && b.Requests+1 > rpmLimit {
		l.buckets[key] = b
		return false
	}
	if tpmLimit > 0 && tokens > 0 && b.Tokens+tokens > tpmLimit {
		l.buckets[key] = b
		return false
	}
	b.Requests++
	b.Tokens += tokens
	l.buckets[key] = b
	return true
}

type apiChannelKeyPicker struct {
	mu      sync.Mutex
	cursors map[string]int
}

func newAPIChannelKeyPicker() *apiChannelKeyPicker {
	return &apiChannelKeyPicker{cursors: map[string]int{}}
}

func (p *apiChannelKeyPicker) order(channelID uint64, priority int, keys []*model.APIChannelKey) []*model.APIChannelKey {
	seq := weightedAPIChannelKeySequence(keys)
	if len(seq) == 0 {
		return nil
	}
	cursorKey := fmt.Sprintf("%d:%d", channelID, priority)
	p.mu.Lock()
	start := p.cursors[cursorKey] % len(seq)
	p.cursors[cursorKey] = (start + 1) % len(seq)
	p.mu.Unlock()

	out := make([]*model.APIChannelKey, 0, len(keys))
	seen := map[uint64]bool{}
	for i := 0; i < len(seq); i++ {
		key := seq[(start+i)%len(seq)]
		if key == nil || key.ID == 0 || seen[key.ID] {
			continue
		}
		seen[key.ID] = true
		out = append(out, key)
	}
	return out
}

func weightedAPIChannelKeySequence(keys []*model.APIChannelKey) []*model.APIChannelKey {
	items := make([]*model.APIChannelKey, 0, len(keys))
	weights := make([]int, 0, len(keys))
	for _, key := range keys {
		if key == nil {
			continue
		}
		weight := key.Weight
		if weight <= 0 {
			weight = 1
		}
		items = append(items, key)
		weights = append(weights, weight)
	}
	if len(items) == 0 {
		return nil
	}
	divisor := weights[0]
	for _, weight := range weights[1:] {
		divisor = gcdInt(divisor, weight)
	}
	maxWeight := 0
	for i, weight := range weights {
		weight = weight / divisor
		weights[i] = weight
		if weight > maxWeight {
			maxWeight = weight
		}
	}
	if maxWeight > 100 {
		for i, weight := range weights {
			scaled := weight * 100 / maxWeight
			if scaled <= 0 {
				scaled = 1
			}
			weights[i] = scaled
		}
		maxWeight = 100
	}
	out := make([]*model.APIChannelKey, 0)
	for round := 0; round < maxWeight; round++ {
		for i, key := range items {
			if weights[i] > round {
				out = append(out, key)
			}
		}
	}
	return out
}

func gcdInt(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	if a == 0 {
		if b == 0 {
			return 1
		}
		return b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func apiChannelKeyPriority(key *model.APIChannelKey) int {
	if key == nil {
		return 100
	}
	if key.Priority == 0 {
		return 100
	}
	return key.Priority
}

func selectAPIChannelCredential(ctx context.Context, r apiChannelCredentialKeyLister, aes *crypto.AESGCM, ch *model.APIChannel, estimatedTokens int) (*APIChannelCredentialRef, error) {
	return selectAPIChannelCredentialWithOptions(ctx, r, aes, ch, estimatedTokens, true, nil)
}

func selectAPIChannelCredentialWithLimiter(ctx context.Context, r apiChannelCredentialKeyLister, aes *crypto.AESGCM, ch *model.APIChannel, estimatedTokens int, limiter apiChannelDistributedLimiter) (*APIChannelCredentialRef, error) {
	return selectAPIChannelCredentialWithOptions(ctx, r, aes, ch, estimatedTokens, true, limiter)
}

func firstAPIChannelCredential(ctx context.Context, r apiChannelCredentialKeyLister, aes *crypto.AESGCM, ch *model.APIChannel) (*APIChannelCredentialRef, error) {
	return selectAPIChannelCredentialWithOptions(ctx, r, aes, ch, 0, false, nil)
}

func selectAPIChannelCredentialWithOptions(ctx context.Context, r apiChannelCredentialKeyLister, aes *crypto.AESGCM, ch *model.APIChannel, estimatedTokens int, enforceLimit bool, limiter apiChannelDistributedLimiter) (*APIChannelCredentialRef, error) {
	if ch == nil {
		return nil, fmt.Errorf("api channel is nil")
	}
	if aes == nil {
		return nil, fmt.Errorf("api channel missing credential crypto")
	}
	if r != nil && ch.ID > 0 {
		status := int8(model.APIChannelKeyStatusEnabled)
		keys, _, err := r.ListKeys(ctx, repo.APIChannelKeyListFilter{ChannelID: ch.ID, Status: &status, Page: 1, PageSize: 500})
		if err == nil && len(keys) > 0 {
			sort.SliceStable(keys, func(i, j int) bool {
				if keys[i] == nil {
					return false
				}
				if keys[j] == nil {
					return true
				}
				pi, pj := apiChannelKeyPriority(keys[i]), apiChannelKeyPriority(keys[j])
				if pi != pj {
					return pi < pj
				}
				return keys[i].ID < keys[j].ID
			})
			totalEligible, totalRateLimited := 0, 0
			for i := 0; i < len(keys); {
				priority := apiChannelKeyPriority(keys[i])
				j := i + 1
				for j < len(keys) && apiChannelKeyPriority(keys[j]) == priority {
					j++
				}
				for _, key := range defaultAPIChannelKeyPicker.order(ch.ID, priority, keys[i:j]) {
					if key == nil || len(key.CredentialEnc) == 0 {
						continue
					}
					totalEligible++
					rpmLimit := key.RPMLimit
					if rpmLimit <= 0 {
						rpmLimit = ch.RPMLimit
					}
					tpmLimit := key.TPMLimit
					if tpmLimit <= 0 {
						tpmLimit = ch.TPMLimit
					}
					limitKey := fmt.Sprintf("api_channel_key:%d", key.ID)
					if enforceLimit && !allowAPIChannelCredentialLimit(ctx, defaultAPIChannelLimiter, limiter, limitKey, rpmLimit, tpmLimit, estimatedTokens) {
						totalRateLimited++
						continue
					}
					token, err := decryptAPIChannelCredential(aes, key.CredentialEnc)
					if err != nil {
						continue
					}
					return &APIChannelCredentialRef{
						Token:     token,
						Source:    apiChannelCredentialSourceKeyPool,
						KeyID:     key.ID,
						KeyName:   key.Name,
						ChannelID: ch.ID,
					}, nil
				}
				i = j
			}
			if totalEligible > 0 && totalRateLimited == totalEligible {
				return nil, fmt.Errorf("api channel key pool rate limited")
			}
		}
	}
	if enforceLimit {
		limitKey := fmt.Sprintf("api_channel_legacy:%d", ch.ID)
		if !allowAPIChannelCredentialLimit(ctx, defaultAPIChannelLimiter, limiter, limitKey, ch.RPMLimit, ch.TPMLimit, estimatedTokens) {
			return nil, fmt.Errorf("api channel rate limited")
		}
	}
	token, err := decryptAPIChannelCredential(aes, ch.CredentialEnc)
	if err != nil {
		return nil, err
	}
	return &APIChannelCredentialRef{
		Token:     token,
		Source:    apiChannelCredentialSourceLegacy,
		ChannelID: ch.ID,
	}, nil
}

func allowAPIChannelCredentialLimit(ctx context.Context, local *apiChannelLimiter, distributed apiChannelDistributedLimiter, key string, rpmLimit, tpmLimit, tokens int) bool {
	if rpmLimit <= 0 && tpmLimit <= 0 {
		return true
	}
	if tokens < 0 {
		tokens = 0
	}
	if distributed != nil {
		allowed, err := allowAPIChannelDistributedLimit(ctx, distributed, key, rpmLimit, tpmLimit, tokens)
		if err == nil {
			return allowed
		}
	}
	if local == nil {
		return true
	}
	return local.allow(key, rpmLimit, tpmLimit, tokens)
}

func allowAPIChannelDistributedLimit(ctx context.Context, limiter apiChannelDistributedLimiter, key string, rpmLimit, tpmLimit, tokens int) (bool, error) {
	if limiter == nil {
		return true, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if tpmLimit > 0 && tokens > 0 {
		allowed, err := limiter.allowN(ctx, apiChannelDistributedLimitKey(key, "tpm"), tpmLimit, tokens)
		if err != nil || !allowed {
			return allowed, err
		}
	}
	if rpmLimit > 0 {
		allowed, err := limiter.allowN(ctx, apiChannelDistributedLimitKey(key, "rpm"), rpmLimit, 1)
		if err != nil || !allowed {
			return allowed, err
		}
	}
	return true, nil
}

func apiChannelDistributedLimitKey(key, bucket string) string {
	return fmt.Sprintf("ratelimit:model_gateway:%s:%s", key, bucket)
}

func decryptAPIChannelCredential(aes *crypto.AESGCM, encrypted []byte) (string, error) {
	if aes == nil || len(encrypted) == 0 {
		return "", fmt.Errorf("api channel missing credential")
	}
	plain, err := aes.Decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt api channel credential failed")
	}
	token := strings.TrimSpace(string(plain))
	if token == "" {
		return "", fmt.Errorf("api channel missing credential")
	}
	return token, nil
}

func addAPIChannelCredentialMeta(meta map[string]any, ref *APIChannelCredentialRef) {
	if meta == nil || ref == nil {
		return
	}
	meta["api_channel_credential_source"] = ref.Source
	if ref.KeyID > 0 {
		meta["api_channel_key_id"] = ref.KeyID
	}
	if strings.TrimSpace(ref.KeyName) != "" {
		meta["api_channel_key_name"] = ref.KeyName
	}
}

func recordAPIChannelCredentialSuccess(ctx context.Context, r apiChannelCredentialUsageRecorder, ref *APIChannelCredentialRef) {
	recordAPIChannelCredentialUsage(ctx, r, ref, "")
}

func recordAPIChannelCredentialError(ctx context.Context, r apiChannelCredentialUsageRecorder, ref *APIChannelCredentialRef, err error) {
	if err == nil {
		return
	}
	recordAPIChannelCredentialUsage(ctx, r, ref, err.Error())
}

func recordAPIChannelCredentialHTTPFailure(ctx context.Context, r apiChannelCredentialUsageRecorder, ref *APIChannelCredentialRef, status int, body []byte) {
	msg := fmt.Sprintf("HTTP %d", status)
	if text := strings.TrimSpace(snippet(body, 240)); text != "" {
		msg += ": " + text
	}
	recordAPIChannelCredentialUsage(ctx, r, ref, msg)
}

func recordAPIChannelCredentialUsage(ctx context.Context, r apiChannelCredentialUsageRecorder, ref *APIChannelCredentialRef, errorText string) {
	if r == nil || ref == nil || ref.KeyID == 0 {
		return
	}
	updateCtx := context.Background()
	if ctx != nil && ctx.Err() == nil {
		updateCtx = ctx
	}
	updateCtx, cancel := context.WithTimeout(updateCtx, 2*time.Second)
	defer cancel()
	_ = r.UpdateKey(updateCtx, ref.KeyID, apiChannelCredentialUsageFields(time.Now().UTC(), errorText))
}

func apiChannelCredentialUsageFields(now time.Time, errorText string) map[string]any {
	fields := map[string]any{
		"last_used_at": now,
		"last_error":   nil,
	}
	errorText = strings.TrimSpace(errorText)
	if errorText != "" {
		fields["last_error"] = truncate(errorText, 512)
	}
	return fields
}

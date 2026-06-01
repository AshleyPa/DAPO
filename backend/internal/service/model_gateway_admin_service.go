package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/errcode"
)

type ModelGatewayAdminService struct {
	modelRepo      *repo.ModelCatalogRepo
	sourceRepo     *repo.ModelSourceRepo
	apiChannelRepo *repo.APIChannelRepo
	accountRepo    *repo.AccountRepo
}

func NewModelGatewayAdminService(modelRepo *repo.ModelCatalogRepo, sourceRepo *repo.ModelSourceRepo, apiChannelRepo *repo.APIChannelRepo, accountRepo *repo.AccountRepo) *ModelGatewayAdminService {
	return &ModelGatewayAdminService{
		modelRepo:      modelRepo,
		sourceRepo:     sourceRepo,
		apiChannelRepo: apiChannelRepo,
		accountRepo:    accountRepo,
	}
}

func (s *ModelGatewayAdminService) ListModels(ctx context.Context, req *dto.ModelCatalogListReq) ([]*dto.ModelCatalogResp, int64, error) {
	items, total, err := s.modelRepo.List(ctx, repo.ModelCatalogListFilter{
		EntryKind: normalizeModelGatewayKindLoose(req.EntryKind),
		Status:    req.Status,
		Visible:   req.Visible,
		Keyword:   strings.TrimSpace(req.Keyword),
		Page:      req.Page,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, 0, errcode.DBError.Wrap(err)
	}
	out := make([]*dto.ModelCatalogResp, 0, len(items))
	for _, item := range items {
		out = append(out, modelCatalogResp(item))
	}
	return out, total, nil
}

func (s *ModelGatewayAdminService) CreateModel(ctx context.Context, adminID uint64, req *dto.ModelCatalogCreateReq) (*model.ModelCatalog, error) {
	code, err := normalizeModelGatewayCode(req.ModelCode, "模型编码")
	if err != nil {
		return nil, err
	}
	kind, err := normalizeModelGatewayKind(req.EntryKind)
	if err != nil {
		return nil, err
	}
	pricingMode, err := normalizeModelPricingMode(req.PricingMode)
	if err != nil {
		return nil, err
	}
	caps, err := marshalStringList(normalizeModelGatewayStringList(req.Capabilities))
	if err != nil {
		return nil, errcode.InvalidParam.Wrap(err)
	}
	tags, err := marshalStringList(normalizeModelGatewayStringList(req.Tags))
	if err != nil {
		return nil, errcode.InvalidParam.Wrap(err)
	}
	visible := int8(1)
	if req.Visible != nil {
		visible = *req.Visible
	}
	status := int8(model.ModelCatalogStatusEnabled)
	if req.Status != nil {
		status = *req.Status
	}
	priceRules, err := marshalCatalogPriceRules(kind, code, pricingMode, status, req.PriceRules)
	if err != nil {
		return nil, err
	}
	parametersSchema, err := marshalCatalogParametersSchema(kind, req.ParametersSchema)
	if err != nil {
		return nil, err
	}
	minPlan := strings.TrimSpace(req.MinPlan)
	if minPlan == "" {
		minPlan = "free"
	}
	item := &model.ModelCatalog{
		ModelCode:            code,
		DisplayName:          strings.TrimSpace(req.DisplayName),
		EntryKind:            kind,
		ProviderHint:         strings.ToLower(strings.TrimSpace(req.ProviderHint)),
		UpstreamDefaultModel: strings.TrimSpace(req.UpstreamDefaultModel),
		Capabilities:         stringPtrOrNil(caps),
		ParametersSchema:     stringPtrOrNil(parametersSchema),
		PricingMode:          pricingMode,
		UnitPoints:           req.UnitPoints,
		InputUnitPoints:      req.InputUnitPoints,
		OutputUnitPoints:     req.OutputUnitPoints,
		PriceRules:           stringPtrOrNil(priceRules),
		MinPlan:              minPlan,
		Tags:                 stringPtrOrNil(tags),
		SortOrder:            req.SortOrder,
		Visible:              visible,
		Status:               status,
		CreatedBy:            &adminID,
	}
	if desc := strings.TrimSpace(req.Description); desc != "" {
		item.Description = &desc
	}
	if err := s.modelRepo.Create(ctx, item); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	return item, nil
}

func (s *ModelGatewayAdminService) UpdateModel(ctx context.Context, id uint64, req *dto.ModelCatalogUpdateReq) error {
	current, err := s.modelRepo.GetByID(ctx, id)
	if err != nil {
		return errcode.ResourceMissing
	}
	fields := map[string]any{}
	nextModelCode := current.ModelCode
	nextKind := current.EntryKind
	nextPricingMode := current.PricingMode
	nextStatus := current.Status
	nextPriceRules := ""
	nextParametersSchema := ""
	if current.PriceRules != nil {
		nextPriceRules = *current.PriceRules
	}
	if current.ParametersSchema != nil {
		nextParametersSchema = *current.ParametersSchema
	}
	if req.ModelCode != nil {
		code, err := normalizeModelGatewayCode(*req.ModelCode, "模型编码")
		if err != nil {
			return err
		}
		if !strings.EqualFold(code, current.ModelCode) {
			count, err := s.sourceRepo.CountByModelCode(ctx, current.ModelCode)
			if err != nil {
				return errcode.DBError.Wrap(err)
			}
			if count > 0 {
				return errcode.InvalidParam.WithMsg("模型已有来源映射，需先删除或迁移映射后再修改模型编码")
			}
		}
		fields["model_code"] = code
		nextModelCode = code
	}
	if req.DisplayName != nil {
		fields["display_name"] = strings.TrimSpace(*req.DisplayName)
	}
	if req.EntryKind != nil {
		kind, err := normalizeModelGatewayKind(*req.EntryKind)
		if err != nil {
			return err
		}
		fields["entry_kind"] = kind
		nextKind = kind
	}
	if req.ProviderHint != nil {
		fields["provider_hint"] = strings.ToLower(strings.TrimSpace(*req.ProviderHint))
	}
	if req.UpstreamDefaultModel != nil {
		fields["upstream_default_model"] = strings.TrimSpace(*req.UpstreamDefaultModel)
	}
	if req.Capabilities != nil {
		caps, err := marshalStringList(normalizeModelGatewayStringList(req.Capabilities))
		if err != nil {
			return errcode.InvalidParam.Wrap(err)
		}
		fields["capabilities"] = stringPtrOrNil(caps)
	}
	if req.ClearParametersSchema != nil && *req.ClearParametersSchema {
		fields["parameters_schema"] = nil
		nextParametersSchema = ""
	} else if req.ParametersSchema != nil {
		parametersSchema, err := marshalCatalogParametersSchema(nextKind, req.ParametersSchema)
		if err != nil {
			return err
		}
		fields["parameters_schema"] = stringPtrOrNil(parametersSchema)
		nextParametersSchema = parametersSchema
	}
	if req.PricingMode != nil {
		pricingMode, err := normalizeModelPricingMode(*req.PricingMode)
		if err != nil {
			return err
		}
		fields["pricing_mode"] = pricingMode
		nextPricingMode = pricingMode
	}
	if req.UnitPoints != nil {
		fields["unit_points"] = *req.UnitPoints
	}
	if req.InputUnitPoints != nil {
		fields["input_unit_points"] = *req.InputUnitPoints
	}
	if req.OutputUnitPoints != nil {
		fields["output_unit_points"] = *req.OutputUnitPoints
	}
	if req.ClearPriceRules != nil && *req.ClearPriceRules {
		fields["price_rules"] = nil
		nextPriceRules = ""
	} else if req.PriceRules != nil {
		priceRules, err := marshalCatalogPriceRules(nextKind, nextModelCode, nextPricingMode, nextStatus, req.PriceRules)
		if err != nil {
			return err
		}
		fields["price_rules"] = stringPtrOrNil(priceRules)
		nextPriceRules = priceRules
	}
	if req.MinPlan != nil {
		minPlan := strings.TrimSpace(*req.MinPlan)
		if minPlan == "" {
			minPlan = "free"
		}
		fields["min_plan"] = minPlan
	}
	if req.Tags != nil {
		tags, err := marshalStringList(normalizeModelGatewayStringList(req.Tags))
		if err != nil {
			return errcode.InvalidParam.Wrap(err)
		}
		fields["tags"] = stringPtrOrNil(tags)
	}
	if req.Description != nil {
		if desc := strings.TrimSpace(*req.Description); desc == "" {
			fields["description"] = nil
		} else {
			fields["description"] = desc
		}
	}
	if req.SortOrder != nil {
		fields["sort_order"] = *req.SortOrder
	}
	if req.Visible != nil {
		fields["visible"] = *req.Visible
	}
	if req.Status != nil {
		fields["status"] = *req.Status
		nextStatus = *req.Status
	}
	if err := validateCatalogParametersSchema(nextKind, nextParametersSchema); err != nil {
		return err
	}
	if err := validateCatalogPricingConfigurationForStatus(nextKind, nextModelCode, nextPricingMode, nextStatus, nextPriceRules); err != nil {
		return err
	}
	if err := s.modelRepo.Update(ctx, id, fields); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func (s *ModelGatewayAdminService) DeleteModel(ctx context.Context, id uint64) error {
	item, err := s.modelRepo.GetByID(ctx, id)
	if err != nil {
		return errcode.ResourceMissing
	}
	count, err := s.sourceRepo.CountByModelCode(ctx, item.ModelCode)
	if err != nil {
		return errcode.DBError.Wrap(err)
	}
	if count > 0 {
		return errcode.InvalidParam.WithMsg("模型仍有关联来源映射，需先删除来源映射")
	}
	if err := s.modelRepo.SoftDelete(ctx, id); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func (s *ModelGatewayAdminService) ListSources(ctx context.Context, req *dto.ModelSourceListReq) ([]*dto.ModelSourceResp, int64, error) {
	items, total, err := s.sourceRepo.List(ctx, repo.ModelSourceListFilter{
		ModelCode:  strings.TrimSpace(req.ModelCode),
		SourceType: normalizeModelSourceTypeLoose(req.SourceType),
		Status:     req.Status,
		Page:       req.Page,
		PageSize:   req.PageSize,
	})
	if err != nil {
		return nil, 0, errcode.DBError.Wrap(err)
	}
	out := make([]*dto.ModelSourceResp, 0, len(items))
	for _, item := range items {
		out = append(out, modelSourceResp(item))
	}
	return out, total, nil
}

func (s *ModelGatewayAdminService) ListSourceConflicts(ctx context.Context) ([]*dto.ModelSourceConflictResp, error) {
	out := []*dto.ModelSourceConflictResp{}
	for page := 1; ; page++ {
		items, total, err := s.sourceRepo.List(ctx, repo.ModelSourceListFilter{
			SourceType: model.ModelSourceTypeAccountPool,
			Page:       page,
			PageSize:   200,
		})
		if err != nil {
			return nil, errcode.DBError.Wrap(err)
		}
		for _, source := range items {
			if source == nil {
				continue
			}
			item, err := s.modelRepo.GetByCode(ctx, source.ModelCode)
			if err != nil {
				if errors.Is(err, repo.ErrNotFound) {
					out = append(out, &dto.ModelSourceConflictResp{
						ID:            source.ID,
						ModelCode:     source.ModelCode,
						SourceType:    source.SourceType,
						SourceCode:    source.SourceCode,
						UpstreamModel: source.UpstreamModel,
						Status:        source.Status,
						Reason:        "模型库中不存在该模型编码",
					})
					continue
				}
				return nil, errcode.DBError.Wrap(err)
			}
			upstreamModel := strings.TrimSpace(source.UpstreamModel)
			if upstreamModel == "" {
				upstreamModel = strings.TrimSpace(item.UpstreamDefaultModel)
			}
			if upstreamModel == "" {
				upstreamModel = item.ModelCode
			}
			if reason := accountPoolSourceMismatchReason(item, source.SourceCode, upstreamModel); reason != "" {
				out = append(out, &dto.ModelSourceConflictResp{
					ID:            source.ID,
					ModelCode:     source.ModelCode,
					SourceType:    source.SourceType,
					SourceCode:    source.SourceCode,
					UpstreamModel: upstreamModel,
					Status:        source.Status,
					Reason:        reason,
				})
			}
		}
		if int64(page*200) >= total || len(items) == 0 {
			break
		}
	}
	return out, nil
}

func (s *ModelGatewayAdminService) DryRun(ctx context.Context, req *dto.ModelGatewayDryRunReq) (*dto.ModelGatewayDryRunResp, error) {
	modelCode, err := normalizeModelGatewayCode(req.ModelCode, "模型编码")
	if err != nil {
		return nil, err
	}
	item, err := s.modelRepo.GetByCode(ctx, modelCode)
	if err != nil {
		return nil, errcode.InvalidParam.WithMsg("模型库中不存在该模型编码")
	}
	entryKind := item.EntryKind
	if strings.TrimSpace(req.EntryKind) != "" {
		normalized, err := normalizeModelGatewayKind(req.EntryKind)
		if err != nil {
			return nil, err
		}
		entryKind = normalized
	}
	sources, _, err := s.sourceRepo.List(ctx, repo.ModelSourceListFilter{
		ModelCode: modelCode,
		Page:      1,
		PageSize:  500,
	})
	if err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	resp := &dto.ModelGatewayDryRunResp{
		ModelCode:      item.ModelCode,
		DisplayName:    item.DisplayName,
		EntryKind:      entryKind,
		MatchedModel:   true,
		SelectedIndex:  0,
		Candidates:     []dto.ModelGatewayDryRunCandidate{},
		CandidateCount: len(sources),
	}
	for i, source := range sources {
		candidate := s.dryRunCandidate(ctx, i+1, item, entryKind, source)
		if candidate.Available {
			resp.AvailableCount++
			if resp.SelectedIndex == 0 {
				resp.SelectedIndex = candidate.Index
			}
		}
		resp.Candidates = append(resp.Candidates, candidate)
	}
	if len(sources) == 0 {
		resp.Warning = "模型没有配置任何来源映射"
	} else if resp.AvailableCount == 0 {
		resp.Warning = "模型已配置来源，但当前没有可用候选"
	}
	return resp, nil
}

func (s *ModelGatewayAdminService) CreateSource(ctx context.Context, req *dto.ModelSourceCreateReq) (*model.ModelSourceMapping, error) {
	modelCode, err := normalizeModelGatewayCode(req.ModelCode, "模型编码")
	if err != nil {
		return nil, err
	}
	modelItem, err := s.modelRepo.GetByCode(ctx, modelCode)
	if err != nil {
		return nil, errcode.InvalidParam.WithMsg("模型库中不存在该模型编码")
	}
	sourceType, err := normalizeModelSourceType(req.SourceType)
	if err != nil {
		return nil, err
	}
	sourceCode, adapter, err := s.normalizeSourceTarget(ctx, sourceType, req.SourceCode, req.Adapter)
	if err != nil {
		return nil, err
	}
	authType, err := normalizeModelSourceAuthType(req.AuthType)
	if err != nil {
		return nil, err
	}
	imageMode, err := normalizeProviderRouteImageAPIModeForConfig(req.ImageAPIMode)
	if err != nil {
		return nil, errcode.InvalidParam.WithMsg("图片调用模式" + err.Error())
	}
	strategy, err := normalizeRouteStrategyForConfig(req.Strategy)
	if err != nil {
		return nil, errcode.InvalidParam.WithMsg(err.Error())
	}
	priority := req.Priority
	if priority == 0 {
		priority = 100
	}
	weight := req.Weight
	if weight == 0 {
		weight = 100
	}
	status := int8(model.ModelSourceStatusEnabled)
	if req.Status != nil {
		status = *req.Status
	}
	upstreamModel := strings.TrimSpace(req.UpstreamModel)
	if err := s.validateModelSourceCompatibility(ctx, modelItem, sourceType, sourceCode, adapter, upstreamModel); err != nil {
		return nil, err
	}
	if err := s.ensureNoDuplicateModelSource(ctx, modelItem, 0, modelCode, sourceType, sourceCode, adapter, authType, imageMode, upstreamModel); err != nil {
		return nil, err
	}
	item := &model.ModelSourceMapping{
		ModelCode:     modelCode,
		SourceType:    sourceType,
		SourceCode:    sourceCode,
		UpstreamModel: upstreamModel,
		Adapter:       adapter,
		AuthType:      authType,
		ImageAPIMode:  imageMode,
		Strategy:      strategy,
		Priority:      priority,
		Weight:        weight,
		Status:        status,
	}
	if remark := strings.TrimSpace(req.Remark); remark != "" {
		item.Remark = &remark
	}
	if err := s.sourceRepo.Create(ctx, item); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	return item, nil
}

func (s *ModelGatewayAdminService) dryRunCandidate(ctx context.Context, index int, item *model.ModelCatalog, entryKind string, source *model.ModelSourceMapping) dto.ModelGatewayDryRunCandidate {
	upstreamModel := strings.TrimSpace(source.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(item.UpstreamDefaultModel)
	}
	if upstreamModel == "" {
		upstreamModel = item.ModelCode
	}
	candidate := dto.ModelGatewayDryRunCandidate{
		Index:         index,
		SourceType:    source.SourceType,
		SourceCode:    source.SourceCode,
		UpstreamModel: upstreamModel,
		Adapter:       source.Adapter,
		AuthType:      source.AuthType,
		ImageAPIMode:  source.ImageAPIMode,
		Strategy:      source.Strategy,
		Priority:      source.Priority,
		Weight:        source.Weight,
		Status:        source.Status,
	}
	if source.Status != model.ModelSourceStatusEnabled {
		candidate.SkipReason = "来源映射已停用"
		return candidate
	}
	switch source.SourceType {
	case model.ModelSourceTypeAPIChannel:
		return s.dryRunAPIChannelCandidate(ctx, candidate, item.ModelCode, upstreamModel, entryKind)
	case model.ModelSourceTypeAccountPool:
		return s.dryRunAccountPoolCandidate(ctx, candidate, item, upstreamModel)
	default:
		candidate.SkipReason = "来源类型不支持"
		return candidate
	}
}

func (s *ModelGatewayAdminService) dryRunAPIChannelCandidate(ctx context.Context, candidate dto.ModelGatewayDryRunCandidate, publicModel, upstreamModel, entryKind string) dto.ModelGatewayDryRunCandidate {
	ch, err := s.apiChannelRepo.GetByCode(ctx, candidate.SourceCode)
	if err != nil {
		candidate.SkipReason = "API 渠道不存在或已删除"
		return candidate
	}
	candidate.SourceName = ch.Name
	if candidate.Adapter == "" {
		candidate.Adapter = ch.Adapter
	}
	if ch.Status != model.APIChannelStatusEnabled {
		candidate.SkipReason = "API 渠道已停用"
		return candidate
	}
	if reason := apiChannelAdapterModelKindSkipReason(entryKind, candidate.Adapter); reason != "" {
		candidate.SkipReason = reason
		return candidate
	}
	if !modelGatewayListAllows(parseStringListJSON(ch.Models), publicModel, upstreamModel) {
		candidate.SkipReason = "API 渠道模型白名单不包含该模型"
		return candidate
	}
	if !modelGatewayCapabilityAllows(parseStringListJSON(ch.Capabilities), entryKind) {
		candidate.SkipReason = "API 渠道能力不匹配"
		return candidate
	}
	state := inspectAPIChannelOperational(ctx, s.apiChannelRepo, candidate.SourceCode)
	candidate.CandidateAccounts = state.CredentialTotal()
	candidate.AvailableAccounts = state.UsableCredentials()
	if reason := apiChannelOperationalSkipReason(state); reason != "" {
		candidate.SkipReason = reason
		return candidate
	}
	candidate.Available = true
	return candidate
}

func (s *ModelGatewayAdminService) dryRunAccountPoolCandidate(ctx context.Context, candidate dto.ModelGatewayDryRunCandidate, item *model.ModelCatalog, upstreamModel string) dto.ModelGatewayDryRunCandidate {
	candidate.SourceName = accountPoolLabel(candidate.SourceCode)
	if reason := accountPoolSourceMismatchReason(item, candidate.SourceCode, upstreamModel); reason != "" {
		candidate.SkipReason = reason
		return candidate
	}
	if s.accountRepo == nil {
		candidate.SkipReason = "账号池仓储未初始化"
		return candidate
	}
	accounts, err := s.accountRepo.AvailableByProvider(ctx, candidate.SourceCode)
	if err != nil {
		candidate.SkipReason = "账号池读取失败"
		return candidate
	}
	candidate.CandidateAccounts = len(accounts)
	available := 0
	for _, acc := range accounts {
		if matchesRouteAuthType(acc, candidate.AuthType) && accountAllowsRouteModel(acc, item.ModelCode, upstreamModel) {
			available++
		}
	}
	candidate.AvailableAccounts = available
	if len(accounts) == 0 {
		candidate.SkipReason = "当前账号池没有可调度账号"
		return candidate
	}
	if available == 0 {
		candidate.SkipReason = "账号池存在账号，但认证类型或模型白名单过滤后无可用账号"
		return candidate
	}
	candidate.Available = true
	return candidate
}

func (s *ModelGatewayAdminService) UpdateSource(ctx context.Context, id uint64, req *dto.ModelSourceUpdateReq) error {
	current, err := s.sourceRepo.GetByID(ctx, id)
	if err != nil {
		return errcode.ResourceMissing
	}
	fields := map[string]any{}
	modelCode := current.ModelCode
	sourceType := current.SourceType
	sourceCode := current.SourceCode
	adapter := current.Adapter
	upstreamModel := current.UpstreamModel
	authType := current.AuthType
	imageMode := current.ImageAPIMode
	if req.ModelCode != nil {
		nextModelCode, err := normalizeModelGatewayCode(*req.ModelCode, "模型编码")
		if err != nil {
			return err
		}
		if _, err := s.modelRepo.GetByCode(ctx, nextModelCode); err != nil {
			return errcode.InvalidParam.WithMsg("模型库中不存在该模型编码")
		}
		modelCode = nextModelCode
		fields["model_code"] = nextModelCode
	}
	if req.SourceType != nil {
		nextType, err := normalizeModelSourceType(*req.SourceType)
		if err != nil {
			return err
		}
		sourceType = nextType
	}
	if req.SourceCode != nil {
		sourceCode = *req.SourceCode
	}
	if req.Adapter != nil {
		adapter = *req.Adapter
	}
	if req.SourceType != nil || req.SourceCode != nil || req.Adapter != nil {
		normalizedCode, normalizedAdapter, err := s.normalizeSourceTarget(ctx, sourceType, sourceCode, adapter)
		if err != nil {
			return err
		}
		sourceCode = normalizedCode
		adapter = normalizedAdapter
		fields["source_type"] = sourceType
		fields["source_code"] = normalizedCode
		fields["adapter"] = normalizedAdapter
	}
	if req.UpstreamModel != nil {
		upstreamModel = strings.TrimSpace(*req.UpstreamModel)
		fields["upstream_model"] = upstreamModel
	}
	if req.AuthType != nil {
		nextAuthType, err := normalizeModelSourceAuthType(*req.AuthType)
		if err != nil {
			return err
		}
		authType = nextAuthType
		fields["auth_type"] = nextAuthType
	}
	if req.ImageAPIMode != nil {
		nextImageMode, err := normalizeProviderRouteImageAPIModeForConfig(*req.ImageAPIMode)
		if err != nil {
			return errcode.InvalidParam.WithMsg("图片调用模式" + err.Error())
		}
		imageMode = nextImageMode
		fields["image_api_mode"] = nextImageMode
	}
	if req.Strategy != nil {
		strategy, err := normalizeRouteStrategyForConfig(*req.Strategy)
		if err != nil {
			return errcode.InvalidParam.WithMsg(err.Error())
		}
		fields["strategy"] = strategy
	}
	if req.Priority != nil {
		fields["priority"] = *req.Priority
	}
	if req.Weight != nil {
		fields["weight"] = *req.Weight
	}
	if req.Status != nil {
		fields["status"] = *req.Status
	}
	if req.Remark != nil {
		if remark := strings.TrimSpace(*req.Remark); remark == "" {
			fields["remark"] = nil
		} else {
			fields["remark"] = remark
		}
	}
	modelItem, err := s.modelRepo.GetByCode(ctx, modelCode)
	if err != nil {
		return errcode.InvalidParam.WithMsg("模型库中不存在该模型编码")
	}
	if err := s.validateModelSourceCompatibility(ctx, modelItem, sourceType, sourceCode, adapter, upstreamModel); err != nil {
		return err
	}
	if err := s.ensureNoDuplicateModelSource(ctx, modelItem, id, modelCode, sourceType, sourceCode, adapter, authType, imageMode, upstreamModel); err != nil {
		return err
	}
	if err := s.sourceRepo.Update(ctx, id, fields); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func (s *ModelGatewayAdminService) DeleteSource(ctx context.Context, id uint64) error {
	if _, err := s.sourceRepo.GetByID(ctx, id); err != nil {
		return errcode.ResourceMissing
	}
	if err := s.sourceRepo.SoftDelete(ctx, id); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func (s *ModelGatewayAdminService) validateModelSourceCompatibility(ctx context.Context, item *model.ModelCatalog, sourceType, sourceCode, adapter, upstreamModel string) error {
	effectiveUpstream := strings.TrimSpace(upstreamModel)
	if effectiveUpstream == "" {
		effectiveUpstream = strings.TrimSpace(item.UpstreamDefaultModel)
	}
	if effectiveUpstream == "" {
		effectiveUpstream = item.ModelCode
	}
	switch sourceType {
	case model.ModelSourceTypeAPIChannel:
		ch, err := s.apiChannelRepo.GetByCode(ctx, sourceCode)
		if err != nil {
			return errcode.InvalidParam.WithMsg("API 渠道不存在或已删除")
		}
		effectiveAdapter := strings.TrimSpace(adapter)
		if effectiveAdapter == "" {
			effectiveAdapter = ch.Adapter
		}
		if isTextLikeModelKind(item.EntryKind) && effectiveAdapter != model.APIChannelAdapterOpenAIChat {
			return errcode.InvalidParam.WithMsg("文字/对话模型的 API 渠道目前只支持 OpenAI 兼容 Chat 协议")
		}
		if reason := apiChannelAdapterModelKindSkipReason(item.EntryKind, effectiveAdapter); reason != "" {
			return errcode.InvalidParam.WithMsg(reason)
		}
		if !modelGatewayListAllows(parseStringListJSON(ch.Models), item.ModelCode, effectiveUpstream) {
			return errcode.InvalidParam.WithMsg("API 渠道模型白名单不包含该模型或上游模型")
		}
		if !modelGatewayCapabilityAllows(parseStringListJSON(ch.Capabilities), item.EntryKind) {
			return errcode.InvalidParam.WithMsg("API 渠道能力与模型入口不匹配")
		}
	case model.ModelSourceTypeAccountPool:
		if reason := accountPoolSourceMismatchReason(item, sourceCode, effectiveUpstream); reason != "" {
			return errcode.InvalidParam.WithMsg(reason)
		}
	}
	return nil
}

func (s *ModelGatewayAdminService) ensureNoDuplicateModelSource(ctx context.Context, item *model.ModelCatalog, excludeID uint64, modelCode, sourceType, sourceCode, adapter, authType, imageMode, upstreamModel string) error {
	for page := 1; ; page++ {
		sources, total, err := s.sourceRepo.List(ctx, repo.ModelSourceListFilter{
			ModelCode: modelCode,
			Page:      page,
			PageSize:  500,
		})
		if err != nil {
			return errcode.DBError.Wrap(err)
		}
		if dup := duplicateModelSourceMapping(item, sources, excludeID, modelCode, sourceType, sourceCode, adapter, authType, imageMode, upstreamModel); dup != nil {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("已存在相同来源映射（ID %d），请调整现有映射或先删除重复项", dup.ID))
		}
		if int64(page*500) >= total || len(sources) == 0 {
			break
		}
	}
	return nil
}

func (s *ModelGatewayAdminService) normalizeSourceTarget(ctx context.Context, sourceType, sourceCodeValue, adapterValue string) (string, string, error) {
	sourceCode, err := normalizeModelGatewayCode(sourceCodeValue, "来源编码")
	if err != nil {
		return "", "", err
	}
	switch sourceType {
	case model.ModelSourceTypeAPIChannel:
		ch, err := s.apiChannelRepo.GetByCode(ctx, sourceCode)
		if err != nil {
			return "", "", errcode.InvalidParam.WithMsg("API 渠道不存在或已删除")
		}
		adapter := strings.TrimSpace(adapterValue)
		if adapter == "" {
			adapter = ch.Adapter
		}
		normalized, err := normalizeAPIChannelAdapter(adapter)
		if err != nil {
			return "", "", err
		}
		return sourceCode, normalized, nil
	case model.ModelSourceTypeAccountPool:
		switch sourceCode {
		case model.ProviderGPT, model.ProviderGROK:
			return sourceCode, strings.TrimSpace(adapterValue), nil
		default:
			return "", "", errcode.InvalidParam.WithMsg("账号池来源当前只能是 gpt 或 grok")
		}
	default:
		return "", "", errcode.InvalidParam.WithMsg("来源类型不支持")
	}
}

func normalizeModelGatewayCode(value, label string) (string, error) {
	code := strings.ToLower(strings.TrimSpace(value))
	if code == "" {
		return "", errcode.InvalidParam.WithMsg(label + "不能为空")
	}
	for _, r := range code {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", errcode.InvalidParam.WithMsg(label + "只能包含字母、数字、点、下划线和短横线")
	}
	return code, nil
}

func normalizeModelGatewayKind(value string) (string, error) {
	kind := normalizeModelGatewayKindLoose(value)
	switch kind {
	case model.ModelCatalogKindText, model.ModelCatalogKindImage, model.ModelCatalogKindVideo, model.ModelCatalogKindChat:
		return kind, nil
	default:
		return "", errcode.InvalidParam.WithMsg("入口类型只能是 text/image/video/chat")
	}
}

func normalizeModelGatewayKindLoose(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "文字", "text":
		return model.ModelCatalogKindText
	case "图片", "image":
		return model.ModelCatalogKindImage
	case "视频", "video":
		return model.ModelCatalogKindVideo
	case "对话", "chat":
		return model.ModelCatalogKindChat
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeModelPricingMode(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		return model.ModelCatalogPricingFixed, nil
	}
	switch mode {
	case model.ModelCatalogPricingFixed, model.ModelCatalogPricingToken, model.ModelCatalogPricingChar, model.ModelCatalogPricingMatrix, model.ModelCatalogPricingManual:
		return mode, nil
	default:
		return "", errcode.InvalidParam.WithMsg("计价方式只能是 fixed/token/char/matrix/manual")
	}
}

func normalizeModelSourceType(value string) (string, error) {
	sourceType := normalizeModelSourceTypeLoose(value)
	switch sourceType {
	case model.ModelSourceTypeAPIChannel, model.ModelSourceTypeAccountPool:
		return sourceType, nil
	default:
		return "", errcode.InvalidParam.WithMsg("来源类型只能是 api_channel 或 account_pool")
	}
}

func normalizeModelSourceTypeLoose(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "api", "api-channel", "api_channel":
		return model.ModelSourceTypeAPIChannel
	case "account", "account-pool", "account_pool", "pool":
		return model.ModelSourceTypeAccountPool
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeModelSourceAuthType(value string) (string, error) {
	authType := strings.ToLower(strings.TrimSpace(value))
	switch authType {
	case "", model.AuthTypeAPIKey, model.AuthTypeCookie, model.AuthTypeOAuth:
		return authType, nil
	default:
		return "", errcode.InvalidParam.WithMsg("认证类型只能是 api_key/cookie/oauth 或留空")
	}
}

func normalizeModelGatewayStringList(values []string) []string {
	return normalizeStringList(values)
}

func marshalJSONValue(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", errcode.InvalidParam.Wrap(err)
	}
	if string(raw) == "null" || string(raw) == "\"\"" {
		return "", nil
	}
	return string(raw), nil
}

func marshalCatalogParametersSchema(kind string, value any) (string, error) {
	raw, err := marshalJSONValue(value)
	if err != nil {
		return "", err
	}
	if err := validateCatalogParametersSchema(kind, raw); err != nil {
		return "", err
	}
	return raw, nil
}

func marshalCatalogPriceRules(kind, modelCode, pricingMode string, status int8, value any) (string, error) {
	raw, err := marshalJSONValue(value)
	if err != nil {
		return "", err
	}
	if err := validateCatalogPricingConfigurationForStatus(kind, modelCode, pricingMode, status, raw); err != nil {
		return "", err
	}
	return raw, nil
}

func validateCatalogParametersSchema(kind, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return errcode.InvalidParam.WithMsg("参数 Schema 必须是合法 JSON")
	}
	var controls []any
	switch v := value.(type) {
	case []any:
		controls = v
	case map[string]any:
		rawControls, ok := v["controls"]
		if !ok {
			return errcode.InvalidParam.WithMsg("参数 Schema 必须是控件数组，或包含 controls 数组的对象")
		}
		rows, ok := rawControls.([]any)
		if !ok {
			return errcode.InvalidParam.WithMsg("参数 Schema 的 controls 必须是数组")
		}
		controls = rows
	default:
		return errcode.InvalidParam.WithMsg("参数 Schema 必须是控件数组，或包含 controls 数组的对象")
	}
	if len(controls) > 50 {
		return errcode.InvalidParam.WithMsg("参数 Schema 控件不能超过 50 个")
	}
	for i, control := range controls {
		if err := validateCatalogParameterControl(kind, i+1, control); err != nil {
			return err
		}
	}
	return nil
}

func validateCatalogParameterControl(kind string, index int, value any) error {
	row, ok := value.(map[string]any)
	if !ok {
		return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个控件必须是对象", index))
	}
	key := strings.TrimSpace(schemaString(row["key"]))
	if key == "" {
		key = strings.TrimSpace(schemaString(row["name"]))
	}
	if key == "" {
		return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个控件缺少 key", index))
	}
	if len(key) > 64 {
		return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个控件 key 不能超过 64 个字符", index))
	}
	if !isCatalogParameterKey(key) {
		return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个控件 key 只能包含字母、数字、点、下划线和短横线", index))
	}
	if isSensitiveParameterKey(key) {
		return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个控件 key 不能使用敏感凭证字段", index))
	}
	controlType := strings.ToLower(strings.TrimSpace(schemaString(row["type"])))
	if controlType == "" {
		controlType = "number"
	}
	switch controlType {
	case "number", "integer", "float", "slider", "select", "enum", "boolean", "switch", "text", "string":
	default:
		return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个控件 type 不支持", index))
	}
	if (controlType == "select" || controlType == "enum") && !schemaHasOptions(row["options"]) {
		return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个选择控件必须配置 options", index))
	}
	if err := validateCatalogParameterModes(kind, index, row["modes"]); err != nil {
		return err
	}
	minValue, hasMin := schemaNumber(row["min"])
	maxValue, hasMax := schemaNumber(row["max"])
	if hasMin && hasMax && minValue > maxValue {
		return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个控件 min 不能大于 max", index))
	}
	return nil
}

func validateCatalogParameterModes(kind string, index int, value any) error {
	if value == nil {
		return nil
	}
	rows, ok := value.([]any)
	if !ok {
		return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个控件 modes 必须是数组", index))
	}
	if len(rows) == 0 {
		return nil
	}
	frontendKind := catalogFrontendParameterKind(kind)
	matched := false
	for _, row := range rows {
		mode := normalizeModelGatewayKindLoose(schemaString(row))
		if mode == "" {
			continue
		}
		if mode != model.ModelCatalogKindText && mode != model.ModelCatalogKindImage && mode != model.ModelCatalogKindVideo {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个控件 modes 只能包含 text/image/video", index))
		}
		if mode == frontendKind {
			matched = true
		}
	}
	if !matched {
		return errcode.InvalidParam.WithMsg(fmt.Sprintf("参数 Schema 第 %d 个控件 modes 必须包含当前前台入口 %s", index, frontendKind))
	}
	return nil
}

func catalogFrontendParameterKind(kind string) string {
	switch normalizeModelGatewayKindLoose(kind) {
	case model.ModelCatalogKindImage:
		return model.ModelCatalogKindImage
	case model.ModelCatalogKindVideo:
		return model.ModelCatalogKindVideo
	default:
		return model.ModelCatalogKindText
	}
}

func isCatalogParameterKey(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func isSensitiveParameterKey(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("-", "_", ".", "_").Replace(normalized)
	switch normalized {
	case "api_key", "apikey", "key", "secret", "token", "access_token", "refresh_token", "credential", "credential_enc", "password", "authorization", "bearer", "auth_token":
		return true
	}
	return strings.HasSuffix(normalized, "_api_key") ||
		strings.HasSuffix(normalized, "_secret") ||
		strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_credential") ||
		strings.HasSuffix(normalized, "_password")
}

func schemaString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func schemaNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	default:
		return 0, false
	}
}

func schemaHasOptions(value any) bool {
	rows, ok := value.([]any)
	if !ok {
		return false
	}
	for _, row := range rows {
		if optionValue(row) != "" {
			return true
		}
	}
	return false
}

func optionValue(value any) string {
	if row, ok := value.(map[string]any); ok {
		if v := strings.TrimSpace(schemaString(row["value"])); v != "" {
			return v
		}
		return strings.TrimSpace(schemaString(row["key"]))
	}
	return strings.TrimSpace(schemaString(value))
}

func validateCatalogPricingConfiguration(kind, modelCode, pricingMode, rawRules string) error {
	return validateCatalogPricingConfigurationForStatus(kind, modelCode, pricingMode, model.ModelCatalogStatusEnabled, rawRules)
}

func validateCatalogPricingConfigurationForStatus(kind, modelCode, pricingMode string, status int8, rawRules string) error {
	kind = normalizeModelGatewayKindLoose(kind)
	pricingMode, err := normalizeModelPricingMode(pricingMode)
	if err != nil {
		return err
	}
	if err := validateCatalogPricingModeForKind(kind, pricingMode); err != nil {
		return err
	}
	rawRules = strings.TrimSpace(rawRules)
	if rawRules == "" {
		if status == model.ModelCatalogStatusEnabled && pricingMode == model.ModelCatalogPricingMatrix && (kind == model.ModelCatalogKindImage || kind == model.ModelCatalogKindVideo) {
			return errcode.InvalidParam.WithMsg("矩阵计价必须配置价格规则")
		}
		return nil
	}
	if pricingMode != model.ModelCatalogPricingMatrix {
		return errcode.InvalidParam.WithMsg("价格规则仅在 matrix 矩阵计价模式下生效")
	}
	switch kind {
	case model.ModelCatalogKindImage:
		return validateCatalogImagePriceRules(modelCode, rawRules)
	case model.ModelCatalogKindVideo:
		return validateCatalogVideoPriceRules(modelCode, rawRules)
	default:
		return errcode.InvalidParam.WithMsg("价格规则仅支持图片/视频模型；文字模型请使用 token/char 输入输出单价")
	}
}

func validateCatalogPricingModeForKind(kind, pricingMode string) error {
	switch kind {
	case model.ModelCatalogKindText, model.ModelCatalogKindChat:
		if pricingMode == model.ModelCatalogPricingMatrix {
			return errcode.InvalidParam.WithMsg("文字/对话模型不能使用 matrix 矩阵计价")
		}
	case model.ModelCatalogKindImage, model.ModelCatalogKindVideo:
		if pricingMode == model.ModelCatalogPricingToken || pricingMode == model.ModelCatalogPricingChar {
			return errcode.InvalidParam.WithMsg("图片/视频模型不能使用 token/char 文字计价")
		}
	}
	return nil
}

type modelSourceRouteSignature struct {
	ModelCode     string
	SourceType    string
	SourceCode    string
	UpstreamModel string
	Adapter       string
	AuthType      string
	ImageAPIMode  string
}

func duplicateModelSourceMapping(item *model.ModelCatalog, sources []*model.ModelSourceMapping, excludeID uint64, modelCode, sourceType, sourceCode, adapter, authType, imageMode, upstreamModel string) *model.ModelSourceMapping {
	target := modelSourceSignature(item, modelCode, sourceType, sourceCode, upstreamModel, adapter, authType, imageMode)
	for _, source := range sources {
		if source == nil || source.ID == excludeID {
			continue
		}
		current := modelSourceSignature(item, source.ModelCode, source.SourceType, source.SourceCode, source.UpstreamModel, source.Adapter, source.AuthType, source.ImageAPIMode)
		if current == target {
			return source
		}
	}
	return nil
}

func modelSourceSignature(item *model.ModelCatalog, modelCode, sourceType, sourceCode, upstreamModel, adapter, authType, imageMode string) modelSourceRouteSignature {
	return modelSourceRouteSignature{
		ModelCode:     sourceSignatureToken(modelCode),
		SourceType:    sourceSignatureToken(sourceType),
		SourceCode:    sourceSignatureToken(sourceCode),
		UpstreamModel: sourceSignatureToken(effectiveModelSourceUpstream(item, upstreamModel)),
		Adapter:       sourceSignatureToken(adapter),
		AuthType:      sourceSignatureToken(authType),
		ImageAPIMode:  sourceSignatureToken(imageMode),
	}
}

func effectiveModelSourceUpstream(item *model.ModelCatalog, upstreamModel string) string {
	upstream := strings.TrimSpace(upstreamModel)
	if upstream != "" {
		return upstream
	}
	if item != nil {
		if upstream = strings.TrimSpace(item.UpstreamDefaultModel); upstream != "" {
			return upstream
		}
		return strings.TrimSpace(item.ModelCode)
	}
	return ""
}

func sourceSignatureToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateCatalogImagePriceRules(modelCode, raw string) error {
	var rules []ImagePriceRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return errcode.InvalidParam.WithMsg("图片价格规则必须是数组 JSON")
	}
	if len(rules) == 0 {
		return errcode.InvalidParam.WithMsg("图片价格规则不能为空")
	}
	expectedModel := normalizePriceToken(modelCode)
	enabled := 0
	for i, rule := range rules {
		if !imagePriceRuleEnabled(rule) {
			continue
		}
		enabled++
		if normalizePriceToken(rule.ModelCode) != expectedModel {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("图片价格规则第 %d 条 model_code 必须等于当前模型编码", i+1))
		}
		mode := normalizePriceToken(rule.Mode)
		if mode != "t2i" && mode != "i2i" {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("图片价格规则第 %d 条 mode 只能是 t2i 或 i2i", i+1))
		}
		if !isCatalogImageResolution(rule.Resolution) {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("图片价格规则第 %d 条 resolution 只能是 1K/2K/4K", i+1))
		}
		if rule.UnitPoints <= 0 {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("图片价格规则第 %d 条 unit_points 必须大于 0", i+1))
		}
	}
	if enabled == 0 {
		return errcode.InvalidParam.WithMsg("图片价格规则至少需要一条启用规则")
	}
	return nil
}

func validateCatalogVideoPriceRules(modelCode, raw string) error {
	var rules []VideoPriceRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return errcode.InvalidParam.WithMsg("视频价格规则必须是数组 JSON")
	}
	if len(rules) == 0 {
		return errcode.InvalidParam.WithMsg("视频价格规则不能为空")
	}
	expectedModel := normalizePriceToken(modelCode)
	enabled := 0
	for i, rule := range rules {
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		enabled++
		if code := normalizePriceToken(rule.ModelCode); code != "" && code != expectedModel {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("视频价格规则第 %d 条 model_code 必须留空或等于当前模型编码", i+1))
		}
		mode := normalizePriceToken(rule.Mode)
		if mode != "" && mode != "t2v" && mode != "i2v" {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("视频价格规则第 %d 条 mode 只能留空、t2v 或 i2v", i+1))
		}
		if strings.TrimSpace(rule.Resolution) != "" && !isCatalogImageResolution(rule.Resolution) {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("视频价格规则第 %d 条 resolution 只能留空或使用 1K/2K/4K", i+1))
		}
		if rule.DurationSec < 0 {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("视频价格规则第 %d 条 duration_sec 不能小于 0", i+1))
		}
		if rule.UnitPoints <= 0 {
			return errcode.InvalidParam.WithMsg(fmt.Sprintf("视频价格规则第 %d 条 unit_points 必须大于 0", i+1))
		}
	}
	if enabled == 0 {
		return errcode.InvalidParam.WithMsg("视频价格规则至少需要一条启用规则")
	}
	return nil
}

func isCatalogImageResolution(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "1", "1K", "2K", "4K":
		return true
	default:
		return false
	}
}

func parseModelGatewayJSON(raw *string) any {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	var value any
	if err := json.Unmarshal([]byte(*raw), &value); err != nil {
		return nil
	}
	return value
}

func modelCatalogResp(item *model.ModelCatalog) *dto.ModelCatalogResp {
	resp := &dto.ModelCatalogResp{
		ID:                   item.ID,
		ModelCode:            item.ModelCode,
		DisplayName:          item.DisplayName,
		EntryKind:            item.EntryKind,
		ProviderHint:         item.ProviderHint,
		UpstreamDefaultModel: item.UpstreamDefaultModel,
		Capabilities:         parseStringListJSON(item.Capabilities),
		ParametersSchema:     parseModelGatewayJSON(item.ParametersSchema),
		PricingMode:          item.PricingMode,
		UnitPoints:           item.UnitPoints,
		InputUnitPoints:      item.InputUnitPoints,
		OutputUnitPoints:     item.OutputUnitPoints,
		PriceRules:           parseModelGatewayJSON(item.PriceRules),
		MinPlan:              item.MinPlan,
		Tags:                 parseStringListJSON(item.Tags),
		SortOrder:            item.SortOrder,
		Visible:              item.Visible,
		Status:               item.Status,
		CreatedAt:            item.CreatedAt.Unix(),
		UpdatedAt:            item.UpdatedAt.Unix(),
	}
	if item.Description != nil {
		resp.Description = *item.Description
	}
	return resp
}

func modelSourceResp(item *model.ModelSourceMapping) *dto.ModelSourceResp {
	resp := &dto.ModelSourceResp{
		ID:            item.ID,
		ModelCode:     item.ModelCode,
		SourceType:    item.SourceType,
		SourceCode:    item.SourceCode,
		UpstreamModel: item.UpstreamModel,
		Adapter:       item.Adapter,
		AuthType:      item.AuthType,
		ImageAPIMode:  item.ImageAPIMode,
		Strategy:      item.Strategy,
		Priority:      item.Priority,
		Weight:        item.Weight,
		Status:        item.Status,
		CreatedAt:     item.CreatedAt.Unix(),
		UpdatedAt:     item.UpdatedAt.Unix(),
	}
	if item.Remark != nil {
		resp.Remark = *item.Remark
	}
	return resp
}

func modelGatewayListAllows(list []string, publicModel, upstreamModel string) bool {
	if len(list) == 0 {
		return true
	}
	allowed := map[string]bool{}
	for _, item := range list {
		value := strings.ToLower(strings.TrimSpace(item))
		if value != "" {
			allowed[value] = true
		}
	}
	if len(allowed) == 0 || allowed["*"] {
		return true
	}
	if publicModel != "" && allowed[strings.ToLower(strings.TrimSpace(publicModel))] {
		return true
	}
	return upstreamModel != "" && allowed[strings.ToLower(strings.TrimSpace(upstreamModel))]
}

func modelGatewayCapabilityAllows(caps []string, entryKind string) bool {
	if len(caps) == 0 {
		return true
	}
	allowed := map[string]bool{}
	for _, cap := range caps {
		value := strings.ToLower(strings.TrimSpace(cap))
		if value != "" {
			allowed[value] = true
		}
	}
	switch normalizeModelGatewayKindLoose(entryKind) {
	case model.ModelCatalogKindImage:
		return allowed["image"] || allowed["edit"]
	case model.ModelCatalogKindVideo:
		return allowed["video"]
	case model.ModelCatalogKindText, model.ModelCatalogKindChat:
		return allowed["chat"] || allowed["text"]
	default:
		return true
	}
}

func isTextLikeModelKind(kind string) bool {
	switch normalizeModelGatewayKindLoose(kind) {
	case model.ModelCatalogKindText, model.ModelCatalogKindChat:
		return true
	default:
		return false
	}
}

func apiChannelAdapterModelKindSkipReason(entryKind, adapter string) string {
	switch normalizeModelGatewayKindLoose(entryKind) {
	case model.ModelCatalogKindText, model.ModelCatalogKindChat:
		if strings.TrimSpace(adapter) != model.APIChannelAdapterOpenAIChat {
			return "文字/对话模型的 API 渠道目前只支持 OpenAI 兼容 Chat 协议"
		}
	case model.ModelCatalogKindImage:
		if imageAPIModeForAPIChannelAdapter(adapter) == "" {
			return "图片模型的 API 渠道目前只支持 OpenAI Images / OpenAI Responses / Nova / Pic2API 协议"
		}
	case model.ModelCatalogKindVideo:
		if strings.TrimSpace(adapter) != model.APIChannelAdapterOpenAIVideo {
			return "视频模型的 API 渠道目前只支持 OpenAI 兼容 Video 协议"
		}
	}
	return ""
}

func accountPoolLabel(sourceCode string) string {
	switch strings.ToLower(strings.TrimSpace(sourceCode)) {
	case model.ProviderGPT:
		return "GPT 账号池"
	case model.ProviderGROK:
		return "Grok 账号池"
	default:
		return sourceCode
	}
}

func accountPoolProviderHintAllows(providerHint, sourceCode string) bool {
	hint := strings.ToLower(strings.TrimSpace(providerHint))
	if hint == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(sourceCode)) {
	case model.ProviderGPT:
		return hint == "gpt" || hint == "openai" || hint == "chatgpt"
	case model.ProviderGROK:
		return hint == "grok" || hint == "xai"
	default:
		return true
	}
}

func accountPoolSourceMismatchReason(item *model.ModelCatalog, sourceCode, upstreamModel string) string {
	if item == nil {
		return ""
	}
	if !accountPoolProviderHintAllows(item.ProviderHint, sourceCode) {
		return "模型 Provider 提示与账号池不匹配"
	}
	if modelLooksLikeStandaloneAPIModel(item, upstreamModel) {
		return "MiMo/DeepSeek 等官方接口模型应挂到 API 渠道"
	}
	return ""
}

func modelLooksLikeStandaloneAPIModel(item *model.ModelCatalog, upstreamModel string) bool {
	candidates := []string{
		item.ProviderHint,
		item.ModelCode,
		item.UpstreamDefaultModel,
		upstreamModel,
	}
	for _, candidate := range candidates {
		value := strings.ToLower(strings.TrimSpace(candidate))
		if strings.HasPrefix(value, "mimo") || strings.HasPrefix(value, "deepseek") {
			return true
		}
	}
	return false
}

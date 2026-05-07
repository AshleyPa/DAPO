package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/errcode"
)

type PromptGalleryService struct {
	repo *repo.PromptGalleryRepo
}

func NewPromptGalleryService(r *repo.PromptGalleryRepo) *PromptGalleryService {
	return &PromptGalleryService{repo: r}
}

func (s *PromptGalleryService) ListAdmin(ctx context.Context, req *dto.PromptGalleryListReq) ([]*dto.PromptGalleryResp, int64, error) {
	rows, total, err := s.repo.List(ctx, repo.PromptGalleryListFilter{
		Keyword:  req.Keyword,
		Modality: normalizeModality(req.Modality),
		Category: strings.TrimSpace(req.Category),
		Locale:   strings.TrimSpace(req.Locale),
		Status:   req.Status,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		return nil, 0, errcode.DBError.Wrap(err)
	}
	return promptGalleryRespList(rows), total, nil
}

func (s *PromptGalleryService) ListPublic(ctx context.Context, req *dto.PublicPromptGalleryListReq) ([]*dto.PromptGalleryResp, error) {
	modality := normalizeModality(req.Modality)
	if modality == "" {
		modality = model.PromptGalleryModalityImage
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	rows, _, err := s.repo.List(ctx, repo.PromptGalleryListFilter{
		Modality:    modality,
		Category:    strings.TrimSpace(req.Category),
		Locale:      normalizeLocale(req.Locale),
		OnlyEnabled: true,
		Limit:       limit,
	})
	if err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	return promptGalleryRespList(rows), nil
}

func (s *PromptGalleryService) Create(ctx context.Context, req *dto.PromptGalleryCreateReq, adminID uint64) (*model.PromptGalleryItem, error) {
	row, err := buildPromptGalleryItem(req, adminID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return nil, errcode.DBError.Wrap(err)
	}
	return row, nil
}

func (s *PromptGalleryService) Update(ctx context.Context, id uint64, req *dto.PromptGalleryUpdateReq, adminID uint64) error {
	fields, err := buildPromptGalleryUpdate(req, adminID)
	if err != nil {
		return err
	}
	if err := s.repo.Update(ctx, id, fields); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func (s *PromptGalleryService) Delete(ctx context.Context, id uint64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func (s *PromptGalleryService) Reorder(ctx context.Context, req *dto.PromptGalleryReorderReq, adminID uint64) error {
	items := make(map[uint64]int, len(req.Items))
	for _, item := range req.Items {
		items[item.ID] = item.SortOrder
	}
	if err := s.repo.Reorder(ctx, items, adminID); err != nil {
		return errcode.DBError.Wrap(err)
	}
	return nil
}

func buildPromptGalleryItem(req *dto.PromptGalleryCreateReq, adminID uint64) (*model.PromptGalleryItem, error) {
	modality := normalizeModality(req.Modality)
	if modality == "" {
		return nil, errcode.InvalidParam.WithMsg("modality is required")
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, errcode.InvalidParam.WithMsg("title is required")
	}
	coverURL := strings.TrimSpace(req.CoverURL)
	if coverURL == "" {
		return nil, errcode.InvalidParam.WithMsg("cover_url is required")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errcode.InvalidParam.WithMsg("prompt is required")
	}
	tags, err := marshalTags(req.Tags)
	if err != nil {
		return nil, err
	}
	variables, err := marshalVariables(req.VariablesSchema)
	if err != nil {
		return nil, err
	}
	status := int8(model.PromptGalleryStatusEnabled)
	if req.Status != nil {
		status = *req.Status
	}
	uid := adminID
	row := &model.PromptGalleryItem{
		Modality:        modality,
		Category:        normalizeCategory(req.Category),
		Title:           title,
		Subtitle:        optionalString(req.Subtitle),
		CoverURL:        coverURL,
		Prompt:          prompt,
		Tags:            tags,
		VariablesSchema: variables,
		SortOrder:       req.SortOrder,
		Status:          status,
		Locale:          normalizeLocale(req.Locale),
		CreatedBy:       &uid,
		UpdatedBy:       &uid,
	}
	return row, nil
}

func buildPromptGalleryUpdate(req *dto.PromptGalleryUpdateReq, adminID uint64) (map[string]any, error) {
	fields := map[string]any{"updated_by": adminID}
	if req.Modality != nil {
		modality := normalizeModality(*req.Modality)
		if modality == "" {
			return nil, errcode.InvalidParam.WithMsg("invalid modality")
		}
		fields["modality"] = modality
	}
	if req.Category != nil {
		fields["category"] = normalizeCategory(*req.Category)
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return nil, errcode.InvalidParam.WithMsg("title is required")
		}
		fields["title"] = title
	}
	if req.Subtitle != nil {
		fields["subtitle"] = optionalString(*req.Subtitle)
	}
	if req.CoverURL != nil {
		coverURL := strings.TrimSpace(*req.CoverURL)
		if coverURL == "" {
			return nil, errcode.InvalidParam.WithMsg("cover_url is required")
		}
		fields["cover_url"] = coverURL
	}
	if req.Prompt != nil {
		prompt := strings.TrimSpace(*req.Prompt)
		if prompt == "" {
			return nil, errcode.InvalidParam.WithMsg("prompt is required")
		}
		fields["prompt"] = prompt
	}
	if req.Tags != nil {
		tags, err := marshalTags(req.Tags)
		if err != nil {
			return nil, err
		}
		fields["tags"] = tags
	}
	if req.VariablesSchema != nil {
		variables, err := marshalVariables(req.VariablesSchema)
		if err != nil {
			return nil, err
		}
		fields["variables_schema"] = variables
	}
	if req.SortOrder != nil {
		fields["sort_order"] = *req.SortOrder
	}
	if req.Status != nil {
		fields["status"] = *req.Status
	}
	if req.Locale != nil {
		fields["locale"] = normalizeLocale(*req.Locale)
	}
	return fields, nil
}

func promptGalleryRespList(rows []*model.PromptGalleryItem) []*dto.PromptGalleryResp {
	out := make([]*dto.PromptGalleryResp, 0, len(rows))
	for _, row := range rows {
		out = append(out, promptGalleryResp(row))
	}
	return out
}

func promptGalleryResp(row *model.PromptGalleryItem) *dto.PromptGalleryResp {
	resp := &dto.PromptGalleryResp{
		ID:              row.ID,
		Modality:        row.Modality,
		Category:        row.Category,
		Title:           row.Title,
		CoverURL:        row.CoverURL,
		Prompt:          row.Prompt,
		Tags:            unmarshalTags(row.Tags),
		VariablesSchema: unmarshalVariables(row.VariablesSchema),
		SortOrder:       row.SortOrder,
		Status:          row.Status,
		Locale:          row.Locale,
		CreatedAt:       row.CreatedAt.Unix(),
		UpdatedAt:       row.UpdatedAt.Unix(),
	}
	if row.Subtitle != nil {
		resp.Subtitle = *row.Subtitle
	}
	return resp
}

func normalizeModality(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case model.PromptGalleryModalityImage:
		return model.PromptGalleryModalityImage
	case model.PromptGalleryModalityText:
		return model.PromptGalleryModalityText
	case model.PromptGalleryModalityVideo:
		return model.PromptGalleryModalityVideo
	default:
		return ""
	}
}

func normalizeCategory(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "default"
	}
	return v
}

func normalizeLocale(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "zh-CN"
	}
	return v
}

func optionalString(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func marshalTags(in []string) (string, error) {
	if in == nil {
		in = []string{}
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, tag := range in {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > 32 {
			return "", errcode.InvalidParam.WithMsg("tag is too long")
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", errcode.InvalidParam.Wrap(err)
	}
	return string(raw), nil
}

func marshalVariables(in map[string]any) (string, error) {
	if in == nil {
		in = map[string]any{}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return "", errcode.InvalidParam.Wrap(err)
	}
	return string(raw), nil
}

func unmarshalTags(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	return out
}

func unmarshalVariables(raw string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}

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

func (s *PromptGalleryService) SeedDefaults(ctx context.Context, adminID uint64) (int, error) {
	rows := make([]*model.PromptGalleryItem, 0, len(defaultPromptGallerySeeds))
	for _, seed := range defaultPromptGallerySeeds {
		exists, err := s.repo.ExistsNaturalKey(ctx, seed.Modality, seed.Title)
		if err != nil {
			return 0, errcode.DBError.Wrap(err)
		}
		if exists {
			continue
		}
		req := &dto.PromptGalleryCreateReq{
			Modality:  seed.Modality,
			Category:  seed.Category,
			Title:     seed.Title,
			CoverURL:  seed.CoverURL,
			Prompt:    seed.Prompt,
			Tags:      seed.Tags,
			SortOrder: seed.SortOrder,
			Status:    ptrInt8(model.PromptGalleryStatusEnabled),
			Locale:    "zh-CN",
		}
		row, err := buildPromptGalleryItem(req, adminID)
		if err != nil {
			return 0, err
		}
		rows = append(rows, row)
	}
	if err := s.repo.CreateMany(ctx, rows); err != nil {
		return 0, errcode.DBError.Wrap(err)
	}
	return len(rows), nil
}

type promptGallerySeed struct {
	Modality  string
	Category  string
	Title     string
	CoverURL  string
	Prompt    string
	Tags      []string
	SortOrder int
}

var defaultPromptGallerySeeds = []promptGallerySeed{
	{
		Modality: "image", Category: "product", Title: "极简产品广告", CoverURL: "/examples/case-1.jpg", SortOrder: 10,
		Tags: []string{"产品", "广告", "极简"},
		Prompt: `A minimalist product advertisement with a {argument name="product" default="fried chicken bucket"} placed on a clean white podium.

Background: soft gradient ({argument name="background gradient" default="light cream to white"}), clean studio.

Lighting: soft diffused, premium Apple-style.

Typography (center): "{argument name="headline" default="PURE CRUNCH"}"

Small text below: "Nothing extra. Just perfection."

Style: ultra clean, editorial minimal, high-end branding, 8K.`,
	},
	{
		Modality: "image", Category: "poster", Title: "城市海报", CoverURL: "/examples/case-2.jpg", SortOrder: 20,
		Tags:   []string{"城市", "海报", "春季"},
		Prompt: `A striking Spring 2026 city poster for Boston with an elegant celebratory mood and a bold contemporary design. On a clean off-white textured background with large areas of negative space, a miniature single sculler rows across the lower right corner of the image on a narrow ribbon of reflective water. The wake from the oar sweeps upward in a dynamic calligraphic curve, gradually transforming into the Charles River and then into a dreamlike hand-painted panorama of Boston. Inside this flowing river-shaped composition are iconic Boston elements: the Back Bay skyline, Beacon Hill brownstones, Acorn Street, Boston Public Garden, Swan Boats, Zakim Bridge, Fenway-inspired details, historic brick architecture, harbor ferries, and the city's waterfront atmosphere. Soft morning fog, golden spring light, subtle festive accents in crimson and gold, rich detail, layered depth, sophisticated city-poster aesthetics, fresh and refined, visually powerful but not overcrowded. Elegant typography in the lower left reads "SPRING 2026" with a vertical slogan "BOSTON, A CITY OF RIVER, MEMORY, AND INVENTION", text clear and beautifully composed, premium graphic design, 9:16`,
	},
	{
		Modality: "image", Category: "workflow", Title: "3D 手办工作流", CoverURL: "/examples/case-3.jpg", SortOrder: 30,
		Tags: []string{"3D", "手办", "工作流"},
		Prompt: `Photorealistic high-quality studio photo of a modern digital art workspace, showing the concept of "from 3D virtual character to real collectible figure."

In the foreground, a highly realistic collectible figurine of [Character Name / Character Identity] is placed on a round wooden display stand. The character has [facial features / appearance], [hairstyle], and a [expression / personality vibe]. The figure is wearing [outfit / costume]. The overall design is refined, premium, and instantly recognizable. The figurine should have realistic collectible statue quality, with subtle resin/sculpture material feel, while still looking highly believable and visually realistic.

The pose is [character pose], natural, stable, elegant, and display-worthy. Shot from a low-angle close-up perspective with slight wide-angle distortion, vertical composition, emphasizing the full figure, clothing structure, leg lines, and pose.

In the background, there is a professional 3D character design workstation with two large curved monitors. Both monitors must show the exact same character as the foreground figurine - same face, same hairstyle, same outfit, same pose, and same overall vibe - clearly expressing the idea of turning a digital 3D character into a real physical figure.

The left monitor shows a gray sculpt / clay model view in a professional 3D sculpting software interface, similar to ZBrush. The gray model must match the foreground figure exactly in character design, pose, outfit structure, and facial identity.

The right monitor shows the fully rendered colored version of the same character, also matching the foreground figure exactly in face, hairstyle, outfit, pose, and temperament. Together, the two monitors reinforce the workflow of "digital character design -> physical collectible statue."

On the desk are a keyboard, mouse, monitor arms, drawing tablet, stylus, and other 3D modeling tools. The workspace is clean, professional, and visually premium. Optional extra elements: [weapon / accessories / theme props / IP-style design details].

Lighting is a mix of soft studio lighting and indoor workspace lighting. The foreground figurine is evenly lit with clear facial and material detail, while the monitors emit cool-toned tech light. Overall mood is realistic, clean, premium, slightly shallow depth of field, ultra-detailed, emphasizing the collectible figure quality, professional 3D design studio atmosphere, and the visual concept of "from digital model to real figure."

photorealistic, ultra detailed, cinematic studio lighting, realistic figurine, collectible statue, 3D character design studio, from digital model to real figure, vertical composition`,
	},
	{
		Modality: "image", Category: "poster", Title: "鹿鼎记海报", CoverURL: "/examples/case-4.jpg", SortOrder: 40,
		Tags:   []string{"海报", "人物"},
		Prompt: "生成鹿鼎记海报，展现韦小宝跟老婆XXX，忠于原著的描述，夸大特点，强调女性的美艳和男性的气质",
	},
	{
		Modality: "image", Category: "infographic", Title: "人类演化图", CoverURL: "/examples/case-5.jpg", SortOrder: 50,
		Tags: []string{"信息图", "3D", "演化"},
		Prompt: `{
  "type": "evolutionary timeline infographic",
  "instruction": "Using REFERENCE_0 as a structural base, transform the flat vector design into a highly realistic 3D infographic. Replace the smooth ramps with distinct stone steps and upgrade all organisms to photorealistic 3D models.",
  "style": {
    "background": "{argument name=\"background style\" default=\"vintage textured parchment paper\"}",
    "staircase": "{argument name=\"staircase material\" default=\"realistic textured stone blocks\"}",
    "subjects": "{argument name=\"organism style\" default=\"highly detailed photorealistic 3D renders\"}"
  }
}`,
	},
	{
		Modality: "text", Category: "copywriting", Title: "产品卖点文案", CoverURL: "/examples/case-1.jpg", SortOrder: 10,
		Tags:   []string{"文案", "产品"},
		Prompt: "请为一个新消费品牌产品写 5 组高级、克制、有记忆点的产品卖点文案。要求：每组包含一句主标题、一句副标题、3 个卖点 bullet，语气简洁，不夸张，不使用空泛形容词。",
	},
	{
		Modality: "text", Category: "social", Title: "社媒发布文案", CoverURL: "/examples/case-2.jpg", SortOrder: 20,
		Tags:   []string{"社媒", "发布"},
		Prompt: "请把以下主题改写成适合小红书/朋友圈发布的文案：主题是【填写主题】。要求：开头有吸引力，正文自然可信，结尾有轻度行动号召，避免油腻营销感。",
	},
	{
		Modality: "text", Category: "script", Title: "视频脚本草稿", CoverURL: "/examples/case-3.jpg", SortOrder: 30,
		Tags:   []string{"脚本", "视频"},
		Prompt: "请为一个 30 秒短视频写脚本，主题是【填写主题】。输出结构：镜头编号、画面描述、旁白/字幕、节奏提示。整体风格清晰、紧凑、有画面感。",
	},
	{
		Modality: "video", Category: "product", Title: "产品动态展示", CoverURL: "/examples/case-1.jpg", SortOrder: 10,
		Tags:   []string{"视频", "产品"},
		Prompt: "A premium product reveal video. The product sits on a clean studio podium, soft directional lighting, slow cinematic camera push-in, subtle reflections, elegant minimal background, high-end commercial style, smooth motion, no text overlay.",
	},
	{
		Modality: "video", Category: "city", Title: "城市氛围短片", CoverURL: "/examples/case-2.jpg", SortOrder: 20,
		Tags:   []string{"城市", "视频"},
		Prompt: "A cinematic city atmosphere video at golden hour. Slow tracking shot through a modern urban street, warm light, gentle motion, refined documentary feeling, realistic details, calm premium mood, 16:9.",
	},
	{
		Modality: "video", Category: "reference", Title: "参考图轻运动", CoverURL: "/examples/case-3.jpg", SortOrder: 30,
		Tags:   []string{"参考图", "视频"},
		Prompt: "Animate the reference image with subtle natural motion. Keep the subject identity and composition stable, add gentle camera movement, realistic lighting changes, cinematic depth, smooth motion, no distortion.",
	},
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

func ptrInt8(v int8) *int8 {
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

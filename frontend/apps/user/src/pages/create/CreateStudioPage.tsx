import { Suspense, lazy, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation } from 'react-router-dom';
import {
  ArrowUp,
  Check,
  ChevronDown,
  FileImage,
  Loader2,
  Maximize2,
  Mic,
  MoreHorizontal,
  Paperclip,
  Play,
  Trash2,
  X,
} from 'lucide-react';
import clsx from 'clsx';

import BorderGlow from '../../components/reactbits/BorderGlow';
import { useEnsureLoggedIn } from '../../hooks/useEnsureLoggedIn';
import { ApiError } from '../../lib/api';
import { fmtRelative } from '../../lib/format';
import { genApi, promptGalleryApi } from '../../lib/services';
import type { GenerationTask, PromptGalleryItem, PublicModel } from '../../lib/types';
import { useAuthStore } from '../../stores/auth';
import { toast } from '../../stores/toast';

type StudioMode = 'image' | 'text' | 'video';
const Galaxy = lazy(() => import('../../components/reactbits/Galaxy'));
const CircularGallery = lazy(() => import('../../components/reactbits/CircularGallery'));
const ASCIIText = lazy(() => import('../../components/reactbits/ASCIIText'));

type PromptGalleryCard = {
  id?: number;
  title: string;
  subtitle?: string;
  image: string;
  prompt: string;
};

const GENERATING_PHRASES = [
  '正在为您设计中...',
  '灵感正在慢慢成形',
  '细节正在被认真打磨',
  '画面很快就会出现',
];

const IMAGE_MODELS = [
  { code: 'gpt-image-2', label: 'GPT Image 2', cost: 4 },
];

type SelectModel = {
  code: string;
  label: string;
  cost?: number;
  input?: number;
  output?: number;
};

const VIDEO_MODELS = [
  { code: 'grok-imagine-video', label: 'Grok Imagine 视频', cost: 20 },
  { code: 'vid-i2v', label: 'Grok 图生视频', cost: 20 },
];

const TEXT_MODELS = [
  { code: 'grok-4.20-fast', label: 'Grok Fast', input: 1, output: 3 },
  { code: 'grok-4.20-auto', label: 'Grok Auto', input: 1.5, output: 4.5 },
  { code: 'grok-4.20-expert', label: 'Grok Expert', input: 2, output: 6 },
  { code: 'grok-4.20-heavy', label: 'Grok Heavy', input: 4, output: 12 },
  { code: 'gpt-4o-mini', label: 'GPT 4o mini', input: 1, output: 3 },
];

const IMAGE_RATIOS = ['1:1', '3:2', '2:3', '4:3', '3:4', '5:4', '4:5', '16:9', '9:16', '21:9'] as const;
const IMAGE_RESOLUTIONS = ['1K', '2K', '4K'] as const;
const VIDEO_RATIOS = ['16:9', '9:16', '1:1'] as const;
const VIDEO_DURATIONS = [6, 10] as const;
const HISTORY_PAGE_SIZES = [20, 50, 100] as const;
type HistoryDeleteScope = 'before_3d' | 'before_7d' | 'all';
const TEXT_MAX_ATTACHMENTS = 5;
const VIDEO_MAX_ATTACHMENTS = 7;
const FALLBACK_PROMPT_GALLERY: Record<StudioMode, PromptGalleryCard[]> = {
  image: [
  {
    title: '极简产品广告',
    image: '/examples/case-1.jpg',
    prompt: `A minimalist product advertisement with a {argument name="product" default="fried chicken bucket"} placed on a clean white podium.

Background: soft gradient ({argument name="background gradient" default="light cream to white"}), clean studio.

Lighting: soft diffused, premium Apple-style.

Typography (center): "{argument name="headline" default="PURE CRUNCH"}"

Small text below: "Nothing extra. Just perfection."

Style: ultra clean, editorial minimal, high-end branding, 8K.`,
  },
  {
    title: '城市海报',
    image: '/examples/case-2.jpg',
    prompt: `A striking Spring 2026 city poster for Boston with an elegant celebratory mood and a bold contemporary design. On a clean off-white textured background with large areas of negative space, a miniature single sculler rows across the lower right corner of the image on a narrow ribbon of reflective water. The wake from the oar sweeps upward in a dynamic calligraphic curve, gradually transforming into the Charles River and then into a dreamlike hand-painted panorama of Boston. Inside this flowing river-shaped composition are iconic Boston elements: the Back Bay skyline, Beacon Hill brownstones, Acorn Street, Boston Public Garden, Swan Boats, Zakim Bridge, Fenway-inspired details, historic brick architecture, harbor ferries, and the city's waterfront atmosphere. Soft morning fog, golden spring light, subtle festive accents in crimson and gold, rich detail, layered depth, sophisticated city-poster aesthetics, fresh and refined, visually powerful but not overcrowded. Elegant typography in the lower left reads "SPRING 2026" with a vertical slogan "BOSTON, A CITY OF RIVER, MEMORY, AND INVENTION", text clear and beautifully composed, premium graphic design, 9:16`,
  },
  {
    title: '3D 手办工作流',
    image: '/examples/case-3.jpg',
    prompt: `Photorealistic high-quality studio photo of a modern digital art workspace, showing the concept of "from 3D virtual character to real collectible figure."

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
    title: '鹿鼎记海报',
    image: '/examples/case-4.jpg',
    prompt: '生成鹿鼎记海报，展现韦小宝跟老婆XXX，忠于原著的描述，夸大特点，强调女性的美艳和男性的气质',
  },
  {
    title: '人类演化图',
    image: '/examples/case-5.jpg',
    prompt: `{
  "type": "evolutionary timeline infographic",
  "instruction": "Using REFERENCE_0 as a structural base, transform the flat vector design into a highly realistic 3D infographic. Replace the smooth ramps with distinct stone steps and upgrade all organisms to photorealistic 3D models.",
  "style": {
    "background": "{argument name=\\"background style\\" default=\\"vintage textured parchment paper\\"}",
    "staircase": "{argument name=\\"staircase material\\" default=\\"realistic textured stone blocks\\"}",
    "subjects": "{argument name=\\"organism style\\" default=\\"highly detailed photorealistic 3D renders\\"}"
  },
  "layout": {
    "main_title": "{argument name=\\"main title\\" default=\\"人类演化\\"}",
    "sections": [
      {
        "position": "left sidebar",
        "count": 8,
        "labels": ["L0: 单细胞生命", "L1: 多细胞生物", "L2: 动物界", "L3: 脊索动物", "L4: 上陆革命", "L5: 哺乳纲", "L6: 人科演化", "L7: 智人纪元"]
      },
      {
        "position": "top right",
        "title": "获得的功能 / 失去的功能",
        "description": "Legend with plus and minus icons"
      },
      {
        "position": "bottom center",
        "title": "演化关键里程碑",
        "count": 6,
        "description": "Timeline with a silhouette graphic of 6 figures showing ape-to-human evolution"
      }
    ],
    "centerpiece": {
      "description": "Winding stone staircase with 25 numbered steps featuring specific organisms.",
      "count": 25,
      "notable_elements": [
        "Step 07: Jellyfish",
        "Step 09: Ammonite",
        "Step 10: Trilobite",
        "Step 24: Walking human",
        "Step 25: {argument name=\\"future evolution concept\\" default=\\"glowing cosmic silhouette with a question mark\\"}"
      ]
    }
  }
}`,
  },
  ],
  text: [
    {
      title: '产品卖点文案',
      image: '/examples/case-1.jpg',
      prompt: '请为一个新消费品牌产品写 5 组高级、克制、有记忆点的产品卖点文案。要求：每组包含一句主标题、一句副标题、3 个卖点 bullet，语气简洁，不夸张，不使用空泛形容词。',
    },
    {
      title: '社媒发布文案',
      image: '/examples/case-2.jpg',
      prompt: '请把以下主题改写成适合小红书/朋友圈发布的文案：主题是【填写主题】。要求：开头有吸引力，正文自然可信，结尾有轻度行动号召，避免油腻营销感。',
    },
    {
      title: '视频脚本草稿',
      image: '/examples/case-3.jpg',
      prompt: '请为一个 30 秒短视频写脚本，主题是【填写主题】。输出结构：镜头编号、画面描述、旁白/字幕、节奏提示。整体风格清晰、紧凑、有画面感。',
    },
  ],
  video: [
    {
      title: '产品动态展示',
      image: '/examples/case-1.jpg',
      prompt: 'A premium product reveal video. The product sits on a clean studio podium, soft directional lighting, slow cinematic camera push-in, subtle reflections, elegant minimal background, high-end commercial style, smooth motion, no text overlay.',
    },
    {
      title: '城市氛围短片',
      image: '/examples/case-2.jpg',
      prompt: 'A cinematic city atmosphere video at golden hour. Slow tracking shot through a modern urban street, warm light, gentle motion, refined documentary feeling, realistic details, calm premium mood, 16:9.',
    },
    {
      title: '参考图轻运动',
      image: '/examples/case-3.jpg',
      prompt: 'Animate the reference image with subtle natural motion. Keep the subject identity and composition stable, add gentle camera movement, realistic lighting changes, cinematic depth, smooth motion, no distortion.',
    },
  ],
};

export default function CreateStudioPage() {
  const location = useLocation();
  const qc = useQueryClient();
  const ensureLoggedIn = useEnsureLoggedIn();
  const refreshMe = useAuthStore((s) => s.refreshMe);
  const token = useAuthStore((s) => s.token);
  const motionProfile = useStudioMotionProfile();
  const compactMotion = !motionProfile.supportsRichEffects;

  const modelCatalog = useQuery({
    queryKey: ['gen.models'],
    queryFn: () => genApi.models(),
    staleTime: 60_000,
  });

  const imageModels = useMemo(() => modelsByKind(modelCatalog.data, 'image', IMAGE_MODELS), [modelCatalog.data]);
  const textModels = useMemo(() => modelsByKind(modelCatalog.data, 'text', TEXT_MODELS), [modelCatalog.data]);
  const videoModels = useMemo(() => modelsByKind(modelCatalog.data, 'video', VIDEO_MODELS), [modelCatalog.data]);

  const mode = modeFromPath(location.pathname);
  const [prompt, setPrompt] = useState('');
  const [textModel, setTextModel] = useState(TEXT_MODELS[0]!.code);
  const [imageModel, setImageModel] = useState(IMAGE_MODELS[0]!.code);
  const [videoModel, setVideoModel] = useState(VIDEO_MODELS[0]!.code);
  const [imageRatio, setImageRatio] = useState<(typeof IMAGE_RATIOS)[number]>('1:1');
  const [imageResolution, setImageResolution] = useState<(typeof IMAGE_RESOLUTIONS)[number]>('1K');
  const [videoRatio, setVideoRatio] = useState<(typeof VIDEO_RATIOS)[number]>('16:9');
  const [count, setCount] = useState(1);
  const [duration, setDuration] = useState<(typeof VIDEO_DURATIONS)[number]>(6);
  const [attachments, setAttachments] = useState<Array<{ id: string; name: string; dataUrl: string }>>([]);
  const [textResult, setTextResult] = useState('');
  const [task, setTask] = useState<GenerationTask | null>(null);
  const [historyPageSize, setHistoryPageSize] = useState<(typeof HISTORY_PAGE_SIZES)[number]>(20);
  const [preview, setPreview] = useState<{ url: string; type: 'image' | 'video'; title: string } | null>(null);
  const pollRef = useRef<number | null>(null);
  const promptRef = useRef<HTMLTextAreaElement | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const promptGallery = useQuery({
    queryKey: ['prompt-gallery', mode],
    queryFn: () => promptGalleryApi.list(mode),
    retry: false,
    staleTime: 60_000,
  });

  const promptGalleryCards = useMemo(() => {
    const remote = (promptGallery.data ?? [])
      .filter((item) => item.title && item.cover_url && item.prompt)
      .map(promptGalleryItemToCard);
    return remote.length ? remote : FALLBACK_PROMPT_GALLERY[mode];
  }, [promptGallery.data, mode]);

  useEffect(() => () => {
    if (pollRef.current) window.clearInterval(pollRef.current);
  }, []);

  useEffect(() => {
    setTask(null);
    setTextResult('');
    setAttachments([]);
  }, [mode]);

  useEffect(() => {
    if (imageModels.length && !imageModels.some((m) => m.code === imageModel)) setImageModel(imageModels[0]!.code);
    if (textModels.length && !textModels.some((m) => m.code === textModel)) setTextModel(textModels[0]!.code);
    if (videoModels.length && !videoModels.some((m) => m.code === videoModel)) setVideoModel(videoModels[0]!.code);
  }, [imageModel, imageModels, textModel, textModels, videoModel, videoModels]);

  useEffect(() => {
    const el = promptRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 260)}px`;
    el.style.overflowY = el.scrollHeight > 260 ? 'auto' : 'hidden';
  }, [prompt, mode]);

  const history = useQuery({
    queryKey: ['gen.history', 'studio', token, historyPageSize],
    enabled: !!token,
    queryFn: () => genApi.history({ kind: 'media', page: 1, page_size: historyPageSize }),
  });

  const deleteHistory = useMutation({
    mutationFn: (scope: HistoryDeleteScope) => genApi.deleteHistory(scope),
    onSuccess: (res, scope) => {
      const label = scope === 'before_3d' ? '3天前作品' : scope === 'before_7d' ? '7天前作品' : '全部作品';
      toast.success(`已删除 ${res.deleted} 条${label}`);
      setTask(null);
      qc.invalidateQueries({ queryKey: ['gen.history'] });
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : '删除失败'),
  });

  const createImage = useMutation({
    mutationFn: () => genApi.createImage({
      model: imageModel,
      prompt,
      count,
      ratio: imageRatio,
      ref_assets: attachments.map((item) => item.dataUrl),
      mode: attachments.length ? 'i2i' : 't2i',
      params: { resolution: imageResolution, quality: 'high' },
    }),
    onSuccess: (t) => handleTask(t),
    onError: (e) => toast.error(e instanceof ApiError ? e.message : '生成失败'),
  });

  const createVideo = useMutation({
    mutationFn: () => genApi.createVideo({ model: videoModel, prompt, duration, ratio: videoRatio, quality: 'hd', ref_assets: attachments.map((item) => item.dataUrl), mode: attachments.length ? 'i2v' : 't2v' }),
    onSuccess: (t) => handleTask(t),
    onError: (e) => toast.error(e instanceof ApiError ? e.message : '生成失败'),
  });

  const createText = useMutation({
    mutationFn: () => genApi.createText({ model: textModel, prompt, max_tokens: 1600, images: attachments.map((item) => item.dataUrl) }),
    onSuccess: async (res) => {
      setTextResult(res.content || '');
      toast.success('文字生成完成');
      await refreshMe();
      qc.invalidateQueries({ queryKey: ['gen.history'] });
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : '生成失败'),
  });

  const inProgress = task && (task.status === 0 || task.status === 1);
  const resultItems = useMemo(() => {
    const visible = (item: GenerationTask) => (item.kind === 'image' || item.kind === 'video') && item.status !== 3;
    const current = task?.results?.length && visible(task) ? [task] : [];
    const rest = (history.data?.list ?? []).filter(visible);
    return [...current, ...rest].filter((item, idx, arr) => arr.findIndex((x) => x.task_id === item.task_id) === idx);
  }, [history.data?.list, task]);

  const expectedCost = mode === 'video'
    ? Math.round(((videoModels.find((m) => m.code === videoModel)?.cost ?? 20) * duration) / 6)
    : mode === 'text'
      ? '按实际 Token'
      : (imageModels.find((m) => m.code === imageModel)?.cost ?? 4) * count;
  const maxAttachments = mode === 'video' ? VIDEO_MAX_ATTACHMENTS : TEXT_MAX_ATTACHMENTS;
  const activeModels = mode === 'video' ? videoModels : mode === 'text' ? textModels : imageModels;
  const activeModelCode = mode === 'video' ? videoModel : mode === 'text' ? textModel : imageModel;
  const activeModelLabel = activeModels.find((m) => m.code === activeModelCode)?.label ?? activeModelCode;
  const isBusy = !!inProgress || createImage.isPending || createVideo.isPending || createText.isPending;

  const handleTask = (t: GenerationTask) => {
    setTask(t);
    startPolling(t.task_id);
    void refreshMe();
    qc.invalidateQueries({ queryKey: ['gen.history'] });
  };

  const startPolling = (taskId: string) => {
    if (pollRef.current) window.clearInterval(pollRef.current);
    pollRef.current = window.setInterval(async () => {
      try {
        const fresh = await genApi.getTask(taskId);
        setTask(fresh);
        if ([2, 3, 4].includes(fresh.status)) {
          if (pollRef.current) window.clearInterval(pollRef.current);
          pollRef.current = null;
          if (fresh.status === 2) toast.success('生成完成');
          else if (fresh.status === 3) toast.error(fresh.error || '生成失败');
          else toast.info('已退款');
          await refreshMe();
          qc.invalidateQueries({ queryKey: ['gen.history'] });
        }
      } catch {
        // keep polling quietly
      }
    }, mode === 'video' ? 2000 : 1500);
  };

  const submit = () => {
    if (!prompt.trim()) {
      toast.info('先描述你想创作的内容');
      return;
    }
    ensureLoggedIn(() => {
      if (mode === 'text') createText.mutate();
      else if (mode === 'video') createVideo.mutate();
      else createImage.mutate();
    }, '登录后即可开始创作');
  };

  const fillPromptFromCard = useCallback((item: PromptGalleryCard) => {
    setPrompt(item.prompt);
    window.requestAnimationFrame(() => promptRef.current?.focus());
  }, []);

  const readFileAsDataURL = (file: File) => new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ''));
    reader.onerror = () => reject(reader.error || new Error('read file failed'));
    reader.readAsDataURL(file);
  });

  const handleAttachFiles = async (files: FileList | null) => {
    if (!files?.length) return;
    const imageFiles = Array.from(files).filter((file) => file.type.startsWith('image/'));
    if (!imageFiles.length) {
      toast.info('请选择图片文件');
      return;
    }
    const slots = Math.max(0, maxAttachments - attachments.length);
    if (slots <= 0) {
      toast.info(`最多上传 ${maxAttachments} 张参考图`);
      return;
    }
    const picked = imageFiles.slice(0, slots);
    try {
      const data = await Promise.all(picked.map(async (file) => ({
        id: `${file.name}-${file.size}-${file.lastModified}`,
        name: file.name,
        dataUrl: await readFileAsDataURL(file),
      })));
      setAttachments((prev) => [...prev, ...data]);
      if (imageFiles.length > slots) toast.info(`已保留前 ${maxAttachments} 张参考图`);
    } catch {
      toast.error('读取图片失败');
    } finally {
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  };

  return (
    <div className={clsx('dapo-studio relative min-h-screen overflow-x-hidden bg-black text-white', compactMotion && 'dapo-studio--compact-motion')}>
      <div className="dapo-galaxy-layer" aria-hidden="true">
        {motionProfile.supportsRichEffects ? (
          <Suspense fallback={null}>
            <Galaxy
              mouseRepulsion
              mouseInteraction
              density={1}
              glowIntensity={0.4}
              saturation={0.2}
              hueShift={30}
              twinkleIntensity={0.3}
              rotationSpeed={0.1}
              repulsionStrength={2}
              autoCenterRepulsion={0}
              starSpeed={0.5}
              speed={1}
              frameRate={60}
              dpr={1}
            />
          </Suspense>
        ) : (
          <LightweightGalaxy />
        )}
      </div>

      <div className="relative z-10 mx-auto w-full max-w-[1500px] px-4 pb-14 pt-5 sm:px-6 lg:px-10">
        <section className="grid gap-4">
          <DevelopmentStage
            mode={mode}
            task={task}
            textResult={textResult}
            activeModelLabel={activeModelLabel}
            attachmentsCount={attachments.length}
            maxAttachments={maxAttachments}
            expectedCost={expectedCost}
            inProgress={!!inProgress}
            supportsRichEffects={motionProfile.supportsRichEffects}
            onOpen={setPreview}
          />

          <div className="grid gap-4">
            <BorderGlow
              className="dapo-composer-glow"
              edgeSensitivity={30}
              glowColor="40 80 80"
              backgroundColor="#120F17"
              borderRadius={28}
              glowRadius={40}
              glowIntensity={1}
              coneSpread={25}
              animated={false}
              colors={['#c084fc', '#f472b6', '#38bdf8']}
            >
              <div className="dapo-composer-card p-4 text-white sm:p-5">
              <div className="mb-5 flex flex-col gap-2">
                <PromptGalleryRail cards={promptGalleryCards} compactMotion={compactMotion} supportsRichEffects={motionProfile.supportsRichEffects} onPick={fillPromptFromCard} />
              </div>

              <div className="dapo-prompt-panel rounded-[8px] border border-[#d7dde5] bg-[#fbfcfd] p-3 sm:p-4">
              <textarea
                ref={promptRef}
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder={promptPlaceholder(mode)}
                className="studio-prompt min-h-[128px] w-full resize-none border-0 bg-transparent px-1 pt-1 text-[16px] leading-8 text-[#101318] outline-none ring-0 placeholder:text-[#98a2b3] focus:border-0 focus:outline-none focus:ring-0"
                maxLength={5000}
              />

              {attachments.length > 0 && (
                <div className="mt-3 flex flex-wrap gap-2 border-t border-[#e6e9ee] pt-3">
                  {attachments.map((item) => (
                    <div key={item.id} className="group relative h-16 w-16 overflow-hidden rounded-[8px] bg-[#eef1f5]">
                      <img src={item.dataUrl} alt={item.name} className="h-full w-full object-cover" />
                      <button
                        type="button"
                        onClick={() => setAttachments((prev) => prev.filter((x) => x.id !== item.id))}
                        className="absolute right-1 top-1 grid h-5 w-5 place-items-center rounded-full bg-black/65 text-white opacity-0 transition group-hover:opacity-100"
                        title="移除"
                      >
                        <X size={12} />
                      </button>
                    </div>
                  ))}
                </div>
              )}

              <div className="dapo-composer-controls mt-4 flex flex-col gap-3 border-t border-[#e6e9ee] pt-4 xl:flex-row xl:items-center xl:justify-between">
                <div className="dapo-composer-options flex min-w-0 flex-wrap items-center gap-2">
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept="image/*"
                    multiple
                    className="hidden"
                    onChange={(e) => void handleAttachFiles(e.target.files)}
                  />
                  <button
                    className="inline-flex h-9 items-center gap-2 rounded-[8px] border border-[#d7dde5] bg-white px-3 text-[13px] text-[#475467] transition hover:border-[#b8c0cc] hover:text-[#101318]"
                    title="上传参考图"
                    type="button"
                    onClick={() => fileInputRef.current?.click()}
                  >
                    <Paperclip size={16} />
                    参考图
                  </button>
                  <ComposerSelect
                    value={activeModelCode}
                    onChange={(v) => mode === 'video' ? setVideoModel(v) : mode === 'text' ? setTextModel(v) : setImageModel(v)}
                    options={activeModels.map((m) => ({ value: m.code, label: m.label }))}
                    wide
                  />
                  {mode === 'image' && (
                    <>
                      <ComposerSelect value={imageRatio} onChange={(v) => setImageRatio(v as typeof IMAGE_RATIOS[number])} options={IMAGE_RATIOS.map((r) => ({ value: r, label: r }))} />
                      <ComposerSelect value={imageResolution} onChange={(v) => setImageResolution(v as typeof IMAGE_RESOLUTIONS[number])} options={IMAGE_RESOLUTIONS.map((r) => ({ value: r, label: r }))} />
                      <ComposerSelect value={String(count)} onChange={(v) => setCount(Number(v))} options={[1, 2, 4].map((n) => ({ value: String(n), label: `${n}张` }))} />
                    </>
                  )}
                  {mode === 'video' && (
                    <>
                      <ComposerSelect value={videoRatio} onChange={(v) => setVideoRatio(v as typeof VIDEO_RATIOS[number])} options={VIDEO_RATIOS.map((r) => ({ value: r, label: r }))} />
                      <ComposerSelect value={String(duration)} onChange={(v) => setDuration(Number(v) as typeof VIDEO_DURATIONS[number])} options={VIDEO_DURATIONS.map((n) => ({ value: String(n), label: `${n}s` }))} />
                    </>
                  )}
                </div>
                <div className="flex shrink-0 items-center justify-between gap-2 sm:justify-end">
                  <span className="hidden h-10 items-center rounded-[8px] border border-[#d7dde5] bg-white px-3 text-[13px] text-[#475467] sm:inline-flex">
                    预计 {typeof expectedCost === 'number' ? `${expectedCost} 点` : expectedCost}
                  </span>
                  <button className="grid h-10 w-10 place-items-center rounded-[8px] border border-[#d7dde5] bg-white text-[#667085] transition hover:text-[#101318]" title="语音输入" type="button">
                    <Mic size={17} />
                  </button>
                  <button
                    type="button"
                    onClick={submit}
                    disabled={isBusy}
                    className="inline-flex h-11 min-w-[124px] items-center justify-center gap-2 rounded-[8px] bg-[#101318] px-4 text-[14px] text-white transition hover:bg-[#2a2f38] disabled:cursor-not-allowed disabled:bg-[#c8ced7]"
                    title={modeActionLabel(mode)}
                  >
                    {isBusy ? <Loader2 size={18} className="animate-spin" /> : <ArrowUp size={18} />}
                    {modeActionLabel(mode)}
                  </button>
                </div>
              </div>
              </div>
              </div>
            </BorderGlow>
          </div>
        </section>

        <section className="mt-10">
          <div className="mb-4 flex items-center justify-between gap-3">
            <div>
              <h2 className="dapo-section-title text-[22px] text-white">我的作品</h2>
              <p className="mt-1 text-[14px] text-white/58">生成后的图片和视频会在这里沉淀。</p>
            </div>
            <div className="flex items-center gap-2">
              <ComposerSelect
                value={String(historyPageSize)}
                onChange={(v) => setHistoryPageSize(Number(v) as typeof HISTORY_PAGE_SIZES[number])}
                options={HISTORY_PAGE_SIZES.map((n) => ({ value: String(n), label: `${n}个` }))}
              />
              <HistoryActionMenu
                disabled={!token || deleteHistory.isPending}
                onDeleteBefore3Days={() => {
                  if (window.confirm('确定删除3天前的作品记录吗？')) {
                    deleteHistory.mutate('before_3d');
                  }
                }}
                onDeleteBefore7Days={() => {
                  if (window.confirm('确定删除7天前的作品记录吗？')) {
                    deleteHistory.mutate('before_7d');
                  }
                }}
                onDeleteAll={() => {
                  if (window.confirm('确定删除全部作品记录吗？已完成、失败和退款记录都会从首页移除。')) {
                    deleteHistory.mutate('all');
                  }
                }}
              />
            </div>
          </div>
          {resultItems.length === 0 ? (
            <div className="grid min-h-[220px] place-items-center rounded-[8px] border border-dashed border-white/16 bg-white/8 text-white/62 backdrop-blur">
              <div className="flex flex-col items-center gap-2 text-center">
                <FileImage size={28} />
                <p className="text-[14px]">{token ? '还没有作品，先生成一张图片吧' : '登录后会在这里显示你的作品'}</p>
              </div>
            </div>
          ) : (
            <div
              className="columns-1 gap-3 sm:columns-2 lg:columns-3 xl:columns-4 2xl:columns-5"
              style={{ columnWidth: '220px' }}
            >
              {resultItems.map((item) => <WorkCard key={item.task_id} item={item} onOpen={setPreview} />)}
            </div>
          )}
        </section>
        {preview && <PreviewLightbox preview={preview} onClose={() => setPreview(null)} />}
      </div>
    </div>
  );
}

function DevelopmentStage({
  mode,
  task,
  textResult,
  activeModelLabel,
  attachmentsCount,
  maxAttachments,
  expectedCost,
  inProgress,
  supportsRichEffects,
  onOpen,
}: {
  mode: StudioMode;
  task: GenerationTask | null;
  textResult: string;
  activeModelLabel: string;
  attachmentsCount: number;
  maxAttachments: number;
  expectedCost: string | number;
  inProgress: boolean;
  supportsRichEffects: boolean;
  onOpen: (preview: { url: string; type: 'image' | 'video'; title: string }) => void;
}) {
  const activeItem = task;
  const media = activeItem?.results?.[0];
  const isVideo = activeItem?.kind === 'video';
  const canOpen = activeItem?.status === 2 && !!media?.url;

  return (
    <section className="dapo-development-stage relative text-white">
      <div className="relative z-10">
        <div className="dapo-stage-frame relative flex">
          {mode === 'text' && textResult ? (
            <div className="dapo-result-shell">
              <div className="max-h-[420px] w-full overflow-auto rounded-[8px] border border-white/12 bg-black/54 p-5 text-[14px] leading-7 text-white/76 backdrop-blur sm:p-6">
                <div className="mb-3 text-[12px] text-white/46">{textResult.length} 字</div>
                <div className="whitespace-pre-wrap">{textResult}</div>
              </div>
              <ParameterStrip
                mode={mode}
                activeModelLabel={activeModelLabel}
                attachmentsCount={attachmentsCount}
                maxAttachments={maxAttachments}
                expectedCost={expectedCost}
                className="dapo-result-meta"
              />
            </div>
          ) : media?.url ? (
            <div className="dapo-result-shell">
              <button
                type="button"
                disabled={!canOpen}
                onClick={() => canOpen && onOpen({ url: media.url, type: isVideo ? 'video' : 'image', title: activeItem?.model ?? activeModelLabel })}
                className={clsx('group relative w-full overflow-hidden rounded-[8px] bg-[#101318]', canOpen && 'cursor-zoom-in')}
              >
                {isVideo ? (
                  media.thumb_url ? (
                    <img src={media.thumb_url} alt="" className="h-full min-h-[390px] w-full object-contain" />
                  ) : (
                    <video src={media.url} className="h-full min-h-[390px] w-full object-contain" muted playsInline preload="metadata" />
                  )
                ) : (
                  <img src={media.url} alt="" className="h-full min-h-[390px] w-full object-contain" />
                )}
                <div className="absolute inset-0 bg-black/0 transition group-hover:bg-black/16" />
                {canOpen && (
                  <span className="absolute right-3 top-3 grid h-10 w-10 place-items-center rounded-[8px] bg-white/92 text-[#101318] opacity-0 shadow-sm transition group-hover:opacity-100">
                    {isVideo ? <Play size={18} fill="currentColor" /> : <Maximize2 size={18} />}
                  </span>
                )}
              </button>
              <ParameterStrip
                mode={mode}
                activeModelLabel={activeModelLabel}
                attachmentsCount={attachmentsCount}
                maxAttachments={maxAttachments}
                expectedCost={expectedCost}
                className="dapo-result-meta"
              />
            </div>
          ) : inProgress ? (
            <div className="relative min-h-[390px] w-full">
              <GeneratingDots />
            </div>
          ) : supportsRichEffects ? (
            <div className="dapo-ascii-stage">
              <Suspense fallback={<MobileStageTitle />}>
                <ASCIIText
                  text="让每一个灵感显影"
                  enableWaves
                  asciiFontSize={6}
                  textFontSize={180}
                  textColor="#ffffff"
                  strokeColor="#7dd3fc"
                  strokeWidth={8}
                  planeBaseHeight={7.2}
                  frameRate={45}
                />
              </Suspense>
            </div>
          ) : (
            <MobileStageTitle />
          )}
        </div>
      </div>
    </section>
  );
}

function MobileStageTitle() {
  return (
    <div className="dapo-mobile-title-stage" aria-label="让每一个灵感显影">
      <span className="dapo-mobile-title-main" aria-hidden="true">
        <span>让每一个</span>
        <span>灵感显影</span>
      </span>
    </div>
  );
}

function ParameterStrip({
  mode,
  activeModelLabel,
  attachmentsCount,
  maxAttachments,
  expectedCost,
  className,
}: {
  mode: StudioMode;
  activeModelLabel: string;
  attachmentsCount: number;
  maxAttachments: number;
  expectedCost: string | number;
  className?: string;
}) {
  const costLabel = typeof expectedCost === 'number' ? `${expectedCost} 点` : expectedCost;
  const items: Array<[string, string]> = [
    ['入口', modeTitle(mode)],
    ['模型', activeModelLabel],
    ['参考素材', `${attachmentsCount}/${maxAttachments}`],
    ['预计消耗', costLabel],
  ];

  return (
    <div className={clsx('dapo-param-strip', className)}>
      {items.map(([label, value]) => (
        <div key={label} className="dapo-param-strip__item">
          <span>{label}</span>
          <strong>{value}</strong>
        </div>
      ))}
    </div>
  );
}

function PromptGalleryRail({ cards, compactMotion, supportsRichEffects, onPick }: { cards: PromptGalleryCard[]; compactMotion: boolean; supportsRichEffects: boolean; onPick: (card: PromptGalleryCard) => void }) {
  const items = useMemo(
    () =>
      cards.map((card, index) => ({
        id: String(card.id ?? card.title),
        title: card.title,
        subtitle: card.subtitle,
        image: card.image,
        fallbackImage: promptGalleryFallbackImage(index),
        prompt: card.prompt,
      })),
    [cards],
  );
  const handleSelect = useCallback((item: { title: string; subtitle?: string; image: string; prompt?: string }) => {
    if (!item.prompt) return;
    onPick({
      title: item.title,
      subtitle: item.subtitle,
      image: item.image,
      prompt: item.prompt,
    });
  }, [onPick]);

  return (
    <div className="mt-4">
      <div className="mb-2 flex items-center justify-between gap-3">
        <p className="text-[13px] text-white/68">快捷提示词</p>
        <p className="hidden text-[12px] text-white/42 sm:block">横向滑动查看</p>
      </div>
      {supportsRichEffects ? (
        <Suspense fallback={<LitePromptGallery items={items} onSelect={handleSelect} />}>
          <CircularGallery
            items={items}
            bend={-1}
            borderRadius={0.11}
            scrollSpeed={2.3}
            scrollEase={0.04}
            textColor="#ffffff"
            font={compactMotion ? 'bold 22px Figtree' : 'bold 30px Figtree'}
            frameRate={60}
            dpr={1.5}
            antialias
            widthSegments={100}
            heightSegments={50}
            onSelect={handleSelect}
          />
        </Suspense>
      ) : (
        <LitePromptGallery items={items} onSelect={handleSelect} />
      )}
    </div>
  );
}

function useStudioMotionProfile() {
  type Profile = {
    isMobile: boolean;
    prefersReducedMotion: boolean;
    isConstrained: boolean;
    isWebKitRisk: boolean;
    supportsRichEffects: boolean;
  };

  const getProfile = () => {
    if (typeof window === 'undefined') {
      return { isMobile: false, prefersReducedMotion: false, isConstrained: false, isWebKitRisk: false, supportsRichEffects: true };
    }
    const width = window.innerWidth || 1440;
    const coarse = window.matchMedia('(pointer: coarse)').matches;
    const small = window.matchMedia('(max-width: 720px)').matches;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const cores = navigator.hardwareConcurrency || 8;
    const ua = navigator.userAgent;
    const isIOS = /iPad|iPhone|iPod/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
    const isSafari = /^((?!chrome|android|crios|fxios|edg|opr).)*safari/i.test(ua);
    const isWechat = /MicroMessenger/i.test(ua);
    const profile: Profile = {
      isMobile: small || (coarse && width <= 900),
      prefersReducedMotion: reduced,
      isConstrained: cores <= 4 && width <= 1100,
      isWebKitRisk: isIOS || isSafari || isWechat,
      supportsRichEffects: false,
    };
    profile.supportsRichEffects = !profile.isMobile && !profile.prefersReducedMotion && !profile.isConstrained && !profile.isWebKitRisk;
    return profile;
  };

  const [profile, setProfile] = useState(getProfile);

  useEffect(() => {
    const mediaQueries = [
      window.matchMedia('(pointer: coarse)'),
      window.matchMedia('(max-width: 720px)'),
      window.matchMedia('(prefers-reduced-motion: reduce)'),
    ];
    const update = () => setProfile(getProfile());
    mediaQueries.forEach((mq) => mq.addEventListener('change', update));
    window.addEventListener('resize', update);
    return () => {
      mediaQueries.forEach((mq) => mq.removeEventListener('change', update));
      window.removeEventListener('resize', update);
    };
  }, []);

  return profile;
}

function LightweightGalaxy() {
  return (
    <div className="dapo-lite-galaxy" aria-hidden="true">
      <span className="dapo-lite-star dapo-lite-star--a" />
      <span className="dapo-lite-star dapo-lite-star--b" />
      <span className="dapo-lite-star dapo-lite-star--c" />
      <span className="dapo-lite-star dapo-lite-star--d" />
      <span className="dapo-lite-star dapo-lite-star--e" />
    </div>
  );
}

function LitePromptGallery({
  items,
  onSelect,
}: {
  items: Array<{ id: string; title: string; subtitle?: string; image: string; fallbackImage?: string; prompt?: string }>;
  onSelect: (item: { title: string; subtitle?: string; image: string; prompt?: string }) => void;
}) {
  return (
    <div className="dapo-lite-gallery" aria-label="快捷提示词">
      {items.map((item, index) => (
        <button
          key={`${item.id}-${index}`}
          type="button"
          className="dapo-lite-gallery__card"
          onClick={() => onSelect(item)}
        >
          <span className="dapo-lite-gallery__image">
            <img src={item.image} alt="" loading={index < 3 ? 'eager' : 'lazy'} onError={(e) => { e.currentTarget.src = item.fallbackImage ?? promptGalleryFallbackImage(index); }} />
          </span>
          <span className="dapo-lite-gallery__title">{item.title}</span>
          {item.subtitle && <span className="dapo-lite-gallery__subtitle">{item.subtitle}</span>}
        </button>
      ))}
    </div>
  );
}

function ComposerSelect({ value, options, onChange, disabled, wide }: { value: string; options: { value: string; label: string }[]; onChange: (value: string) => void; disabled?: boolean; wide?: boolean }) {
  const [open, setOpen] = useState(false);
  const current = options.find((o) => o.value === value) ?? options[0];

  return (
    <div
      className={clsx('composer-select relative min-w-0', wide && 'composer-select--wide')}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false);
      }}
    >
      <button
        type="button"
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
        className={clsx(
          'inline-flex h-9 w-full max-w-full items-center gap-1.5 rounded-[8px] border border-[#d7dde5] bg-white px-3 text-[13px] text-[#2156d9] outline-none transition',
          wide && 'min-w-[150px] justify-between',
          open ? 'border-[#a9b8f4] bg-[#f6f8ff]' : 'hover:border-[#b8c0cc]',
          disabled && 'cursor-not-allowed text-[#98a2b3] hover:border-[#d7dde5]',
        )}
      >
        <span>{current?.label}</span>
        <ChevronDown size={15} className={clsx('transition', open && 'rotate-180')} />
      </button>

      {open && !disabled && (
        <div className={clsx('absolute left-0 top-11 z-30 overflow-hidden rounded-[8px] border border-[#dfe3e8] bg-white p-1.5 shadow-[0_18px_50px_rgba(15,23,42,.14)]', wide ? 'min-w-[210px]' : 'min-w-[132px]')}>
          {options.map((o) => {
            const selected = o.value === value;
            return (
              <button
                key={o.value}
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => {
                  onChange(o.value);
                  setOpen(false);
                }}
                className={clsx(
                  'flex h-10 w-full items-center justify-between gap-3 rounded-[7px] px-3 text-left text-[13px] transition',
                  selected ? 'bg-[#f1f4f8] text-[#101318]' : 'text-[#667085] hover:bg-[#f6f7f8] hover:text-[#101318]',
                )}
              >
                <span className="min-w-0 truncate">{o.label}</span>
                {selected && <Check size={16} />}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

function HistoryActionMenu({
  disabled,
  onDeleteBefore3Days,
  onDeleteBefore7Days,
  onDeleteAll,
}: {
  disabled?: boolean;
  onDeleteBefore3Days: () => void;
  onDeleteBefore7Days: () => void;
  onDeleteAll: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div
      className="relative"
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false);
      }}
    >
      <button
        type="button"
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
        className="inline-flex h-9 items-center gap-1.5 rounded-[8px] border border-[#d7dde5] bg-white px-3 text-[13px] text-[#667085] outline-none transition hover:text-[#101318] disabled:cursor-not-allowed disabled:text-[#c8ced7]"
      >
        <MoreHorizontal size={16} />
        管理
      </button>
      {open && !disabled && (
        <div className="absolute right-0 top-11 z-30 min-w-[150px] overflow-hidden rounded-[8px] border border-[#dfe3e8] bg-white p-1.5 shadow-[0_18px_50px_rgba(15,23,42,.14)]">
          <button
            type="button"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              setOpen(false);
              onDeleteBefore3Days();
            }}
            className="flex h-10 w-full items-center gap-2 rounded-[7px] px-3 text-left text-[13px] text-[#475467] transition hover:bg-[#f6f7f8]"
          >
            <Trash2 size={15} />
            删除3天前
          </button>
          <button
            type="button"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              setOpen(false);
              onDeleteBefore7Days();
            }}
            className="flex h-10 w-full items-center gap-2 rounded-[7px] px-3 text-left text-[13px] text-[#475467] transition hover:bg-[#f6f7f8]"
          >
            <Trash2 size={15} />
            删除7天前
          </button>
          <button
            type="button"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              setOpen(false);
              onDeleteAll();
            }}
            className="flex h-10 w-full items-center gap-2 rounded-[7px] px-3 text-left text-[13px] text-red-600 transition hover:bg-red-50"
          >
            <Trash2 size={15} />
            删除全部
          </button>
        </div>
      )}
    </div>
  );
}

function WorkCard({ item, onOpen }: { item: GenerationTask; onOpen: (preview: { url: string; type: 'image' | 'video'; title: string }) => void }) {
  const result = item.results?.[0];
  const thumb = result?.thumb_url;
  const original = result?.url;
  const [thumbFailed, setThumbFailed] = useState(false);
  const [loadedRatio, setLoadedRatio] = useState<string | null>(null);
  const isVideo = item.kind === 'video';
  const showThumb = !!thumb && !thumbFailed;
  const declaredRatio = result?.width && result?.height ? `${result.width} / ${result.height}` : '';
  const mediaRatio = loadedRatio || declaredRatio || (isVideo ? '16 / 9' : '1 / 1');
  const canOpen = item.status === 2 && !!original;
  const prompt = compactPrompt(item.prompt);
  const setRatioFromImage = (el: HTMLImageElement) => {
    if (el.naturalWidth > 0 && el.naturalHeight > 0) {
      setLoadedRatio(`${el.naturalWidth} / ${el.naturalHeight}`);
    }
  };
  const setRatioFromVideo = (el: HTMLVideoElement) => {
    if (el.videoWidth > 0 && el.videoHeight > 0) {
      setLoadedRatio(`${el.videoWidth} / ${el.videoHeight}`);
    }
  };

  return (
    <article className="mb-3 break-inside-avoid overflow-hidden rounded-[8px] border border-[#dfe3e8] bg-white shadow-sm">
      <button
        type="button"
        disabled={!canOpen}
        onClick={() => original && onOpen({ url: original, type: isVideo ? 'video' : 'image', title: item.model })}
        style={{ aspectRatio: mediaRatio }}
        className={clsx(
          'relative grid w-full place-items-center overflow-hidden bg-[#eef1f5] text-[#667085] transition-[height]',
          !original && item.status === 1 && 'bg-white',
          canOpen && 'group cursor-zoom-in',
        )}
      >
        {original ? (
          isVideo ? (
            showThumb ? (
              <img
                src={thumb}
                alt=""
                className="h-full w-full object-cover"
                loading="lazy"
                onLoad={(e) => setRatioFromImage(e.currentTarget)}
                onError={() => setThumbFailed(true)}
              />
            ) : (
              <video
                src={original}
                className="h-full w-full object-cover"
                muted
                playsInline
                preload="metadata"
                onLoadedMetadata={(e) => setRatioFromVideo(e.currentTarget)}
              />
            )
          ) : (
            <img src={original} alt="" className="h-full w-full object-cover" loading="lazy" onLoad={(e) => setRatioFromImage(e.currentTarget)} />
          )
        ) : item.status === 1 ? (
          <GeneratingDots />
        ) : (
          <div className="flex flex-col items-center gap-2 text-sm">
            <FileImage size={24} />
            <span>{statusText(item.status)}</span>
          </div>
        )}
        <div className="absolute left-2 top-2 rounded-[7px] bg-black/58 px-2 py-0.5 text-[11px] text-white">{item.kind === 'video' ? '\u89c6\u9891' : '\u56fe\u7247'}</div>
        {canOpen && (
          <div className="absolute inset-0 grid place-items-center bg-black/0 opacity-0 transition group-hover:bg-black/20 group-hover:opacity-100">
            <span className="grid h-10 w-10 place-items-center rounded-full bg-white/90 text-neutral-950 shadow-sm">
              {isVideo ? <Play size={18} fill="currentColor" /> : <Maximize2 size={18} />}
            </span>
          </div>
        )}
      </button>
      <div className="flex items-center gap-1.5 px-2.5 py-2 text-[12px] text-[#667085]">
        <span className="shrink-0">{fmtRelative(item.created_at)}</span>
        {prompt && <span className="truncate text-[#475467]">{prompt}</span>}
      </div>
    </article>
  );
}

function compactPrompt(prompt?: string) {
  const text = String(prompt || '').replace(/\s+/g, ' ').trim();
  if (!text) return '';
  return text.length > 28 ? text.slice(0, 28) + '...' : text;
}

function GeneratingDots() {
  const [phraseIndex, setPhraseIndex] = useState(0);

  useEffect(() => {
    const timer = window.setInterval(() => {
      setPhraseIndex((idx) => (idx + 1) % GENERATING_PHRASES.length);
    }, 1800);
    return () => window.clearInterval(timer);
  }, []);

  return (
    <div className="generating-dots" aria-label="正在为您设计中">
      <div className="generating-dots__phrases">
        <span className="generating-dots__phrase generating-dots__phrase--active" key={phraseIndex}>
          {GENERATING_PHRASES[phraseIndex]}
        </span>
      </div>
    </div>
  );
}

function PreviewLightbox({ preview, onClose }: { preview: { url: string; type: 'image' | 'video'; title: string }; onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4" onMouseDown={onClose}>
      <div className="relative max-h-[92vh] max-w-[92vw]" onMouseDown={(e) => e.stopPropagation()}>
        <button
          type="button"
          onClick={onClose}
          className="absolute right-3 top-3 z-10 grid h-9 w-9 place-items-center rounded-full bg-white/90 text-neutral-900 shadow-sm transition hover:bg-white"
          title="关闭"
        >
          <X size={18} />
        </button>
        {preview.type === 'video' ? (
          <video src={preview.url} controls autoPlay className="max-h-[92vh] max-w-[92vw] rounded-[12px] bg-black shadow-2xl" />
        ) : (
          <img src={preview.url} alt={preview.title} className="max-h-[92vh] max-w-[92vw] rounded-[12px] object-contain shadow-2xl" />
        )}
      </div>
    </div>
  );
}

function modeFromPath(pathname: string): StudioMode {
  if (pathname.includes('/create/video')) return 'video';
  if (pathname.includes('/create/text')) return 'text';
  return 'image';
}

function modeTitle(mode: StudioMode) {
  if (mode === 'video') return '视频';
  if (mode === 'text') return '文字';
  return '图片';
}

function modeActionLabel(mode: StudioMode) {
  if (mode === 'video') return '生成视频';
  if (mode === 'text') return '生成文字';
  return '生成图片';
}

function promptPlaceholder(mode: StudioMode) {
  if (mode === 'video') return '描述镜头、主体、运动方式、时长感和画面比例。例如：一支高级产品揭幕短片，慢速推近，柔和棚拍灯光，背景干净...';
  if (mode === 'text') return '写下你要生成的文字任务。例如：为一个新消费品牌写 5 组克制、有记忆点的产品卖点文案...';
  return '描述你想生成的画面。例如：一张极简产品广告海报，白色展台，柔和棚拍光，克制排版，高级商业摄影质感...';
}

function statusText(status: number) {
  if (status === 2) return '已完成';
  if (status === 3) return '失败';
  if (status === 4) return '已退款';
  if (status === 1) return '生成中';
  return '排队中';
}

function promptGalleryItemToCard(item: PromptGalleryItem): PromptGalleryCard {
  return {
    id: item.id,
    title: item.title,
    subtitle: item.subtitle,
    image: item.cover_url,
    prompt: item.prompt,
  };
}

function promptGalleryFallbackImage(index: number) {
  const fallback = FALLBACK_PROMPT_GALLERY.image[index % FALLBACK_PROMPT_GALLERY.image.length];
  return fallback?.image ?? '/examples/case-1.jpg';
}

function modelsByKind(models: PublicModel[] | undefined, kind: PublicModel['kind'], fallback: SelectModel[]): SelectModel[] {
  const rows = (models ?? [])
    .filter((m) => m.enabled !== false && m.kind === kind && m.model_code)
    .map((m) => ({
      code: m.model_code,
      label: m.name || m.model_code,
      cost: typeof m.unit_points === 'number' ? m.unit_points / 100 : undefined,
      input: typeof m.input_unit_points === 'number' ? m.input_unit_points / 100 : undefined,
      output: typeof m.output_unit_points === 'number' ? m.output_unit_points / 100 : undefined,
    }));
  return rows.length ? rows : fallback;
}

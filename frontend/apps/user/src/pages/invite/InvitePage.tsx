import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import QRCode from 'qrcode';
import { Copy, Download, ImageDown, Link, Share2, Users } from 'lucide-react';

import { fmtPoints } from '../../lib/format';
import { billingApi } from '../../lib/services';
import type { InviteRules } from '../../lib/types';
import { useAuthStore } from '../../stores/auth';
import { toast } from '../../stores/toast';

export default function InvitePage() {
  const me = useAuthStore((s) => s.me);
  const code = (me?.invite_code || '').trim().toUpperCase();
  const hasCode = code.length > 0;
  const [qrDataUrl, setQrDataUrl] = useState('');
  const [posterUrl, setPosterUrl] = useState('');

  const rulesQ = useQuery({
    queryKey: ['billing.invite.rules'],
    queryFn: billingApi.inviteRules,
  });

  const link = useMemo(() => {
    const origin = typeof window !== 'undefined' ? window.location.origin : 'https://www.dapo-ai.com';
    if (!hasCode) return `${origin}/register`;
    return `${origin}/register?invite=${encodeURIComponent(code)}`;
  }, [code, hasCode]);

  const rewardLines = useMemo(() => rewardCopy(rulesQ.data), [rulesQ.data]);

  useEffect(() => {
    let cancelled = false;
    setQrDataUrl('');
    if (!hasCode) return;
    QRCode.toDataURL(link, {
      errorCorrectionLevel: 'M',
      margin: 1,
      width: 360,
      color: { dark: '#121212', light: '#ffffff' },
    }).then((url) => {
      if (!cancelled) setQrDataUrl(url);
    }).catch(() => {
      if (!cancelled) setQrDataUrl('');
    });
    return () => {
      cancelled = true;
    };
  }, [hasCode, link]);

  useEffect(() => {
    let cancelled = false;
    setPosterUrl('');
    if (!hasCode || !qrDataUrl) return;
    drawPoster({ code, link, qrDataUrl, rewardLines }).then((url) => {
      if (!cancelled) setPosterUrl(url);
    }).catch(() => {
      if (!cancelled) setPosterUrl('');
    });
    return () => {
      cancelled = true;
    };
  }, [code, hasCode, link, qrDataUrl, rewardLines]);

  const copy = async (text: string, label: string) => {
    if (!text || !hasCode) return;
    await navigator.clipboard.writeText(text);
    toast.success(`${label}已复制`);
  };

  const downloadPoster = () => {
    if (!posterUrl) {
      toast.error('海报生成中，请稍后再试');
      return;
    }
    const a = document.createElement('a');
    a.href = posterUrl;
    a.download = `dapo-invite-${code}.png`;
    a.click();
  };

  return (
    <div className="page">
      <header className="page-header">
        <div>
          <h1 className="page-title">邀请中心</h1>
          <p className="page-subtitle">复制链接或下载邀请海报，好友注册后会自动绑定到你的邀请码。</p>
        </div>
      </header>

      <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
        <div className="card-tinted card-section space-y-5">
          <div>
            <p className="text-overline mb-1">你的专属邀请码</p>
            <p className="font-mono text-display gradient-text break-all leading-tight">{hasCode ? code : '—'}</p>
          </div>

          <div>
            <p className="text-overline mb-1">邀请链接</p>
            <code className="block rounded-md bg-surface-1 border border-border px-4 py-2.5 font-mono text-small break-all">
              {link}
            </code>
          </div>

          <div className="grid gap-2 sm:grid-cols-3">
            <button className="btn btn-primary btn-lg" disabled={!hasCode} onClick={() => copy(code, '邀请码')}>
              <Copy size={16} /> 复制邀请码
            </button>
            <button className="btn btn-outline btn-lg" disabled={!hasCode} onClick={() => copy(link, '邀请链接')}>
              <Share2 size={16} /> 复制链接
            </button>
            <button className="btn btn-outline btn-lg" disabled={!posterUrl} onClick={downloadPoster}>
              <Download size={16} /> 下载海报
            </button>
          </div>

          <div className="grid gap-3 md:grid-cols-3">
            {rewardLines.map((item) => (
              <div key={item.title} className="rounded-md border border-border bg-surface-1 p-4">
                <p className="text-small text-text-tertiary">{item.title}</p>
                <p className="mt-1 text-[22px] font-semibold text-text-primary">{item.value}</p>
              </div>
            ))}
          </div>
        </div>

        <aside className="card card-section space-y-4">
          <h3 className="section-title">
            <ImageDown size={16} className="text-text-tertiary" />
            邀请海报
          </h3>
          <div className="rounded-lg border border-border bg-surface-2 p-3">
            {posterUrl ? (
              <img className="w-full rounded-md border border-border bg-white" src={posterUrl} alt="DAPO 邀请海报" />
            ) : (
              <div className="aspect-[3/4] grid place-items-center rounded-md bg-surface-3 text-small text-text-tertiary">
                正在生成海报...
              </div>
            )}
          </div>
          <p className="text-small text-text-tertiary leading-relaxed">
            手机端可以长按保存海报；二维码会自动带上你的邀请码。
          </p>
        </aside>
      </section>

      <section className="card card-section mt-4">
        <h3 className="section-title mb-3">
          <Users size={16} className="text-text-tertiary" />
          规则说明
        </h3>
        <div className="grid gap-3 md:grid-cols-3">
          <Rule icon={<Link size={16} />} title="自动绑定" desc="好友通过邀请链接注册，系统会自动填写邀请码并绑定关系。" />
          <Rule icon={<Users size={16} />} title="奖励入账" desc="后台启用邀请奖励后，注册奖励、首充奖励和分润会写入余额明细。" />
          <Rule icon={<Share2 size={16} />} title="合规使用" desc="禁止刷邀请或买卖邀请码，异常邀请关系会被取消奖励。" />
        </div>
      </section>
    </div>
  );
}

function Rule({ icon, title, desc }: { icon: ReactNode; title: string; desc: string }) {
  return (
    <div className="rounded-md border border-border bg-surface-1 p-4">
      <div className="flex items-center gap-2 text-text-primary font-semibold">
        {icon}
        {title}
      </div>
      <p className="mt-2 text-small text-text-tertiary leading-relaxed">{desc}</p>
    </div>
  );
}

function rewardCopy(rules?: InviteRules) {
  if (!rules?.enabled) {
    return [
      { title: '邀请状态', value: '待开启' },
      { title: '关系绑定', value: '已支持' },
      { title: '奖励规则', value: '后台配置' },
    ];
  }
  return [
    { title: '好友注册', value: rules.inviter_register_reward > 0 ? `+${fmtPoints(rules.inviter_register_reward)} 点` : '绑定关系' },
    { title: '好友首充', value: rules.first_recharge_reward > 0 ? `+${fmtPoints(rules.first_recharge_reward)} 点` : '按规则' },
    { title: '长期分润', value: rules.lifetime_share_pct > 0 ? `${rules.lifetime_share_pct}%` : '未开启' },
  ];
}

async function drawPoster(params: { code: string; link: string; qrDataUrl: string; rewardLines: Array<{ title: string; value: string }> }) {
  const canvas = document.createElement('canvas');
  canvas.width = 1080;
  canvas.height = 1440;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('canvas unsupported');

  const gradient = ctx.createLinearGradient(0, 0, 1080, 1440);
  gradient.addColorStop(0, '#09090b');
  gradient.addColorStop(0.52, '#141118');
  gradient.addColorStop(1, '#050505');
  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, 1080, 1440);

  const halo = ctx.createRadialGradient(820, 240, 0, 820, 240, 520);
  halo.addColorStop(0, 'rgba(154, 123, 255, 0.35)');
  halo.addColorStop(0.45, 'rgba(72, 138, 255, 0.16)');
  halo.addColorStop(1, 'rgba(0, 0, 0, 0)');
  ctx.fillStyle = halo;
  ctx.fillRect(0, 0, 1080, 720);

  ctx.strokeStyle = 'rgba(255, 255, 255, 0.18)';
  ctx.lineWidth = 2;
  roundRect(ctx, 72, 72, 936, 1296, 44);
  ctx.stroke();

  ctx.fillStyle = '#ffffff';
  ctx.font = '500 60px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
  ctx.fillText('DAPO 达波显影', 120, 178);

  ctx.fillStyle = 'rgba(255, 255, 255, 0.68)';
  ctx.font = '30px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
  ctx.fillText('全站接入 GPT-IMG2 模型', 120, 232);

  ctx.fillStyle = '#ffffff';
  ctx.font = '700 82px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
  wrapText(ctx, '邀请你一起把灵感显影', 120, 386, 820, 96);

  ctx.fillStyle = 'rgba(255, 255, 255, 0.72)';
  ctx.font = '34px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
  wrapText(ctx, '做海报、商品图、人物分析与创意视觉，让一张提示词变成可分享的作品。', 120, 560, 790, 48);

  const cardY = 730;
  params.rewardLines.forEach((item, idx) => {
    const x = 120 + idx * 280;
    ctx.fillStyle = 'rgba(255,255,255,0.08)';
    roundRect(ctx, x, cardY, 240, 140, 28);
    ctx.fill();
    ctx.fillStyle = 'rgba(255,255,255,0.58)';
    ctx.font = '26px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
    ctx.fillText(item.title, x + 28, cardY + 48);
    ctx.fillStyle = '#ffffff';
    ctx.font = '600 36px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
    ctx.fillText(item.value, x + 28, cardY + 100);
  });

  ctx.fillStyle = '#ffffff';
  roundRect(ctx, 120, 980, 300, 300, 34);
  ctx.fill();
  const qr = await loadImage(params.qrDataUrl);
  ctx.drawImage(qr, 150, 1010, 240, 240);

  ctx.fillStyle = '#ffffff';
  ctx.font = '600 38px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
  ctx.fillText('扫码注册体验 DAPO', 468, 1056);
  ctx.fillStyle = 'rgba(255,255,255,0.7)';
  ctx.font = '28px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
  ctx.fillText('邀请码', 468, 1122);
  ctx.fillStyle = '#ffffff';
  ctx.font = '700 52px ui-monospace, SFMono-Regular, Menlo, monospace';
  ctx.fillText(params.code, 468, 1192);

  ctx.fillStyle = 'rgba(255,255,255,0.46)';
  ctx.font = '24px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
  wrapText(ctx, params.link, 120, 1308, 840, 34);

  return canvas.toDataURL('image/png');
}

function loadImage(src: string) {
  return new Promise<HTMLImageElement>((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = reject;
    img.src = src;
  });
}

function roundRect(ctx: CanvasRenderingContext2D, x: number, y: number, width: number, height: number, radius: number) {
  ctx.beginPath();
  ctx.moveTo(x + radius, y);
  ctx.arcTo(x + width, y, x + width, y + height, radius);
  ctx.arcTo(x + width, y + height, x, y + height, radius);
  ctx.arcTo(x, y + height, x, y, radius);
  ctx.arcTo(x, y, x + width, y, radius);
  ctx.closePath();
}

function wrapText(ctx: CanvasRenderingContext2D, text: string, x: number, y: number, maxWidth: number, lineHeight: number) {
  const chars = Array.from(text);
  let line = '';
  for (const ch of chars) {
    const test = line + ch;
    if (ctx.measureText(test).width > maxWidth && line) {
      ctx.fillText(line, x, y);
      line = ch;
      y += lineHeight;
    } else {
      line = test;
    }
  }
  if (line) ctx.fillText(line, x, y);
}

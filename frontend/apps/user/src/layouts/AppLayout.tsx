import { useEffect, type MouseEvent } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import {
  Clock3,
  CreditCard,
  Image,
  LogIn,
  LogOut,
  MessageCircle,
  Video,
  type LucideIcon,
} from 'lucide-react';
import clsx from 'clsx';

import Magnet from '../components/reactbits/Magnet';
import ShinyText from '../components/reactbits/ShinyText';
import { fmtPoints } from '../lib/format';
import { useAuthStore } from '../stores/auth';
import { useLoginGateStore } from '../stores/loginGate';
import { toast } from '../stores/toast';

interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  authed?: boolean;
}

const NAV_ITEMS: NavItem[] = [
  { to: '/create/image', label: '图片', icon: Image },
  { to: '/create/text', label: '文字', icon: MessageCircle },
  { to: '/create/video', label: '视频', icon: Video },
  { to: '/history', label: '历史', icon: Clock3, authed: true },
  { to: '/billing', label: '充值', icon: CreditCard, authed: true },
];

export function AppLayout() {
  const token = useAuthStore((s) => s.token);
  const me = useAuthStore((s) => s.me);
  const refreshMe = useAuthStore((s) => s.refreshMe);
  const logout = useAuthStore((s) => s.logout);
  const openGate = useLoginGateStore((s) => s.openGate);
  const navigate = useNavigate();
  const isAuthed = !!token;

  useEffect(() => {
    if (token && !me) {
      void refreshMe();
    }
  }, [me, refreshMe, token]);

  const onLogout = async () => {
    await logout();
    toast.info('已退出登录');
    navigate('/create/image', { replace: true });
  };

  const handleNav = (item: NavItem, e: MouseEvent) => {
    if (item.authed && !isAuthed) {
      e.preventDefault();
      openGate({ hint: `登录后即可使用“${item.label}”`, onLoggedIn: () => navigate(item.to) });
    }
  };

  return (
    <div className="min-h-full bg-[#f6f7f8] text-neutral-950">
      <header className="sticky top-0 z-40 border-b border-white/10 bg-black text-white">
        <div className="mx-auto flex h-16 max-w-[1540px] items-center justify-between gap-4 px-4 sm:px-6 lg:px-10">
          <div className="flex min-w-0 shrink items-center gap-2 sm:gap-3">
            <button
              type="button"
              className="flex shrink-0 items-center gap-2 text-left"
              title="DAPO 达波显影"
              onClick={() => navigate('/create/image')}
            >
              <span className="text-[25px] leading-none tracking-[-.01em]">DAPO</span>
              <span className="hidden text-[14px] text-white/76 sm:inline">达波显影</span>
            </button>
            <ShinyText
              text="全站接入GPT-IMG2模型"
              speed={1.2}
              delay={1}
              color="#b5b5b5"
              shineColor="#d2ffb6"
              spread={160}
              direction="left"
              yoyo
              pauseOnHover={false}
              disabled={false}
              className="max-w-[148px] truncate text-[11px] sm:max-w-[180px] sm:text-[12px]"
            />
          </div>

          <nav className="hidden min-w-0 flex-1 items-center justify-center gap-1 md:flex">
            {NAV_ITEMS.slice(0, 3).map((item) => <TopNavLink key={item.to} item={item} onClick={handleNav} />)}
            <div className="mx-1 h-5 w-px bg-white/16" />
            {NAV_ITEMS.slice(3).map((item) => <TopNavLink key={item.to} item={item} onClick={handleNav} />)}
          </nav>

          <div className="flex shrink-0 items-center gap-2">
            {isAuthed && (
              <button
                type="button"
                onClick={() => navigate('/billing')}
                className="hidden h-9 items-center gap-2 rounded-[8px] px-3 text-[13px] text-white/82 transition hover:bg-white/10 sm:inline-flex"
                title="查看积分"
              >
                <CreditCard size={15} />
                {fmtPoints(me?.points)} 积分
              </button>
            )}
            {isAuthed ? (
              <>
                <button
                  type="button"
                  className="grid h-9 w-9 place-items-center rounded-full bg-white text-[13px] text-black"
                  title={me?.username || me?.email || '我的账号'}
                  onClick={() => navigate('/settings')}
                >
                  {(me?.username || me?.email || 'U').slice(0, 1).toUpperCase()}
                </button>
                <button
                  type="button"
                  className="grid h-9 w-9 place-items-center rounded-[8px] text-white/70 transition hover:bg-white/10 hover:text-white"
                  title="退出登录"
                  onClick={onLogout}
                >
                  <LogOut size={17} />
                </button>
              </>
            ) : (
              <button
                type="button"
                className="inline-flex h-9 items-center gap-2 rounded-[8px] bg-white px-3 text-[13px] text-black transition hover:bg-white/88"
                title="登录"
                onClick={() => openGate({ hint: '登录后可保存作品和查看额度' })}
              >
                <LogIn size={16} />
                登录
              </button>
            )}
          </div>
        </div>

        <nav className="dapo-mobile-nav flex gap-1 overflow-x-auto border-t border-white/10 px-3 py-2 md:hidden">
          {NAV_ITEMS.map((item) => <MobileMode key={item.to} item={item} onClick={handleNav} />)}
        </nav>
      </header>

      <main className="min-h-screen">
        <Outlet />
      </main>
    </div>
  );
}

function TopNavLink({ item, onClick }: { item: NavItem; onClick: (item: NavItem, e: MouseEvent) => void }) {
  const Icon = item.icon;
  return (
    <Magnet padding={28} magnetStrength={10}>
      <NavLink
        to={item.to}
        onClick={(e) => onClick(item, e)}
        className={({ isActive }) =>
          clsx(
            'inline-flex h-9 items-center gap-2 rounded-[8px] px-3 text-[13px] transition',
            isActive ? 'bg-white text-black' : 'text-white/70 hover:bg-white/10 hover:text-white',
          )
        }
      >
        <Icon size={15} />
        {item.label}
      </NavLink>
    </Magnet>
  );
}

function MobileMode({ item, onClick }: { item: NavItem; onClick: (item: NavItem, e: MouseEvent) => void }) {
  const Icon = item.icon;
  return (
    <Magnet padding={22} magnetStrength={8} className="shrink-0">
      <NavLink
        to={item.to}
        onClick={(e) => onClick(item, e)}
        className={({ isActive }) =>
          clsx(
            'inline-flex h-9 shrink-0 items-center gap-1.5 rounded-[8px] px-3 text-[13px]',
            isActive ? 'bg-white text-black' : 'text-white/72 hover:bg-white/10 hover:text-white',
          )
        }
      >
        <Icon size={15} />
        {item.label}
      </NavLink>
    </Magnet>
  );
}

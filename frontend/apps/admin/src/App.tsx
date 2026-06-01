import { Suspense, lazy } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { AdminLayout } from './layouts/AdminLayout';
import RequireAuth from './routes/RequireAuth';
import { Toaster } from './components/Toaster';

const LoginPage = lazy(() => import('./pages/auth/LoginPage'));
const DashboardPage = lazy(() => import('./pages/dashboard/DashboardPage'));
const TokenAccountsPage = lazy(() => import('./pages/accounts/TokenAccountsPage'));
const ProxiesPage = lazy(() => import('./pages/proxies/ProxiesPage'));
const APIChannelsPage = lazy(() => import('./pages/system/APIChannelsPage'));
const ModelGatewayPage = lazy(() => import('./pages/system/ModelGatewayPage'));
const ModelGatewayAuditPage = lazy(() => import('./pages/system/ModelGatewayAuditPage'));
const UsersPage = lazy(() => import('./pages/users/UsersPage'));
const BillingPage = lazy(() => import('./pages/billing/BillingPage'));
const PromoPage = lazy(() => import('./pages/promo/PromoPage'));
const CDKPage = lazy(() => import('./pages/promo/CDKPage'));
const PromptGalleryPage = lazy(() => import('./pages/content/PromptGalleryPage'));
const ConfigPage = lazy(() => import('./pages/system/ConfigPage'));
const BillingSettingsPage = lazy(() => import('./pages/system/BillingSettingsPage'));
const RechargePackagesPage = lazy(() => import('./pages/system/RechargePackagesPage'));
const ModelPricesPage = lazy(() => import('./pages/system/ModelPricesPage'));
const LogsPage = lazy(() => import('./pages/logs/LogsPage'));

export default function App() {
  return (
    <>
      <Suspense fallback={<div className="grid h-screen place-items-center text-text-tertiary">加载中…</div>}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route element={<RequireAuth />}>
            <Route element={<AdminLayout />}>
              <Route path="/" element={<Navigate to="/dashboard" replace />} />
              <Route path="/dashboard"  element={<DashboardPage />} />
              <Route path="/accounts"   element={<TokenAccountsPage />} />
              <Route path="/api-channels" element={<APIChannelsPage />} />
              <Route path="/model-gateway" element={<ModelGatewayPage />} />
              <Route path="/model-gateway-audit" element={<ModelGatewayAuditPage />} />
              <Route path="/proxies"    element={<ProxiesPage />} />
              <Route path="/users"      element={<UsersPage />} />
              <Route path="/billing"    element={<BillingPage />} />
              <Route path="/promo"      element={<PromoPage />} />
              <Route path="/cdk"        element={<CDKPage />} />
              <Route path="/prompt-gallery" element={<PromptGalleryPage />} />
              <Route path="/config"     element={<ConfigPage />} />
              <Route path="/billing-settings" element={<BillingSettingsPage />} />
              <Route path="/recharge-packages" element={<RechargePackagesPage />} />
              <Route path="/model-prices" element={<ModelPricesPage />} />
              <Route path="/logs"       element={<LogsPage />} />
            </Route>
          </Route>
        </Routes>
      </Suspense>
      <Toaster />
    </>
  );
}

import { useEffect, useState } from 'react';
import { HashRouter, Navigate, Route, Routes } from 'react-router-dom';
import {
  AUTH_INVALID_EVENT,
  clearAccessToken,
  clearToken,
  getRefreshToken,
  getToken,
  logout,
} from './api/client';
import { OperatorProvider } from './api/operator';
import ConsoleLayout from './components/ConsoleLayout';
import LoginPage from './components/LoginPage';
import Overview from './pages/Overview';
import MatchPage from './pages/Match';
import UserPage from './pages/User';
import Threads from './pages/Threads';
import Operators from './pages/Operators';
import CitationAudit from './pages/CitationAudit';

export default function App() {
  // 登录态三种来源：内存访问令牌（刷新后丢失）、刷新令牌（localStorage，
  // 401 时经无感续期恢复）、机令牌（evals/脚本通道）。
  const [authenticated, setAuthenticated] = useState(
    () => Boolean(getToken()) || Boolean(getRefreshToken()),
  );
  const [authMessage, setAuthMessage] = useState('');

  useEffect(() => {
    const onAuthInvalid = (event: Event) => {
      const detail = (event as CustomEvent<'expired' | 'forbidden'>).detail;
      if (detail === 'expired') {
        // 401（含刷新续期失败）：清空会话回到登录页。
        clearAccessToken();
        clearToken();
        logout();
        setAuthMessage('令牌无效或已被吊销，请重新登录');
        setAuthenticated(false);
      }
      // 403：令牌有效但权限不足 → 保持登录，由页面内提示（见各页）。
    };
    window.addEventListener(AUTH_INVALID_EVENT, onAuthInvalid);
    return () => window.removeEventListener(AUTH_INVALID_EVENT, onAuthInvalid);
  }, []);

  return (
    <HashRouter>
      {authenticated ? (
        <OperatorProvider>
          <Routes>
            <Route
              element={<ConsoleLayout onLogout={() => setAuthenticated(false)} />}
            >
              <Route path="/console" element={<Overview />} />
              <Route path="/console/match/:matchId" element={<MatchPage />} />
              <Route path="/console/match/:matchId/user/:userId" element={<UserPage />} />
              <Route path="/console/threads" element={<Threads />} />
              <Route path="/console/operators" element={<Operators />} />
              <Route path="/console/citations" element={<CitationAudit />} />
              <Route path="*" element={<Navigate to="/console" replace />} />
            </Route>
          </Routes>
        </OperatorProvider>
      ) : (
        <Routes>
          <Route
            path="*"
            element={<LoginPage message={authMessage} onAuthenticated={() => setAuthenticated(true)} />}
          />
        </Routes>
      )}
    </HashRouter>
  );
}

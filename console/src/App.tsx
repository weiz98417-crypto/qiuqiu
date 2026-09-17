import { useEffect, useState } from 'react';
import { HashRouter, Navigate, Route, Routes } from 'react-router-dom';
import { AUTH_INVALID_EVENT, clearToken, getToken } from './api/client';
import { OperatorProvider } from './api/operator';
import ConsoleLayout from './components/ConsoleLayout';
import TokenGate from './components/TokenGate';
import Overview from './pages/Overview';
import MatchPage from './pages/Match';
import UserPage from './pages/User';
import Threads from './pages/Threads';
import Operators from './pages/Operators';
import CitationAudit from './pages/CitationAudit';

export default function App() {
  const [hasToken, setHasToken] = useState(() => Boolean(getToken()));
  const [authMessage, setAuthMessage] = useState('');

  useEffect(() => {
    const onAuthInvalid = (event: Event) => {
      const detail = (event as CustomEvent<'expired' | 'forbidden'>).detail;
      if (detail === 'expired') {
        // 401：令牌无效或已被吊销 → 清除并回到令牌录入页。
        clearToken();
        setAuthMessage('令牌无效或已被吊销，请重新录入');
        setHasToken(false);
      }
      // 403：令牌有效但权限不足 → 保持登录，由页面内提示（见各页）。
    };
    window.addEventListener(AUTH_INVALID_EVENT, onAuthInvalid);
    return () => window.removeEventListener(AUTH_INVALID_EVENT, onAuthInvalid);
  }, []);

  return (
    <HashRouter>
      {hasToken ? (
        <OperatorProvider>
          <Routes>
            <Route element={<ConsoleLayout onLogout={() => setHasToken(false)} />}>
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
          <Route path="*" element={<TokenGate message={authMessage} onValidated={() => setHasToken(true)} />} />
        </Routes>
      )}
    </HashRouter>
  );
}

import React from 'react';
import ReactDOM from 'react-dom/client';
import { ConfigProvider, App as AntApp, theme } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import App from './App';
import { consoleTheme } from './theme';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    {/* 关闭按钮两字标签的自动空格（antd 默认「退 出」），保证文案与可访问名精确。 */}
    <ConfigProvider locale={zhCN} button={{ autoInsertSpace: false }} theme={{ algorithm: theme.darkAlgorithm, ...consoleTheme }}>
      <AntApp>
        <App />
      </AntApp>
    </ConfigProvider>
  </React.StrictMode>,
);

import { Card, Typography } from 'antd';
import { consoleApi } from '../api/client';
import { useAsync } from '../api/useAsync';
import ConsolePageShell from '../components/ConsolePageShell';

const { Paragraph, Text } = Typography;

// 观测页（operations-metrics-stack）：内嵌 Grafana 运营观测面板（kiosk 模式）。
// 面板地址来自 /api/console/config——后端返回同源前缀 /grafana（Go 反代到
// QIUQIU_GRAFANA_URL），同源 iframe 让本页能在加载后注入 CSS 隐藏 Grafana
// 13.2 的 dashboard controls chrome（kiosk 各形态都保留的 64px sticky 条，
// 滚动时盖住面板即「黑杠」）。未配置时不报错，渲染部署指引占位（与
// Operators 页 501 降级卡同气质）。
const hideChromeCss =
  '[class*="dashboard-controls-chrome"], [class*="css-nav-header"] { display: none !important; }';

export default function Observation() {
  const config = useAsync(() => consoleApi.config(), []);
  const grafanaUrl = config.data?.grafanaUrl ?? '';

  const injectChromeHide = () => {
    try {
      const frame = document.querySelector<HTMLIFrameElement>('iframe[title="Grafana 可观测性面板"]');
      const doc = frame?.contentDocument;
      if (!doc) return;
      if (!doc.getElementById('qiuqiu-hide-chrome')) {
        const style = doc.createElement('style');
        style.id = 'qiuqiu-hide-chrome';
        style.textContent = hideChromeCss;
        doc.head.appendChild(style);
      }
    } catch {
      // 跨域（直连 Grafana 形态）时注入被浏览器拦截：控制条保留，可接受。
    }
  };

  return (
    <ConsolePageShell title="观测" error={config.error} loading={config.loading}>
      {grafanaUrl ? (
        <iframe
          title="Grafana 可观测性面板"
          src={`${grafanaUrl}/d/qiuqiu-ops/?kiosk`}
          onLoad={injectChromeHide}
          style={{ width: '100%', height: '78vh', border: 'none' }}
        />
      ) : (
        <Card data-cell="observation-guide">
          <Paragraph strong style={{ marginBottom: 8 }}>
            未配置 QIUQIU_GRAFANA_URL，可观测性面板未启用。
          </Paragraph>
          <Paragraph type="secondary" style={{ marginBottom: 0 }}>
            参见仓库 <Text code>deploy/grafana/README.md</Text> 完成本机/生产部署；配置后本页将以
            kiosk 模式内嵌 Grafana 观测面板。
          </Paragraph>
        </Card>
      )}
    </ConsolePageShell>
  );
}

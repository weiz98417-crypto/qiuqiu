import { Card, Typography } from 'antd';
import { consoleApi } from '../api/client';
import { useAsync } from '../api/useAsync';
import ConsolePageShell from '../components/ConsolePageShell';

const { Paragraph, Text, Title } = Typography;

// 观测页（operations-metrics-stack）：内嵌 Grafana 运营观测面板 + 告警规则
// 列表（grafana-alerting，均 kiosk 模式）。地址来自 /api/console/config——
// 后端返回同源前缀 /grafana（Go 反代到 QIUQIU_GRAFANA_URL），同源 iframe
// 让本页能在加载后注入 CSS 隐藏 Grafana 13.2 的 dashboard controls chrome
// （kiosk 各形态都保留的 64px sticky 条，滚动时盖住面板即「黑杠」）。
// 未配置时不报错，渲染部署指引占位（与 Operators 页 501 降级卡同气质）。
const hideChromeCss =
  '[class*="dashboard-controls-chrome"], [class*="css-nav-header"] { display: none !important; }';

// 告警列表页（/alerting/list）kiosk 下仍有侧栏 chrome，一并隐藏；
// 与面板 iframe 共用同一注入机制（data-grafana-embed 标记）。
const hideAlertingChromeCss = `${hideChromeCss} [class*="nav-toolbar"], [data-testid="navbar"] { display: none !important; } [class*="page-scroll"] { padding-top: 0 !important; }`;

export default function Observation() {
  const config = useAsync(() => consoleApi.config(), []);
  const grafanaUrl = config.data?.grafanaUrl ?? '';

  const injectChromeHide = () => {
    try {
      const frames = document.querySelectorAll<HTMLIFrameElement>(
        'iframe[data-grafana-embed]',
      );
      frames.forEach((frame, index) => {
        const doc = frame.contentDocument;
        if (!doc) return;
        const styleId = `qiuqiu-hide-chrome-${index}`;
        if (doc.getElementById(styleId)) return;
        const style = doc.createElement('style');
        style.id = styleId;
        style.textContent = index === 0 ? hideChromeCss : hideAlertingChromeCss;
        doc.head.appendChild(style);
      });
    } catch {
      // 跨域（直连 Grafana 形态）时注入被浏览器拦截：控制条保留，可接受。
    }
  };

  return (
    <ConsolePageShell title="观测" error={config.error} loading={config.loading}>
      {grafanaUrl ? (
        <>
          <iframe
            title="Grafana 可观测性面板"
            data-grafana-embed
            src={`${grafanaUrl}/d/qiuqiu-ops/?kiosk`}
            onLoad={injectChromeHide}
            style={{ width: '100%', height: '60vh', border: 'none' }}
          />
          <Title level={5} style={{ margin: '16px 0 8px' }}>
            告警规则（qiuqiu-operations，钉钉 + webhook 兜底）
          </Title>
          <iframe
            title="Grafana 告警规则"
            data-grafana-embed
            src={`${grafanaUrl}/alerting/list?kiosk`}
            onLoad={injectChromeHide}
            style={{ width: '100%', height: '40vh', border: 'none' }}
          />
        </>
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

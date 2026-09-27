import { Card, Typography } from 'antd';
import { consoleApi } from '../api/client';
import { useConsoleQuery } from '../api/useConsoleQuery';
import ObservationPage from '../components/ObservationPage';

const { Paragraph, Text } = Typography;

// 观测页（operations-metrics-stack）：内嵌 Grafana 运营观测面板（kiosk 模式）。
// 面板地址来自 /api/console/config（QIUQIU_GRAFANA_URL，TraceRead 即可读）；
// 未配置时不报错，渲染部署指引占位（与 Operators 页 501 降级卡同气质）。
export default function Observation() {
  const config = useConsoleQuery(() => consoleApi.config(), []);
  const grafanaUrl = config.data?.grafanaUrl ?? '';

  return (
    <ObservationPage title="观测" error={config.error} loading={config.loading}>
      {grafanaUrl ? (
        <iframe
          title="Grafana 可观测性面板"
          src={`${grafanaUrl}/d/qiuqiu-ops/?kiosk`}
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
    </ObservationPage>
  );
}

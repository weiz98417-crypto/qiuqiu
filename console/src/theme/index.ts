import type { ThemeConfig } from 'antd';

// QiuQiu 运营台色板 —— 色源 client/lib/theme/app_theme.dart（见 DESIGN.md）。
export const palette = {
  night: '#070B12', // 夜空画布
  terrace: '#111824', // 看台 surface-1
  terraceLift: '#182234', // surface-2
  deepOverlay: '#1E2A3E', // surface-3
  line: '#253142', // 边线
  lineStrong: '#384B66',
  ink: '#F4F1E8', // 墨色
  muted: '#AAB4C0', // 弱墨
  subtle: '#7C8794', // 隐墨
  orange: '#FF6B35', // 球场橙（主强调）
  orangeHover: '#FF8559',
  orangeDeep: '#E83A24', // 深橙（按压）
  skyBlue: '#55A8FF', // 天蓝（次强调）
  green: '#5FCB8B',
  yellow: '#F2C94C',
  red: '#F05D5E',
} as const;

export const fontFamily =
  '"PingFang SC", "Microsoft YaHei", "Noto Sans SC", system-ui, -apple-system, sans-serif';

// antd ConfigProvider 暗色 token —— 映射自 DESIGN.md。
export const consoleTheme: ThemeConfig = {
  token: {
    colorPrimary: palette.orange,
    colorInfo: palette.skyBlue,
    colorSuccess: palette.green,
    colorWarning: palette.yellow,
    colorError: palette.red,
    colorBgBase: palette.night,
    colorBgLayout: palette.night,
    colorBgContainer: palette.terrace,
    colorBgElevated: palette.deepOverlay,
    colorText: palette.ink,
    colorTextSecondary: palette.muted,
    colorTextTertiary: palette.subtle,
    colorBorder: palette.line,
    colorBorderSecondary: palette.line,
    borderRadius: 8,
    borderRadiusLG: 12,
    fontFamily,
    fontSize: 14,
  },
  components: {
    Layout: {
      siderBg: palette.night,
      headerBg: palette.night,
      bodyBg: palette.night,
    },
    Menu: {
      itemBg: 'transparent',
      activeBarBorderWidth: 0,
      itemSelectedBg: palette.terrace,
      itemSelectedColor: palette.orange,
      itemColor: palette.muted,
      itemHoverColor: palette.ink,
    },
    Table: {
      headerBg: palette.terraceLift,
      rowHoverBg: palette.terraceLift,
      borderColor: palette.line,
    },
    Card: {
      colorBorderSecondary: palette.line,
    },
  },
};

import { alpha, createTheme } from '@mui/material/styles'

const fontSans = '"Inter Variable", Inter, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif'
export const fontMono = '"JetBrains Mono Variable", "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace'

const brand = { main: '#4f46e5', light: '#818cf8', dark: '#3730a3', contrastText: '#ffffff' }

export const theme = createTheme({
  cssVariables: { colorSchemeSelector: 'class' },
  colorSchemes: {
    light: {
      palette: {
        primary: brand,
        secondary: { main: '#0f766e' },
        success: { main: '#15803d' },
        warning: { main: '#b45309' },
        error: { main: '#b91c1c' },
        info: { main: '#0369a1' },
        background: { default: '#f6f7f9', paper: '#ffffff' },
        text: { primary: '#0f172a', secondary: '#475569' },
        divider: '#e5e7eb',
      },
    },
    dark: {
      palette: {
        primary: { main: '#818cf8', light: '#a5b4fc', dark: '#6366f1', contrastText: '#0b0f17' },
        secondary: { main: '#2dd4bf' },
        success: { main: '#4ade80' },
        warning: { main: '#fbbf24' },
        error: { main: '#f87171' },
        info: { main: '#38bdf8' },
        background: { default: '#0b0f17', paper: '#111723' },
        text: { primary: '#e5e7eb', secondary: '#94a3b8' },
        divider: 'rgba(148, 163, 184, 0.16)',
      },
    },
  },
  shape: { borderRadius: 8 },
  typography: {
    fontFamily: fontSans,
    fontSize: 14,
    h4: { fontSize: '1.5rem', fontWeight: 650, letterSpacing: '-0.02em' },
    h5: { fontSize: '1.25rem', fontWeight: 650, letterSpacing: '-0.01em' },
    h6: { fontSize: '1rem', fontWeight: 600 },
    subtitle1: { fontWeight: 600 },
    subtitle2: { fontWeight: 600, fontSize: '0.8125rem' },
    body2: { fontSize: '0.8125rem' },
    overline: { fontSize: '0.6875rem', fontWeight: 600, letterSpacing: '0.08em' },
    button: { textTransform: 'none', fontWeight: 600 },
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: { WebkitFontSmoothing: 'antialiased' },
        'code, pre, kbd': { fontFamily: fontMono },
      },
    },
    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: { root: { borderRadius: 8 }, sizeSmall: { paddingInline: 10 } },
    },
    MuiPaper: {
      defaultProps: { elevation: 0 },
      styleOverrides: { root: { backgroundImage: 'none' } },
    },
    MuiCard: {
      defaultProps: { variant: 'outlined' },
      styleOverrides: { root: { borderRadius: 12 } },
    },
    MuiCardHeader: {
      defaultProps: { titleTypographyProps: { variant: 'h6' }, subheaderTypographyProps: { variant: 'body2' } },
      styleOverrides: { root: { padding: '16px 20px' }, action: { alignSelf: 'center', margin: 0 } },
    },
    MuiCardContent: { styleOverrides: { root: { padding: 20, '&:last-child': { paddingBottom: 20 } } } },
    MuiChip: {
      styleOverrides: { root: { fontWeight: 500, borderRadius: 6 }, sizeSmall: { height: 22, fontSize: '0.75rem' } },
    },
    MuiTextField: { defaultProps: { size: 'small', fullWidth: true } },
    MuiFormControl: { defaultProps: { size: 'small', fullWidth: true } },
    MuiDialog: { styleOverrides: { paper: { borderRadius: 12 } } },
    MuiDialogTitle: { styleOverrides: { root: { fontSize: '1.0625rem', fontWeight: 600, padding: '20px 24px 8px' } } },
    MuiDialogActions: { styleOverrides: { root: { padding: '12px 24px 20px' } } },
    MuiTooltip: { defaultProps: { arrow: true } },
    MuiListItemButton: {
      styleOverrides: {
        root: ({ theme }) => ({
          borderRadius: 8,
          marginBottom: 2,
          '&.Mui-selected': {
            backgroundColor: alpha(theme.palette.primary.main, 0.1),
            color: theme.palette.primary.main,
            '& .MuiListItemIcon-root': { color: theme.palette.primary.main },
          },
          '&.Mui-selected:hover': { backgroundColor: alpha(theme.palette.primary.main, 0.14) },
        }),
      },
    },
    MuiListItemIcon: { styleOverrides: { root: { minWidth: 34 } } },
    MuiAlert: { styleOverrides: { root: { borderRadius: 8 } } },
  },
})

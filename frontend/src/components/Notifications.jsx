import { createContext, useCallback, useContext, useMemo, useState } from 'react'
import { Alert, Snackbar } from '@mui/material'

const NotifyContext = createContext(() => {})

export function NotificationsProvider({ children }) {
  const [toast, setToast] = useState(null)

  const notify = useCallback((message, severity = 'success') => {
    setToast({ message, severity, key: Date.now() })
  }, [])
  const api = useMemo(
    () => ({
      success: (m) => notify(m, 'success'),
      error: (m) => notify(m instanceof Error ? m.message : m, 'error'),
      info: (m) => notify(m, 'info'),
    }),
    [notify],
  )

  const close = (_, reason) => {
    if (reason !== 'clickaway') setToast(null)
  }

  return (
    <NotifyContext.Provider value={api}>
      {children}
      <Snackbar
        key={toast?.key}
        open={!!toast}
        autoHideDuration={toast?.severity === 'error' ? 8000 : 4000}
        onClose={close}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
      >
        {toast ? (
          <Alert onClose={close} severity={toast.severity} variant="filled" sx={{ minWidth: 300, maxWidth: 460 }}>
            {toast.message}
          </Alert>
        ) : undefined}
      </Snackbar>
    </NotifyContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useNotify = () => useContext(NotifyContext)

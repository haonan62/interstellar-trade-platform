import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
// Self-hosted fonts: no third-party requests, and the CSP stays 'self'-only.
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import App from './app/App'

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

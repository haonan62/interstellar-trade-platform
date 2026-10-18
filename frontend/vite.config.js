import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      // changeOrigin must stay false: the backend's CSRF check compares the
      // browser's Origin (localhost:5173) with the Host header. Vite's string
      // shorthand would rewrite Host to localhost:8080 and every cookie POST
      // would be refused with "cross-origin request refused".
      '/api': { target: 'http://localhost:8080', changeOrigin: false }
    }
  }
})

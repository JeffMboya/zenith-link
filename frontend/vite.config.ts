import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import cesium from 'vite-plugin-cesium'

export default defineConfig({
  plugins: [react(), cesium()],
  server: {
    port: 5173,
    proxy: {
      '/ws': { target: 'ws://localhost:8081', ws: true },
      '/metrics': 'http://localhost:8081',
      '/command': 'http://localhost:8081',
      '/health': 'http://localhost:8081',
    },
  },
})

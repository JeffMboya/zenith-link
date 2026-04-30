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
      '/windows': 'http://localhost:8080',
      '/state': 'http://localhost:8080',
      '/track': 'http://localhost:8080',
      '/constellation': 'http://localhost:8080',
      '/tle': 'http://localhost:8080',
      '/events': 'http://localhost:8080',
      '/payload': 'http://localhost:8080',
      '/relay/health': {
        target: 'http://localhost:8082',
        rewrite: (path: string) => path.replace(/^\/relay/, ''),
      },
      '/relay2/health': {
        target: 'http://localhost:8083',
        rewrite: (path: string) => path.replace(/^\/relay2/, ''),
      },
    },
  },
})

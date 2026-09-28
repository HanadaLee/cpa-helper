import { readFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

function readAppVersion(): string {
  return readFileSync(fileURLToPath(new URL('../VERSION', import.meta.url)), 'utf8').trim()
}

const appVersion = readAppVersion()
const logoUrl = `/logo.png?v=${encodeURIComponent(appVersion)}`
const apiProxyTarget = process.env.CPA_HELPER_PROXY_TARGET?.trim() || 'http://127.0.0.1:18317'

export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    {
      name: 'cpa-helper-html-assets',
      transformIndexHtml: {
        order: 'pre',
        handler(html) {
          return html.replaceAll('__CPA_HELPER_LOGO_URL__', logoUrl)
        },
      },
    },
  ],
  define: {
    'import.meta.env.VITE_APP_VERSION': JSON.stringify(appVersion),
  },
  build: {
    chunkSizeWarningLimit: 700,
    rollupOptions: {
      output: {
        manualChunks: {
          unovis: ['@unovis/vue'],
        },
      },
    },
  },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: apiProxyTarget,
        changeOrigin: true,
      },
      '/cas': {
        target: apiProxyTarget,
        changeOrigin: true,
      },
      '/oidc': {
        target: apiProxyTarget,
        changeOrigin: true,
      },
    },
  },
})

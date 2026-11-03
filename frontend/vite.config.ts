import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'

/**
 * One identifier per production build. The same value is injected into
 * `index.html` (`meta[name="hb-build-id"]`) and into the bundle
 * (`__HB_BUILD_ID__`), so a running tab can compare what the server now serves
 * against what it actually loaded. The HTML entry is the only source of truth:
 * assets are content hashed and the HTML marker matches its compiled bundle.
 */
const buildId = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`

function buildIdPlugin(): Plugin {
  return {
    name: 'household-bills:build-id',
    apply: 'build',
    transformIndexHtml: {
      order: 'pre',
      handler(html) {
        const meta = `<meta name="hb-build-id" content="${buildId}" />`
        return html.includes('</head>')
          ? html.replace('</head>', `    ${meta}\n  </head>`)
          : `${meta}\n${html}`
      },
    },
  }
}

export default defineConfig({
  plugins: [vue(), buildIdPlugin()],
  define: {
    __HB_BUILD_ID__: JSON.stringify(buildId),
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8000',
        changeOrigin: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})

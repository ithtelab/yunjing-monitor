const SHELL_CACHE = 'monitor-shell-v2'
const APP_SHELL = ['/', '/logo.svg', '/manifest.webmanifest']
const isPublicAppNavigation = (pathname) => (
  pathname === '/' ||
  pathname === '/monitor' || pathname.startsWith('/monitor/') ||
  pathname === '/market' || pathname.startsWith('/market/') ||
  pathname.startsWith('/status/')
)

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(SHELL_CACHE).then((cache) => cache.addAll(APP_SHELL)))
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((key) => key !== SHELL_CACHE).map((key) => caches.delete(key))))
      .then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const request = event.request
  if (request.method !== 'GET') return

  const url = new URL(request.url)
  if (
    url.origin !== self.location.origin ||
    url.pathname.startsWith('/api/') ||
    url.pathname === '/config.json' ||
    url.pathname === '/ws' ||
    url.pathname === '/openapi.json' ||
    url.pathname.startsWith('/admin') ||
    url.pathname.startsWith('/download/') ||
    url.pathname.startsWith('/install/')
  ) return

  if (request.mode === 'navigate') {
    if (!isPublicAppNavigation(url.pathname)) return
    event.respondWith(
      fetch(request)
        .then((response) => {
          if (response.ok && (response.headers.get('Content-Type') || '').includes('text/html')) {
            caches.open(SHELL_CACHE).then((cache) => cache.put('/', response.clone()))
          }
          return response
        })
        .catch(() => caches.match('/'))
    )
    return
  }

  const staticAsset = url.pathname.startsWith('/assets/') || ['/logo.svg', '/favicon.svg', '/favicon.ico', '/manifest.webmanifest'].includes(url.pathname)
  if (staticAsset) {
    event.respondWith(
      caches.match(request).then((cached) => cached || fetch(request).then((response) => {
        if (response.ok) caches.open(SHELL_CACHE).then((cache) => cache.put(request, response.clone()))
        return response
      }))
    )
  }
})

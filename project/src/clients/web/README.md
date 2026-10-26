# Web and PWA client

This Vue 3 + TypeScript application is shared by the browser client and the PWA.

## Run locally

```sh
npm install
npm run dev
```

Use `localhost` for local PWA testing. Browsers require a secure context for service workers and installation; deploy the app over HTTPS outside local development.

For production hosting, run `npm ci && npm run build`, then serve `dist/` from the manifest scope (`/` with the current configuration). Configure Nginx to rewrite client-side routes to `index.html` and allow `sw.js` to be fetched from the app root. If deploying under a subpath, update Vite's `base` and the manifest's `start_url` and `scope` together. The maintained Nginx configuration is `nginx/nginx.conf`; API upstreams target the host's loopback Go service ports.

## Check the production PWA

```sh
npm run build
npm run preview
```

Open the preview URL in a supported browser and inspect the Web App Manifest and service worker in browser developer tools. The service worker precaches only the built application shell and static assets. API requests, media streams, and MCP traffic are not cached. Offline use therefore covers the application shell only; it does not make live data available offline.

PWA update notices are handled by the shared app shell. Push notifications, offline data workflows, and platform-specific installation guidance can be added when those product requirements are designed.

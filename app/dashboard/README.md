# PPP Dashboard

React + Vite SPA for monitoring and controlling PPP devices/tasks/workflows/events.

## Local Development

```bash
cd app/dashboard
npm install
cp .env.example .env
npm run dev
```

Default dev URL: `http://localhost:5173/`

## Environment Variables

Only `VITE_` prefixed variables are exposed to browser runtime.

```bash
VITE_API_URL=http://localhost:3000
VITE_POLL_MS=5000
VITE_METRICS_URL=http://localhost:3000/metrics
# Optional
VITE_ANDROID_IDENTITY_MAP={"6d5c110f26f0d28d":"Pixel 7"}
```

Use [`.env.example`](./.env.example) as baseline.

## Build Validation (Recommended Before Push)

```bash
npm run type-check
npm run build
npm run preview:prod
```

Or single command:

```bash
npm run build:verify
```

Preview URL: `http://localhost:4173/`

## SPA Deployment Notes (Critical)

If deployed behind static hosting, all unknown routes must rewrite to `index.html`.

### Nginx

```nginx
location / {
  try_files $uri $uri/ /index.html;
}
```

### Netlify

Create `public/_redirects`:

```text
/* /index.html 200
```

### Vercel

Use `vercel.json` rewrite:

```json
{
  "rewrites": [{ "source": "/(.*)", "destination": "/index.html" }]
}
```

## Caching Strategy

- `index.html`: `Cache-Control: no-cache` (or very short max-age)
- Hashed assets (`/assets/*.js`, `/assets/*.css`): `Cache-Control: public, max-age=31536000, immutable`

This avoids stale app-shell after new deployment.

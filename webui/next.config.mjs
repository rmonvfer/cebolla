/** @type {import('next').NextConfig} */
const explorer = process.env.EXPLORER_URL || 'http://explorer:8088';
const nextConfig = {
  output: 'standalone',
  reactStrictMode: true,
  poweredByHeader: false,
  async rewrites() {
    // Proxy the data API to the Go explorer service (server-side, same-origin
    // to the browser). Keeps Prometheus/Postgres on the internal network.
    return [{ source: '/api/:path*', destination: `${explorer}/api/:path*` }];
  },
  async headers() {
    // The UI only ever renders crawled onion data as text; never let it frame,
    // leak a referrer, or sniff types. (Scripts are Next's own bundles.)
    return [
      {
        source: '/:path*',
        headers: [
          { key: 'Referrer-Policy', value: 'no-referrer' },
          { key: 'X-Content-Type-Options', value: 'nosniff' },
          { key: 'X-Frame-Options', value: 'DENY' },
        ],
      },
    ];
  },
};
export default nextConfig;

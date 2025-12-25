// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
  integrations: [
    starlight({
      title: 'WarpDL',
      logo: {
        src: './src/assets/logo.png',
      },
      favicon: '/logo.png',
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/warpdl/warpdl'
        }
      ],
      sidebar: [
        {
          label: 'Getting Started',
          items: [
            { label: 'Installation', slug: 'getting-started/installation' },
            { label: 'Quick Start', slug: 'getting-started/quick-start' },
          ],
        },
        {
          label: 'Usage',
          items: [
            { label: 'CLI Reference', slug: 'usage/cli-reference' },
            { label: 'Daemon', slug: 'usage/daemon' },
            { label: 'Resume Downloads', slug: 'usage/resume-downloads' },
            { label: 'Extensions', slug: 'usage/extensions' },
          ],
        },
        {
          label: 'Configuration',
          items: [
            { label: 'Environment Variables', slug: 'configuration/environment-variables' },
            { label: 'Service Setup', slug: 'configuration/service-setup' },
          ],
        },
        {
          label: 'Troubleshooting',
          autogenerate: { directory: 'troubleshooting' },
        },
        {
          label: 'Development',
          autogenerate: { directory: 'development' },
        },
        {
          label: 'API',
          items: [
            { label: 'Extension API', slug: 'api/extension-api' },
          ],
        },
      ],
    }),
  ],
});

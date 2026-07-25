import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const siteUrl = (process.env.DOCS_SITE_URL ?? 'http://localhost:3000').replace(
  /\/+$/,
  '',
);

const config: Config = {
  title: 'Merlon',
  tagline: 'Self-hosted AML/CFT platform for non-bank financial institutions',
  favicon: 'img/favicon.svg',

  // Future flags, see https://docusaurus.io/docs/api/docusaurus-config#future
  future: {
    v4: true, // Improve compatibility with the upcoming Docusaurus v4
  },

  // Production is supplied by the deployment environment; localhost keeps
  // local development and pull-request builds self-contained.
  url: siteUrl,
  // Set the /<baseUrl>/ pathname under which your site is served
  baseUrl: '/',

  onBrokenLinks: 'throw',
  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en', 'ja'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          // Serve the repository's plain-Markdown docs directly; they stay
          // site-independent at the repo root.
          path: '../docs',
          routeBasePath: 'docs',
          sidebarPath: './sidebars.ts',
          editUrl: ({docPath}) =>
            `https://github.com/ksuk/merlon/edit/main/docs/${docPath}`,
          // Architecture Decision Records and internal audit standards are
          // internal material, not published end-user documentation.
          exclude: ['decisions/**', 'standards/**'],
          // Versioning: leave the standard docs-plugin versioning capability
          // available. No version snapshot has been cut yet, so the site
          // only serves the "current" version for now.
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  // Interim local search until Algolia DocSearch is set up.
  themes: [
    [
      '@easyops-cn/docusaurus-search-local',
      {
        hashed: true,
        language: ['en', 'ja'],
        indexBlog: false,
        docsDir: '../docs',
      },
    ],
  ],

  themeConfig: {
    image: 'img/og-image.png',
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      // No `title`: the wide logo lockup already carries the wordmark, so a
      // text link beside it would duplicate it. Both modes use the same
      // 800x200 lockup (img/icon.svg is the square favicon artwork and bakes
      // in a white background, so it is not usable in the navbar).
      logo: {
        alt: 'Merlon',
        src: 'img/logo.svg',
        srcDark: 'img/logo-inverse.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          type: 'doc',
          docId: 'api/openapi',
          position: 'left',
          label: 'Reference',
        },
        {
          type: 'localeDropdown',
          position: 'right',
        },
        {
          href: 'https://github.com/ksuk/merlon',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {
              label: 'Getting Started',
              to: '/docs/getting-started',
            },
            {
              label: 'Architecture',
              to: '/docs/architecture',
            },
            {
              label: 'Configuration',
              to: '/docs/configuration',
            },
          ],
        },
        {
          title: 'Reference',
          items: [
            {
              label: 'REST API',
              to: '/docs/api/openapi',
            },
            {
              label: 'Rule Schemas',
              to: '/docs/api/schema',
            },
          ],
        },
        {
          title: 'Project',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/ksuk/merlon',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Merlon.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;

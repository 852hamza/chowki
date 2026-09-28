// The Chowki documentation site. It builds the pages in ../docs, the
// developer guide, which stay readable on GitHub as they are.

const fs = require('node:fs');
const path = require('node:path');

// The project's identity lives only in project.env; the site reads it.
function projectEnv() {
  const vars = {};
  const text = fs.readFileSync(path.join(__dirname, '..', 'project.env'), 'utf8');
  for (const line of text.split('\n')) {
    const m = line.match(/^([A-Z_]+)=(.*)$/);
    if (m) vars[m[1]] = m[2].trim();
  }
  return vars;
}

const env = projectEnv();
const repoUrl = `https://github.com/${env.GITHUB_OWNER}/${env.GITHUB_REPO}`;
// The address that the site is served at: DOCS_SITE_URL when the Pages
// workflow sets it, else the website of project.env.
const site = new URL(process.env.DOCS_SITE_URL || env.WEBSITE_URL || `https://${env.PROJECT_DOMAIN}`);

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Chowki',
  tagline: 'A self-hosted AI gateway that saves tokens, blocks secrets and reports honest costs.',
  favicon: 'img/icon.svg',
  url: site.origin,
  baseUrl: site.pathname.endsWith('/') ? site.pathname : `${site.pathname}/`,
  organizationName: env.GITHUB_OWNER,
  projectName: env.GITHUB_REPO,
  trailingSlash: false,
  onBrokenLinks: 'throw',
  onBrokenAnchors: 'throw',
  markdown: {
    // The guide is plain Markdown, where <VIRTUAL_KEY> is text, not JSX.
    format: 'detect',
    hooks: {onBrokenMarkdownLinks: 'throw'},
  },
  i18n: {defaultLocale: 'en', locales: ['en']},
  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          path: '../docs',
          routeBasePath: 'docs',
          sidebarPath: require.resolve('./sidebars.js'),
          // Files for the guide's writers, not its readers.
          exclude: ['README.md', 'STYLE_GUIDE.md', '_templates/**'],
          editUrl: `${repoUrl}/edit/main/docs/`,
          showLastUpdateTime: true,
        },
        blog: {
          path: 'blog',
          showReadingTime: true,
          onUntruncatedBlogPosts: 'ignore',
        },
        theme: {customCss: require.resolve('./src/css/custom.css')},
      }),
    ],
  ],
  plugins: [require.resolve('docusaurus-lunr-search')],
  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      navbar: {
        title: 'Chowki',
        logo: {alt: 'Chowki', src: 'img/icon.svg'},
        items: [
          {type: 'docSidebar', sidebarId: 'guide', position: 'left', label: 'Guide'},
          {href: repoUrl, label: 'GitHub', position: 'right'},
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'Guide',
            items: [
              {label: 'Send your first request', to: '/docs/get-started/quickstart'},
              {label: 'Install Chowki', to: '/docs/get-started/install'},
            ],
          },
          {
            title: 'Project',
            items: [
              {label: 'GitHub', href: repoUrl},
              {label: 'Report a bug', href: `${repoUrl}/issues`},
            ],
          },
        ],
        copyright: 'Chowki is open source, under the Apache License 2.0.',
      },
      colorMode: {respectPrefersColorScheme: true},
    }),
};

module.exports = config;

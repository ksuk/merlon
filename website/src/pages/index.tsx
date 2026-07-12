import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import Translate, {translate} from '@docusaurus/Translate';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';

import PersonaCards from '@site/src/components/PersonaCards';
import FeatureHighlights from '@site/src/components/FeatureHighlights';

import styles from './index.module.css';

function HomepageHeader() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={clsx('hero', 'hero--merlon', styles.heroBanner)}>
      <div className="container">
        <Heading as="h1" className="hero__title">
          {siteConfig.title}
        </Heading>
        <p className="hero__subtitle">{siteConfig.tagline}</p>
        <div className={styles.buttons}>
          <Link
            className="button button--primary button--lg"
            to="/docs/getting-started">
            <Translate id="homepage.cta.getStarted">Get Started</Translate>
          </Link>
          <Link
            className="button button--secondary button--lg"
            to="https://github.com/ksuk/merlon">
            <Translate id="homepage.cta.github">GitHub</Translate>
          </Link>
        </div>
      </div>
    </header>
  );
}

export default function Home(): ReactNode {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout
      title={siteConfig.title}
      description={translate({
        id: 'homepage.meta.description',
        message: siteConfig.tagline,
      })}>
      <HomepageHeader />
      <main>
        <PersonaCards />
        <FeatureHighlights />
      </main>
    </Layout>
  );
}

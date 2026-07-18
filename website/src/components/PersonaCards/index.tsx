import type {ReactNode} from 'react';
import Link from '@docusaurus/Link';
import Translate, {translate} from '@docusaurus/Translate';
import Heading from '@theme/Heading';

import styles from './styles.module.css';

type Persona = {
  id: string;
  titleId: string;
  titleDefault: string;
  descriptionId: string;
  descriptionDefault: string;
  to: string;
};

const personas: Persona[] = [
  {
    id: 'deploy',
    titleId: 'homepage.cards.deploy.title',
    titleDefault: 'Deploy & Operate',
    descriptionId: 'homepage.cards.deploy.description',
    descriptionDefault:
      'Stand up Merlon and run it in production, from install to day-to-day operations.',
    to: '/docs/getting-started',
  },
  {
    id: 'compliance',
    titleId: 'homepage.cards.compliance.title',
    titleDefault: 'Compliance',
    descriptionId: 'homepage.cards.compliance.description',
    descriptionDefault:
      'Map Merlon\'s controls to FSA guidelines and regulatory requirements.',
    to: '/docs/compliance/fsa-guideline-mapping',
  },
  {
    id: 'integrate',
    titleId: 'homepage.cards.integrate.title',
    titleDefault: 'Integrate',
    descriptionId: 'homepage.cards.integrate.description',
    descriptionDefault:
      'Connect Merlon to your core banking system via the REST API.',
    to: '/docs/adapter-guide',
  },
  {
    id: 'contribute',
    titleId: 'homepage.cards.contribute.title',
    titleDefault: 'Contribute',
    descriptionId: 'homepage.cards.contribute.description',
    descriptionDefault:
      'Set up a local development environment and submit changes.',
    to: '/docs/development/setup',
  },
  {
    id: 'architecture',
    titleId: 'homepage.cards.architecture.title',
    titleDefault: 'Architecture',
    descriptionId: 'homepage.cards.architecture.description',
    descriptionDefault:
      'Understand how the Go API, native engine, and UI fit together.',
    to: '/docs/architecture',
  },
];

export default function PersonaCards(): ReactNode {
  return (
    <section className={styles.section}>
      <div className="container">
        <Heading as="h2" className={styles.sectionTitle}>
          <Translate id="homepage.personas.heading">
            Find your entry point
          </Translate>
        </Heading>
        <div className={styles.grid}>
          {personas.map((persona) => (
            <Link
              key={persona.id}
              to={persona.to}
              className={styles.card}>
              <Heading as="h3" className={styles.cardTitle}>
                {translate({
                  id: persona.titleId,
                  message: persona.titleDefault,
                })}
              </Heading>
              <p className={styles.cardDescription}>
                {translate({
                  id: persona.descriptionId,
                  message: persona.descriptionDefault,
                })}
              </p>
            </Link>
          ))}
        </div>
      </div>
    </section>
  );
}

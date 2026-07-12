import type {ReactNode} from 'react';
import {translate} from '@docusaurus/Translate';
import Heading from '@theme/Heading';

import styles from './styles.module.css';

type Feature = {
  id: string;
  titleId: string;
  titleDefault: string;
  descriptionId: string;
  descriptionDefault: string;
};

const features: Feature[] = [
  {
    id: 'score-driven',
    titleId: 'homepage.features.scoreDriven.title',
    titleDefault: 'Score-Driven Architecture',
    descriptionId: 'homepage.features.scoreDriven.description',
    descriptionDefault:
      'CDD score is the central axis: TM thresholds, case priority, and screening frequency all derive from it.',
  },
  {
    id: 'auditability',
    titleId: 'homepage.features.auditability.title',
    titleDefault: 'Auditability First',
    descriptionId: 'homepage.features.auditability.description',
    descriptionDefault:
      'Every decision rationale is recorded reproducibly, with deterministic output pinned by tests.',
  },
  {
    id: 'fail-alert',
    titleId: 'homepage.features.failAlert.title',
    titleDefault: 'Fail-Alert',
    descriptionId: 'homepage.features.failAlert.description',
    descriptionDefault:
      'On failure, the system errs toward alerting, preferring false positives over missed detections.',
  },
  {
    id: 'config-as-product',
    titleId: 'homepage.features.configAsProduct.title',
    titleDefault: 'Configuration as the Product',
    descriptionId: 'homepage.features.configAsProduct.description',
    descriptionDefault:
      'Rules are expressed as JSON/YAML configuration, never hardcoded.',
  },
];

export default function FeatureHighlights(): ReactNode {
  return (
    <section className={styles.section}>
      <div className="container">
        <div className={styles.grid}>
          {features.map((feature) => (
            <div key={feature.id} className={styles.item}>
              <Heading as="h3" className={styles.itemTitle}>
                {translate({
                  id: feature.titleId,
                  message: feature.titleDefault,
                })}
              </Heading>
              <p className={styles.itemDescription}>
                {translate({
                  id: feature.descriptionId,
                  message: feature.descriptionDefault,
                })}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

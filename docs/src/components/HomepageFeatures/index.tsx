import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import styles from './styles.module.css';

type FeatureItem = {
  title: string;
  to: string;
  description: ReactNode;
};

const FeatureList: FeatureItem[] = [
  {
    title: 'Engineered Labor Standards',
    to: '/docs/business-context/domain-vision',
    description: (
      <>
        An expected time per TaskType (PICK, PACK, SLAM), kept as append-only
        history — revising a standard closes the prior record rather than
        overwriting it, so past scores stay historically accurate.
      </>
    ),
  },
  {
    title: 'Actual-vs-Standard Scoring',
    to: '/docs/ddd/subdomain-classification',
    description: (
      <>
        Every completed task is scored against the standard genuinely active
        when it finished — resolved once, at ingestion time, and frozen
        forever. Never recomputed from a later revision.
      </>
    ),
  },
  {
    title: 'Pure Kafka Consumer',
    to: '/docs/ecosystem/context-map',
    description: (
      <>
        No REST or Go-import dependency on fulfillment-execution or
        workforce-management. Everything needed travels on the existing
        TaskCompleted event — choreography, not orchestration.
      </>
    ),
  },
  {
    title: 'Visibility, Not Enforcement',
    to: '/docs/adr/0002-new-bounded-context-not-extension-of-workforce-or-fulfillment',
    description: (
      <>
        This context surfaces the number — no automatic coaching, no
        automatic pay-for-performance, no gate on a below-standard
        associate's ability to claim tasks.
      </>
    ),
  },
];

function Feature({title, to, description}: FeatureItem) {
  return (
    <div className={clsx('col col--3')}>
      <Link to={to} className={styles.featureCard}>
        <Heading as="h3" className={styles.featureTitle}>
          {title}
        </Heading>
        <p className={styles.featureBody}>{description}</p>
      </Link>
    </div>
  );
}

export default function HomepageFeatures(): ReactNode {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}

import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/remote-access.module.css';

export default function RemoteAccess() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.remote} website-reveal`}>
      <h2 className={styles.sectionTitle}>Remote access</h2>
      <p className={styles.painPoint}>
        You kicked off a fleet of agents and left your desk. Now you're on the train, wondering
        whether they're done or stuck. Your laptop is at home, behind a NAT, on a private network.
      </p>
      <div className={styles.commandBlock}>
        <pre className={styles.commandCode}>schmux remote on</pre>
        <span className={styles.commandLabel}>That's it.</span>
      </div>
      <p className={styles.note}>
        A Cloudflare tunnel spins up with password protection. Push notifications via ntfy tell your
        phone when it's ready. Open the dashboard on any device — check on agents, approve requests,
        kick off new work — then close the tab and go back to your life.
      </p>
    </section>
  );
}

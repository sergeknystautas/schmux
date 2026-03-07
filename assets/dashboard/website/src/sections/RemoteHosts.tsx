import { useScrollReveal } from '../hooks/useScrollReveal';
import styles from '../styles/sections.module.css';

export default function RemoteHosts() {
  const ref = useScrollReveal();

  return (
    <section ref={ref} className={`${styles.section} website-reveal`}>
      <h2 className={styles.sectionTitle}>Remote hosts</h2>
      <p className={styles.painPoint}>
        Your laptop has 16 gigs of RAM and the fans are already screaming. You have a 96-core cloud
        instance sitting idle. You'd rather run agents there, but orchestrating tmux sessions over
        SSH sounds like its own project.
      </p>
      <p className={styles.solution}>
        Configure a remote "flavor" — connection command, provisioning steps, workspace path — and
        spawn agents on any machine you can SSH into. Remote sessions show up in the dashboard
        alongside local ones. Same terminal streaming, same status signals, same management
        interface.
      </p>
    </section>
  );
}

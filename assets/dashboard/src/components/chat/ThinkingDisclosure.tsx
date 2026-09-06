import styles from './chat.module.css';

export default function ThinkingDisclosure({ text }: { text: string }) {
  return (
    <details className={styles.thinking}>
      <summary>Thinking</summary>
      <pre>{text}</pre>
    </details>
  );
}

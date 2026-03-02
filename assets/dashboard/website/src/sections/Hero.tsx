import { useCallback, useEffect, useState } from 'react';
import styles from '../styles/hero.module.css';

const slides = [
  {
    src: './screenshot1.jpeg',
    alt: 'schmux dashboard with multiple agents and terminal output',
    caption:
      'Multiple agents working in parallel across isolated workspaces, all visible from one dashboard.',
  },
  {
    src: './screenshot2.jpeg',
    alt: 'schmux code review session with validation results',
    caption:
      'Automated code review with actionable findings — severity, file locations, and fix suggestions.',
  },
  {
    src: './screenshot3.jpeg',
    alt: 'schmux session running a development server',
    caption:
      'Live terminal output from any session — watch builds, servers, and tests as they run.',
  },
  {
    src: './screenshot4.jpeg',
    alt: 'schmux web preview showing an application built by agents',
    caption:
      'Preview what agents build in real time — web apps render inline alongside the terminal.',
  },
  {
    src: './screenshot5.jpeg',
    alt: 'schmux commit graph with branch visualization',
    caption: 'Visual commit graph with one-click branching, cherry-picking, and push-to-remote.',
  },
];

export default function Hero() {
  const [activeIndex, setActiveIndex] = useState(0);

  const prev = useCallback(() => {
    setActiveIndex((i) => Math.max(0, i - 1));
  }, []);

  const next = useCallback(() => {
    setActiveIndex((i) => Math.min(slides.length - 1, i + 1));
  }, []);

  // Keyboard navigation
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'ArrowLeft') prev();
      if (e.key === 'ArrowRight') next();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [prev, next]);

  return (
    <section className={styles.hero}>
      <h1 className={styles.title}>schmux</h1>
      <p className={styles.tagline}>Factory-floor tooling for AI-assisted software development.</p>
      <p className={styles.description}>
        Run multiple AI coding agents in parallel, each in its own
        <br className={styles.descriptionBr} /> isolated version-controlled workspace, all visible
        from one dashboard.
      </p>

      {/* Screenshot viewer */}
      <div className={styles.carousel}>
        {/* Prev arrow */}
        <button
          className={`${styles.arrow} ${styles.arrowLeft}`}
          onClick={prev}
          disabled={activeIndex === 0}
          aria-label="Previous screenshot"
        >
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
            <path
              d="M12.5 15L7.5 10L12.5 5"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </button>

        {/* Active screenshot */}
        <img
          key={activeIndex}
          src={slides[activeIndex].src}
          alt={slides[activeIndex].alt}
          className={styles.slideImage}
          draggable={false}
        />

        {/* Next arrow */}
        <button
          className={`${styles.arrow} ${styles.arrowRight}`}
          onClick={next}
          disabled={activeIndex === slides.length - 1}
          aria-label="Next screenshot"
        >
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
            <path
              d="M7.5 5L12.5 10L7.5 15"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </button>
      </div>

      {/* Dots */}
      <div className={styles.dots}>
        {slides.map((_, i) => (
          <button
            key={i}
            className={`${styles.dot} ${i === activeIndex ? styles.dotActive : ''}`}
            onClick={() => setActiveIndex(i)}
            aria-label={`Go to screenshot ${i + 1}`}
          />
        ))}
      </div>

      {/* Caption */}
      <p className={styles.caption} key={activeIndex}>
        {slides[activeIndex].caption}
      </p>
    </section>
  );
}

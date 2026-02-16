import { useState } from 'react';
import { QRCodeSVG } from 'qrcode.react';

interface Props {
  currentTopic: string;
  onChange: (topic: string) => void;
}

function generateSecureTopic(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
  return `schmux-${hex}`;
}

function isSecureTopic(topic: string): boolean {
  return /^schmux-[0-9a-f]{32}$/.test(topic);
}

export function NtfyTopicGenerator({ currentTopic, onChange }: Props) {
  const [showQR, setShowQR] = useState(isSecureTopic(currentTopic));
  const ntfyURL = currentTopic ? `https://ntfy.sh/${currentTopic}` : '';

  function handleGenerate() {
    const topic = generateSecureTopic();
    onChange(topic);
    setShowQR(true);
  }

  return (
    <div>
      <button type="button" onClick={handleGenerate} className="btn btn--secondary btn--sm">
        Generate secure topic
      </button>
      {showQR && ntfyURL && (
        <div style={{ marginTop: '0.75rem' }}>
          <QRCodeSVG value={ntfyURL} size={160} />
          <p className="form-group__hint">Scan with the ntfy app to subscribe to this topic.</p>
        </div>
      )}
    </div>
  );
}

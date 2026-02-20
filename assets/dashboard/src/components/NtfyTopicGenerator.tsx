import { QRCodeSVG } from 'qrcode.react';

interface GenerateButtonProps {
  onChange: (topic: string) => void;
}

interface QRDisplayProps {
  topic: string;
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

export function NtfyTopicGenerateButton({ onChange }: GenerateButtonProps) {
  function handleGenerate() {
    const topic = generateSecureTopic();
    onChange(topic);
  }

  return (
    <button type="button" onClick={handleGenerate} className="btn btn--secondary btn--sm">
      Generate secure topic
    </button>
  );
}

export function NtfyTopicQRDisplay({ topic }: QRDisplayProps) {
  const showQR = isSecureTopic(topic);
  const ntfyURL = topic ? `https://ntfy.sh/${topic}` : '';

  return (
    <div className="ntfy-qr-container">
      {showQR && ntfyURL ? (
        <>
          <div className="ntfy-qr-code">
            <QRCodeSVG value={ntfyURL} size={144} />
          </div>
          <p className="form-group__hint" style={{ maxWidth: '176px' }}>
            Scan this QR code with your phone to subscribe to the ntfy channel to receive the URL to
            access Schmux.
          </p>
        </>
      ) : (
        <div className="ntfy-qr-placeholder">
          <span>QR code will appear here after generating a topic</span>
        </div>
      )}
    </div>
  );
}

/** @deprecated Use NtfyTopicGenerateButton and NtfyTopicQRDisplay separately */
export function NtfyTopicGenerator({
  currentTopic,
  onChange,
}: {
  currentTopic: string;
  onChange: (topic: string) => void;
}) {
  return (
    <div>
      <NtfyTopicGenerateButton onChange={onChange} />
      <NtfyTopicQRDisplay topic={currentTopic} />
    </div>
  );
}

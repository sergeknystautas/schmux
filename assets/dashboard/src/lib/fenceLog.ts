// Fence monitor.log lines are plain text emitted by the `fence` OS tool, e.g.
//   [fence:http]      10:52:49 ✗ CONNECT 403 www.google.com https://www.google.com:443 (0s)
//   [fence:logstream] 10:52:49 ✗ file-write-create /private/etc/secret (bash:46380)
//   [fence:logstream] 10:52:49 ✗ mach-lookup com.apple.diagnosticd (curl:46381)
// parseFenceLine turns one into structured fields for the Logs "Fence" view.

type FenceKind = 'network' | 'file' | 'system' | 'other';

export interface FenceLine {
  time: string; // HH:MM:SS (or HH:MM); empty when unrecognized
  kind: FenceKind;
  message: string; // the action + target, e.g. "CONNECT 403 www.google.com …"
}

// [fence:<channel>] <time> <✗|✓> <message>
const LINE_RE = /^\[fence:([^\]]+)\]\s+(\S+)\s+[✗✓]\s+(.*)$/;

export function parseFenceLine(raw: string): FenceLine {
  const m = LINE_RE.exec(raw);
  if (!m) {
    // Unrecognized shape: still presented as a formatted row, never raw.
    return { time: '', kind: 'other', message: raw };
  }
  const [, channel, time, message] = m;
  let kind: FenceKind = 'other';
  if (channel === 'http') kind = 'network';
  else if (message.startsWith('file-')) kind = 'file';
  else if (message.startsWith('mach-lookup')) kind = 'system';
  return { time, kind, message };
}

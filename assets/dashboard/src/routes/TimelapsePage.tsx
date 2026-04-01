import { useState, useEffect, useCallback } from 'react';
import {
  getTimelapseRecordings,
  exportTimelapseRecording,
  deleteTimelapseRecording,
  type TimelapseRecording,
} from '../lib/api';

export default function TimelapsePage() {
  const [recordings, setRecordings] = useState<TimelapseRecording[]>([]);
  const [loading, setLoading] = useState(true);
  const [exporting, setExporting] = useState<Set<string>>(new Set());

  const fetchRecordings = useCallback(async () => {
    const data = await getTimelapseRecordings();
    setRecordings(data || []);
    setLoading(false);
  }, []);

  useEffect(() => {
    fetchRecordings();
  }, [fetchRecordings]);

  const handleExport = async (recordingId: string) => {
    setExporting((prev) => new Set(prev).add(recordingId));
    try {
      await exportTimelapseRecording(recordingId);
      fetchRecordings(); // refresh list to show Download button
    } finally {
      setExporting((prev) => {
        const next = new Set(prev);
        next.delete(recordingId);
        return next;
      });
    }
  };

  const handleDownload = (recordingId: string) => {
    window.open(`/api/timelapse/${recordingId}/download`, '_blank');
  };

  const handleDelete = async (recordingId: string) => {
    if (!confirm(`Delete recording ${recordingId}?`)) return;
    await deleteTimelapseRecording(recordingId);
    fetchRecordings();
  };

  const formatDuration = (seconds: number) => {
    if (seconds < 60) return `${Math.round(seconds)}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m${Math.round(seconds % 60)}s`;
    return `${Math.floor(seconds / 3600)}h${Math.floor((seconds % 3600) / 60)}m`;
  };

  const formatSize = (bytes: number) => {
    if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${bytes} B`;
  };

  if (loading) {
    return (
      <div className="page-content" style={{ padding: 'var(--spacing-lg)' }}>
        <h1>Timelapse Recordings</h1>
        <p>Loading...</p>
      </div>
    );
  }

  return (
    <div className="page-content" style={{ padding: 'var(--spacing-lg)' }}>
      <h1>Timelapse Recordings</h1>
      <p style={{ color: 'var(--text-secondary)', marginBottom: 'var(--spacing-md)' }}>
        Terminal sessions are automatically recorded. Export to .cast for playback with asciinema.
      </p>

      {recordings.length === 0 ? (
        <p>No recordings found. Recordings appear after sessions produce output.</p>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border-color)', textAlign: 'left' }}>
              <th style={{ padding: 'var(--spacing-sm)' }}>Recording</th>
              <th style={{ padding: 'var(--spacing-sm)' }}>Session</th>
              <th style={{ padding: 'var(--spacing-sm)' }}>Duration</th>
              <th style={{ padding: 'var(--spacing-sm)' }}>Size</th>
              <th style={{ padding: 'var(--spacing-sm)' }}>Status</th>
              <th style={{ padding: 'var(--spacing-sm)' }}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {recordings.map((rec) => (
              <tr key={rec.RecordingID} style={{ borderBottom: '1px solid var(--border-color)' }}>
                <td style={{ padding: 'var(--spacing-sm)', fontFamily: 'var(--font-mono)' }}>
                  {rec.RecordingID}
                </td>
                <td style={{ padding: 'var(--spacing-sm)' }}>
                  {rec.SessionID?.slice(0, 12) || '\u2014'}
                </td>
                <td style={{ padding: 'var(--spacing-sm)' }}>{formatDuration(rec.Duration)}</td>
                <td style={{ padding: 'var(--spacing-sm)' }}>{formatSize(rec.FileSize)}</td>
                <td style={{ padding: 'var(--spacing-sm)' }}>
                  <span
                    style={{
                      color: rec.InProgress ? 'var(--status-running)' : 'var(--text-secondary)',
                    }}
                  >
                    {rec.InProgress ? 'recording' : 'complete'}
                  </span>
                </td>
                <td style={{ padding: 'var(--spacing-sm)' }}>
                  <div style={{ display: 'flex', gap: 'var(--spacing-xs)' }}>
                    <button
                      className="btn btn--sm"
                      onClick={() => handleExport(rec.RecordingID)}
                      disabled={exporting.has(rec.RecordingID)}
                    >
                      {exporting.has(rec.RecordingID) ? 'Exporting...' : 'Export'}
                    </button>
                    {rec.HasExport && (
                      <button
                        className="btn btn--sm"
                        onClick={() => handleDownload(rec.RecordingID)}
                      >
                        Download
                      </button>
                    )}
                    <button
                      className="btn btn--sm btn--danger"
                      onClick={() => handleDelete(rec.RecordingID)}
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

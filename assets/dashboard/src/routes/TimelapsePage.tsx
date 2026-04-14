import { useState, useEffect, useCallback } from 'react';
import {
  getTimelapseRecordings,
  exportTimelapseRecording,
  deleteTimelapseRecording,
  type TimelapseRecording,
} from '../lib/api';
import { useModal } from '../components/ModalProvider';

export default function TimelapsePage() {
  const { confirm } = useModal();
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
      fetchRecordings();
      window.open(`/api/timelapse/${recordingId}/download?type=timelapse`, '_blank');
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
    const accepted = await confirm(`Delete recording ${recordingId}?`, { danger: true });
    if (!accepted) return;
    await deleteTimelapseRecording(recordingId);
    fetchRecordings();
  };

  const handleDeleteAll = async () => {
    const accepted = await confirm(
      `Delete all ${recordings.length} recording${recordings.length !== 1 ? 's' : ''}? This cannot be undone.`,
      { danger: true }
    );
    if (!accepted) return;
    await Promise.all(recordings.map((r) => deleteTimelapseRecording(r.RecordingID)));
    fetchRecordings();
  };

  const formatDuration = (seconds: number) => {
    if (seconds < 60) return `${Math.round(seconds)}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m${Math.round(seconds % 60)}s`;
    return `${Math.floor(seconds / 3600)}h${Math.floor((seconds % 3600) / 60)}m`;
  };

  const formatRelativeTime = (iso: string) => {
    const diff = Date.now() - new Date(iso).getTime();
    const seconds = Math.floor(diff / 1000);
    if (seconds < 60) return 'just now';
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
  };

  const formatSize = (bytes: number) => {
    if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${bytes} B`;
  };

  if (loading) {
    return (
      <div className="page-content timelapse">
        <div className="timelapse__header">
          <h1>Timelapses</h1>
        </div>
        <p className="timelapse__description">Loading...</p>
      </div>
    );
  }

  return (
    <div className="page-content timelapse">
      <div className="timelapse__header">
        <h1>Timelapses</h1>
        {recordings.length > 0 && (
          <button className="btn btn--sm btn--danger" onClick={handleDeleteAll}>
            Delete all
          </button>
        )}
      </div>
      <p className="timelapse__description">
        Terminal sessions are recorded as .cast files, playable with{' '}
        <a href="https://asciinema.org/" target="_blank" rel="noopener noreferrer">
          asciinema
        </a>
        . &ldquo;Timelapse&rdquo; compresses the recording with idle time removed.
      </p>

      {recordings.length === 0 ? (
        <p className="timelapse__empty">
          No recordings found. Recordings appear after sessions produce output.
        </p>
      ) : (
        <div className="timelapse__table-wrapper">
          <table className="timelapse__table">
            <thead>
              <tr>
                <th>Recording</th>
                <th>Session</th>
                <th className="timelapse__th--sorted">Modified &#x25BE;</th>
                <th>Duration</th>
                <th>Size</th>
                <th className="timelapse__th--center">Status</th>
                <th className="timelapse__th--center">Actions</th>
              </tr>
            </thead>
            <tbody>
              {recordings.map((rec) => (
                <tr key={rec.RecordingID}>
                  <td>
                    <span className="timelapse__recording-id">{rec.RecordingID}</span>
                  </td>
                  <td>
                    <span className="timelapse__session-id">
                      {rec.SessionID?.slice(0, 12) || '\u2014'}
                    </span>
                  </td>
                  <td
                    className="timelapse__meta"
                    title={rec.ModTime ? new Date(rec.ModTime).toLocaleString() : ''}
                  >
                    {rec.ModTime ? formatRelativeTime(rec.ModTime) : '\u2014'}
                  </td>
                  <td>{formatDuration(rec.Duration)}</td>
                  <td className="timelapse__meta">{formatSize(rec.FileSize)}</td>
                  <td className="timelapse__td--center">
                    <span
                      className={`badge ${rec.InProgress ? 'badge--success' : 'badge--neutral'}`}
                    >
                      {rec.InProgress ? 'recording' : 'complete'}
                    </span>
                  </td>
                  <td className="timelapse__td--center">
                    <div className="timelapse__actions">
                      <button
                        className="btn btn--sm btn--secondary"
                        onClick={() => handleDownload(rec.RecordingID)}
                      >
                        Original
                      </button>
                      <button
                        className="btn btn--sm btn--secondary"
                        onClick={() => handleExport(rec.RecordingID)}
                        disabled={exporting.has(rec.RecordingID)}
                      >
                        {exporting.has(rec.RecordingID) ? 'Creating...' : 'Timelapse'}
                      </button>
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
        </div>
      )}
    </div>
  );
}

import { useParams, useNavigate } from 'react-router-dom';
import CastPlayer from '../components/CastPlayer';

export default function TimelapsePlayerPage() {
  const { recordingId } = useParams<{ recordingId: string }>();
  const navigate = useNavigate();

  if (!recordingId) {
    return <div className="page-content timelapse">Recording not found.</div>;
  }

  return (
    <div className="page-content timelapse-player-page">
      <div className="timelapse-player-page__header">
        <button className="btn btn--sm" onClick={() => navigate('/timelapse')}>
          <svg
            viewBox="0 0 24 24"
            width="14"
            height="14"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            style={{ marginRight: 4, verticalAlign: -2 }}
          >
            <polyline points="15 18 9 12 15 6" />
          </svg>
          Back
        </button>
        <span className="timelapse-player-page__title">{recordingId}</span>
      </div>
      <CastPlayer recordingId={recordingId} />
    </div>
  );
}

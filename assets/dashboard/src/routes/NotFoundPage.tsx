import React from 'react';
import { Link } from 'react-router-dom';

export default function NotFoundPage() {
  return (
    <div className="empty-state">
      <div className="empty-state__icon">⚠️</div>
      <h3 className="empty-state__title">Page not found</h3>
      <p className="empty-state__description">The page you requested does not exist.</p>
      <Link to="/" className="btn btn--primary">
        Back to Home
      </Link>
    </div>
  );
}

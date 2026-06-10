export default function AuthGate() {
  return (
    <div className="auth-gate">
      <div className="auth-gate__card">
        <h1 className="auth-gate__title">Sign in to schmux</h1>
        <p className="auth-gate__text">This dashboard requires you to sign in with GitHub.</p>
        {/* Real navigation: /auth/login is a server-side redirect to GitHub, not an SPA route. */}
        <a className="btn auth-gate__btn" href="/auth/login">
          Sign in with GitHub
        </a>
      </div>
    </div>
  );
}

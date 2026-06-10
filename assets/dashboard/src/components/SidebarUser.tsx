import { useAuth } from '../contexts/AuthContext';

export default function SidebarUser({ navCollapsed }: { navCollapsed: boolean }) {
  const { authenticated, user, logout } = useAuth();
  if (authenticated !== true || !user) {
    return null;
  }
  return (
    <div className={`sidebar-user${navCollapsed ? ' sidebar-user--collapsed' : ''}`}>
      <img className="sidebar-user__avatar" src={user.avatar_url} alt="" width={20} height={20} />
      {!navCollapsed && (
        <>
          <span className="sidebar-user__login" title={user.name || user.login}>
            {user.login}
          </span>
          <button type="button" className="nav-workspace__dev-btn" onClick={() => logout()}>
            Sign out
          </button>
        </>
      )}
    </div>
  );
}

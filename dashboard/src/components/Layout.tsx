import { Outlet, Link, useNavigate } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';

export default function Layout() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white border-b border-gray-200">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between h-16">
            <div className="flex items-center space-x-8">
              <Link to="/" className="text-xl font-bold text-gray-900">Poly Paper</Link>
              <Link to="/" className="text-gray-600 hover:text-gray-900">Dashboard</Link>
              <Link to="/orders" className="text-gray-600 hover:text-gray-900">Orders</Link>
              <Link to="/positions" className="text-gray-600 hover:text-gray-900">Positions</Link>
              <Link to="/trades" className="text-gray-600 hover:text-gray-900">Trades</Link>
              <Link to="/wallet" className="text-gray-600 hover:text-gray-900">Wallet</Link>
              <Link to="/api-keys" className="text-gray-600 hover:text-gray-900">API Keys</Link>
            </div>
            <div className="flex items-center space-x-4">
              <span className="text-sm text-gray-500">{user?.email}</span>
              <button onClick={handleLogout} className="text-sm text-red-600 hover:text-red-800">
                Logout
              </button>
            </div>
          </div>
        </div>
      </nav>
      <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        <Outlet />
      </main>
    </div>
  );
}

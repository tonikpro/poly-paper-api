import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthProvider } from './context/AuthContext';
import Layout from './components/Layout';
import ProtectedRoute from './components/ProtectedRoute';
import Login from './pages/Login';
import Register from './pages/Register';
import Dashboard from './pages/Dashboard';
import Orders from './pages/Orders';
import History from './pages/History';
import Positions from './pages/Positions';
import Wallet from './pages/Wallet';
import ApiKeys from './pages/ApiKeys';

export default function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/register" element={<Register />} />
          <Route path="/" element={<ProtectedRoute><Layout /></ProtectedRoute>}>
            <Route index element={<Dashboard />} />
            <Route path="orders" element={<Orders />} />
            <Route path="history" element={<History />} />
            <Route path="positions" element={<Positions />} />
            <Route path="wallet" element={<Wallet />} />
            <Route path="api-keys" element={<ApiKeys />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  );
}

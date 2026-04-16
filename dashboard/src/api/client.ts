import axios from 'axios';

const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL ?? '',
  headers: { 'Content-Type': 'application/json' },
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('token');
      localStorage.removeItem('user');
      window.location.href = '/login';
    }
    return Promise.reject(error);
  }
);

// Auth
export const register = (email: string, password: string) =>
  api.post('/auth/register', { email, password });

export const login = (email: string, password: string) =>
  api.post('/auth/login', { email, password });

// Wallet
export const getWallet = () => api.get('/api/wallet');
export const deposit = (amount: string) => api.post('/api/wallet/deposit', { amount });
export const withdraw = (amount: string) => api.post('/api/wallet/withdraw', { amount });

// Trading data
export const getOrders = (params?: { status?: string; limit?: number; offset?: number }) =>
  api.get('/api/orders', { params });

export const getPositions = () => api.get('/api/positions');

export const getTrades = (params?: { limit?: number; offset?: number; asset_id?: string }) =>
  api.get('/api/trades', { params });

export const cancelOrder = (orderID: string) =>
  api.delete('/clob/order', { data: { orderID } });

// Stats
export const getStats = () => api.get('/api/stats');

// Eth address
export const getEthAddress = () => api.get('/api/eth-address');

// API Keys
export const getApiKeys = () => api.get('/api/api-keys');
export const createApiKey = () => api.post('/api/api-keys');
export const deleteApiKey = (apiKey: string) => api.delete('/api/api-keys', { data: { apiKey } });

export default api;

import { useEffect, useState } from 'react';
import { getWallet, getOrders, getPositions, getTrades } from '../api/client';
import { useAuth } from '../context/AuthContext';

export default function Dashboard() {
  const { user } = useAuth();
  const [balance, setBalance] = useState('--');
  const [orderCount, setOrderCount] = useState(0);
  const [positionCount, setPositionCount] = useState(0);
  const [tradeCount, setTradeCount] = useState(0);

  useEffect(() => {
    getWallet().then(r => setBalance(r.data.balance || '0')).catch(() => {});
    getOrders({ limit: 1 }).then(r => setOrderCount(r.data.total || 0)).catch(() => {});
    getPositions().then(r => setPositionCount(r.data.positions?.length || 0)).catch(() => {});
    getTrades({ limit: 1 }).then(r => setTradeCount(r.data.total || 0)).catch(() => {});
  }, []);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Dashboard</h1>
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <Card title="Balance" value={`$${balance}`} />
        <Card title="Open Orders" value={String(orderCount)} />
        <Card title="Positions" value={String(positionCount)} />
        <Card title="Total Trades" value={String(tradeCount)} />
      </div>

      <div className="bg-white rounded-lg shadow p-6">
        <h2 className="text-lg font-semibold mb-4">Bot Configuration</h2>
        <div className="space-y-2 text-sm font-mono">
          <div><span className="text-gray-500">CLOB URL:</span> <code className="bg-gray-100 px-2 py-1 rounded">http://localhost:8080/clob</code></div>
          <div><span className="text-gray-500">Email:</span> {user?.email}</div>
          <div><span className="text-gray-500">ETH Address:</span> <code className="bg-gray-100 px-2 py-1 rounded text-xs">{user?.eth_address}</code></div>
        </div>
        <p className="mt-4 text-sm text-gray-500">
          Use the API Keys page to generate credentials, then configure your bot with the CLOB URL above.
        </p>
      </div>
    </div>
  );
}

function Card({ title, value }: { title: string; value: string }) {
  return (
    <div className="bg-white rounded-lg shadow p-6">
      <div className="text-sm text-gray-500">{title}</div>
      <div className="text-2xl font-bold mt-1">{value}</div>
    </div>
  );
}

import { useEffect, useState } from 'react';
import { getWallet, getPositions, getTrades, getStats } from '../api/client';
import { useAuth } from '../context/AuthContext';

interface Stats {
  total_pnl: number;
  today_pnl: number;
  month_pnl: number;
  total_bets: number;
  won_bets: number;
  lost_bets: number;
}

export default function Dashboard() {
  const { user } = useAuth();
  const [balance, setBalance] = useState('--');
  const [positionCount, setPositionCount] = useState(0);
  const [tradeCount, setTradeCount] = useState(0);
  const [stats, setStats] = useState<Stats | null>(null);

  useEffect(() => {
    getWallet().then(r => setBalance(r.data.balance || '0')).catch(() => {});
    getPositions().then(r => setPositionCount((r.data.positions || []).filter((p: any) => p.is_open).length)).catch(() => {});
    getTrades({ limit: 1 }).then(r => setTradeCount(r.data.total || 0)).catch(() => {});
    getStats().then(r => setStats(r.data)).catch(() => {});
  }, []);

  const winRate = stats && stats.total_bets > 0
    ? Math.round((stats.won_bets / stats.total_bets) * 100)
    : null;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Dashboard</h1>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 mb-4">
        <Card title="Balance" value={`$${balance}`} />
        <Card title="Open Positions" value={String(positionCount)} />
        <Card title="Total Trades" value={String(tradeCount)} />
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <PnlCard title="Total P&L" value={stats?.total_pnl} />
        <PnlCard title="This Month" value={stats?.month_pnl} />
        <PnlCard title="Today" value={stats?.today_pnl} />
        <div className="bg-white rounded-lg shadow p-6">
          <div className="text-sm text-gray-500">Win Rate</div>
          <div className="text-2xl font-bold mt-1">
            {winRate !== null ? `${winRate}%` : '--'}
          </div>
          {stats && (
            <div className="text-xs text-gray-400 mt-1">
              {stats.won_bets}W / {stats.lost_bets}L / {stats.total_bets - stats.won_bets - stats.lost_bets} pending
            </div>
          )}
        </div>
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

function PnlCard({ title, value }: { title: string; value?: number }) {
  const isPositive = value !== undefined && value >= 0;
  const formatted = value !== undefined ? `${isPositive ? '+' : ''}$${value.toFixed(2)}` : '--';
  return (
    <div className="bg-white rounded-lg shadow p-6">
      <div className="text-sm text-gray-500">{title}</div>
      <div className={`text-2xl font-bold mt-1 ${value === undefined ? 'text-gray-800' : isPositive ? 'text-green-600' : 'text-red-600'}`}>
        {formatted}
      </div>
    </div>
  );
}

import { useEffect, useState } from 'react';
import { getTrades } from '../api/client';

interface Trade {
  id: string;
  asset_id: string;
  side: string;
  price: string;
  size: string;
  status: string;
  match_time: string;
}

export default function Trades() {
  const [trades, setTrades] = useState<Trade[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getTrades({ limit: 50 })
      .then(r => setTrades(r.data.trades || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="text-center py-8">Loading...</div>;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Trade History</h1>
      {trades.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No trades yet.
        </div>
      ) : (
        <div className="bg-white rounded-lg shadow overflow-x-auto">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Side</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Price</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Token</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {trades.map(t => (
                <tr key={t.id}>
                  <td className={`px-4 py-3 font-medium ${t.side === 'BUY' ? 'text-green-600' : 'text-red-600'}`}>{t.side}</td>
                  <td className="px-4 py-3">{t.price}</td>
                  <td className="px-4 py-3">{t.size}</td>
                  <td className="px-4 py-3 text-xs">{t.status}</td>
                  <td className="px-4 py-3 text-xs font-mono truncate max-w-[120px]" title={t.asset_id}>{t.asset_id?.slice(0, 12)}...</td>
                  <td className="px-4 py-3 text-xs text-gray-500">{new Date(t.match_time).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

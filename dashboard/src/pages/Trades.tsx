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
  outcome: string;
  winner: boolean | null;
  profit_loss: number | null;
}

function ResultBadge({ side, winner }: { side: string; winner: boolean | null }) {
  if (side !== 'BUY') return <span className="text-gray-400 text-xs">—</span>;
  if (winner === null) return <span className="bg-gray-100 text-gray-500 text-xs px-2 py-0.5 rounded-full">Pending</span>;
  return winner
    ? <span className="bg-green-100 text-green-700 text-xs px-2 py-0.5 rounded-full font-medium">Win</span>
    : <span className="bg-red-100 text-red-700 text-xs px-2 py-0.5 rounded-full font-medium">Loss</span>;
}

export default function Trades() {
  const [trades, setTrades] = useState<Trade[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getTrades({ limit: 100 })
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
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Price</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Result</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">P&L</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Token</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {trades.map(t => (
                <tr key={t.id} className="hover:bg-gray-50">
                  <td className={`px-4 py-3 font-medium ${t.side === 'BUY' ? 'text-green-600' : 'text-red-600'}`}>{t.side}</td>
                  <td className="px-4 py-3 text-sm">{t.outcome || '—'}</td>
                  <td className="px-4 py-3">{t.price}</td>
                  <td className="px-4 py-3">{t.size}</td>
                  <td className="px-4 py-3">
                    <ResultBadge side={t.side} winner={t.winner} />
                  </td>
                  <td className="px-4 py-3 font-medium">
                    {t.profit_loss !== null && t.profit_loss !== undefined
                      ? <span className={t.profit_loss >= 0 ? 'text-green-600' : 'text-red-600'}>
                          {t.profit_loss >= 0 ? '+' : ''}${t.profit_loss.toFixed(2)}
                        </span>
                      : <span className="text-gray-400 text-xs">—</span>
                    }
                  </td>
                  <td className="px-4 py-3 text-xs font-mono truncate max-w-[100px]" title={t.asset_id}>{t.asset_id?.slice(0, 10)}…</td>
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
